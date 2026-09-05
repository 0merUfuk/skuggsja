// Package analytics turns normalized, content-free sessions into the artifact
// served by the local Rewind UI.
package analytics

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/0merUfuk/skuggsja/internal/audit"
	"github.com/0merUfuk/skuggsja/internal/model"
)

const SchemaVersion = 2

// Report is the only persisted representation. It contains no raw text,
// source identifiers, or absolute filesystem paths.
type Report struct {
	SchemaVersion int               `json:"schema_version"`
	GeneratedAt   time.Time         `json:"generated_at"`
	ProductName   string            `json:"product_name"`
	Coverage      Coverage          `json:"coverage"`
	Totals        Totals            `json:"totals"`
	Providers     []ProviderSummary `json:"providers"`
	Rhythm        Rhythm            `json:"rhythm"`
	PromptStyle   PromptStyle       `json:"prompt_style"`
	Models        []ModelSummary    `json:"models"`
	Projects      []ProjectSummary  `json:"projects"`
	Longest       LongestSession    `json:"longest_session"`
	Privacy       Privacy           `json:"privacy"`
	Methodology   []string          `json:"methodology"`
	Warnings      []ReportWarning   `json:"warnings"`
}

type Coverage struct {
	Label           string    `json:"label"`
	Start           time.Time `json:"start"`
	End             time.Time `json:"end"`
	Timezone        string    `json:"timezone"`
	CalendarFraming bool      `json:"calendar_framing"`
}

type Totals struct {
	Sessions           int   `json:"sessions"`
	Prompts            int   `json:"prompts"`
	Projects           int   `json:"projects"`
	ToolCalls          int64 `json:"tool_calls"`
	ActiveDays         int   `json:"active_days"`
	ChildSessions      int   `json:"child_sessions"`
	SourceFiles        int   `json:"source_files"`
	SourceFilesChanged int   `json:"source_files_changed"`
}

type ProviderSummary struct {
	ID                 model.Harness    `json:"id"`
	Name               string           `json:"name"`
	Status             string           `json:"status"`
	Verification       string           `json:"verification"`
	Sessions           int              `json:"sessions"`
	ChildSessions      int              `json:"child_sessions"`
	Prompts            int              `json:"prompts"`
	Projects           int              `json:"projects"`
	ToolCalls          int64            `json:"tool_calls"`
	ToolCallsAvailable bool             `json:"tool_calls_available"`
	SpanStart          time.Time        `json:"span_start"`
	SpanEnd            time.Time        `json:"span_end"`
	TimeBasis          string           `json:"time_basis"`
	TokenUsage         model.TokenUsage `json:"token_usage"`
	Limitations        []string         `json:"limitations"`
	Warnings           []model.Warning  `json:"warnings"`
	SourceFileCount    int              `json:"source_file_count"`
	Coverage           ProviderCoverage `json:"coverage"`
}

// ProviderCoverage makes local-data completeness explicit instead of allowing
// a small recovered count to imply light real-world usage.
type ProviderCoverage struct {
	Status                 string    `json:"status"`
	Confidence             string    `json:"confidence"`
	EarliestLocalEvidence  time.Time `json:"earliest_local_evidence"`
	EarliestDetailedRecord time.Time `json:"earliest_detailed_record"`
	HistoryOnlySessions    int       `json:"history_only_sessions"`
	UnmaterializedSessions int       `json:"unmaterialized_sessions"`
	Note                   string    `json:"note"`
}

type Rhythm struct {
	Activity         []DayActivity `json:"activity"`
	Hours            []int         `json:"hours"`
	Weekdays         []int         `json:"weekdays"`
	FavoriteHour     int           `json:"favorite_hour"`
	LateNightPercent float64       `json:"late_night_percent"`
	LongestStreak    int           `json:"longest_streak"`
	BusiestDay       BusiestDay    `json:"busiest_day"`
	BusiestMonth     BusiestMonth  `json:"busiest_month"`
}

type DayActivity struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

type BusiestDay struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

type BusiestMonth struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

type PromptStyle struct {
	Available         bool    `json:"available"`
	Count             int     `json:"count"`
	MedianWords       float64 `json:"median_words"`
	AverageWords      float64 `json:"average_words"`
	AverageCharacters float64 `json:"average_characters"`
	Label             string  `json:"label"`
}

type ModelSummary struct {
	Harness model.Harness `json:"harness"`
	Name    string        `json:"name"`
	Turns   int64         `json:"turns"`
}

type ProjectSummary struct {
	Name     string `json:"name"`
	Sessions int    `json:"sessions"`
}

type LongestSession struct {
	Available       bool          `json:"available"`
	Harness         model.Harness `json:"harness"`
	DurationMinutes int64         `json:"duration_minutes"`
}

type Privacy struct {
	RawContentPersisted    bool             `json:"raw_content_persisted"`
	AbsolutePathsPersisted bool             `json:"absolute_paths_persisted"`
	SourceAudit            audit.Comparison `json:"source_audit"`
}

type ReportWarning struct {
	Harness model.Harness `json:"harness"`
	Code    string        `json:"code"`
	Count   int           `json:"count"`
	Message string        `json:"message"`
}

// Options supplies nondeterministic inputs explicitly for testability.
type Options struct {
	Now         time.Time
	Location    *time.Location
	SourceAudit audit.Comparison
}

// Build derives a report from every provider result.
func Build(results []model.ProviderResult, options Options) Report {
	location := options.Location
	if location == nil {
		location = time.Local
	}
	if options.Now.IsZero() {
		options.Now = time.Now()
	}
	report := Report{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   options.Now,
		ProductName:   "skuggsja",
		Privacy: Privacy{
			RawContentPersisted: false, AbsolutePathsPersisted: false,
			SourceAudit: options.SourceAudit,
		},
		Methodology: []string{
			"A session is a top-level local history record; child and subagent runs are counted separately.",
			"Activity uses each harness's session start or composer creation time, converted to the machine's local timezone.",
			"Prompt lengths are Unicode word and character counts derived transiently in memory; raw text is discarded.",
			"Token fields are shown only when recorded by the source. Harness totals remain separate because cache semantics differ.",
			"Coverage describes surviving histories on this machine, not a complete account lifetime.",
		},
	}

	dayCounts := make(map[string]int)
	monthCounts := make(map[string]int)
	projectCounts := make(map[string]int)
	report.Rhythm.Hours = make([]int, 24)
	report.Rhythm.Weekdays = make([]int, 7)
	var coverageStart, coverageEnd time.Time
	var textualPrompts []model.PromptMetric
	var longestDuration time.Duration

	for _, result := range results {
		summary, providerModels, prompts := summarizeProvider(result)
		report.Providers = append(report.Providers, summary)
		report.Models = append(report.Models, providerModels...)
		report.Totals.Sessions += summary.Sessions
		report.Totals.ChildSessions += summary.ChildSessions
		report.Totals.Prompts += summary.Prompts
		if summary.ToolCallsAvailable {
			report.Totals.ToolCalls += summary.ToolCalls
		}
		report.Totals.SourceFiles += summary.SourceFileCount
		for _, warning := range result.Warnings {
			report.Warnings = append(report.Warnings, ReportWarning{
				Harness: result.Harness, Code: warning.Code, Count: warning.Count, Message: warning.Message,
			})
		}
		textualPrompts = append(textualPrompts, prompts...)

		for _, session := range result.Sessions {
			if session.IsChild {
				continue
			}
			if session.Unanchored || strings.HasPrefix(session.ActivityBasis, "file modification time") {
				continue
			}
			if session.Project != "" {
				projectCounts[session.Project]++
			}
			if session.TimeUnavailable {
				continue
			}
			start := session.StartedAt
			end := session.EndedAt
			if end.IsZero() {
				end = start
			}
			if !start.IsZero() && (coverageStart.IsZero() || start.Before(coverageStart)) {
				coverageStart = start
			}
			if !end.IsZero() && (coverageEnd.IsZero() || end.After(coverageEnd)) {
				coverageEnd = end
			}
			duration := end.Sub(start)
			if duration >= 0 && (!report.Longest.Available || duration > longestDuration) {
				longestDuration = duration
				report.Longest = LongestSession{
					Available: true, Harness: session.Harness,
					DurationMinutes: int64(math.Round(duration.Minutes())),
				}
			}
			activityAt := session.ActivityAt
			if activityAt.IsZero() {
				activityAt = start
			}
			if activityAt.IsZero() {
				continue
			}
			local := activityAt.In(location)
			dayCounts[local.Format("2006-01-02")]++
			monthCounts[local.Format("2006-01")]++
			report.Rhythm.Hours[local.Hour()]++
			weekday := (int(local.Weekday()) + 6) % 7 // Monday first.
			report.Rhythm.Weekdays[weekday]++
		}
	}

	report.Coverage = Coverage{
		Label: coverageLabel(coverageStart, coverageEnd, location), Start: coverageStart,
		End: coverageEnd, Timezone: timezoneLabel(location, coverageStart, options.Now),
		CalendarFraming: supportsCalendarYear(coverageStart, coverageEnd, location),
	}
	report.Rhythm.Activity = sortedActivity(dayCounts)
	report.Rhythm.FavoriteHour = maxIndex(report.Rhythm.Hours)
	report.Rhythm.LateNightPercent = lateNightPercent(report.Rhythm.Hours)
	report.Rhythm.LongestStreak = longestStreak(report.Rhythm.Activity)
	report.Rhythm.BusiestDay = busiestDay(report.Rhythm.Activity)
	report.Rhythm.BusiestMonth = busiestMonth(monthCounts)
	report.Totals.ActiveDays = len(report.Rhythm.Activity)
	report.PromptStyle = promptStyle(textualPrompts)

	for name, count := range projectCounts {
		report.Projects = append(report.Projects, ProjectSummary{Name: name, Sessions: count})
	}
	sort.Slice(report.Projects, func(i, j int) bool {
		if report.Projects[i].Sessions == report.Projects[j].Sessions {
			return strings.ToLower(report.Projects[i].Name) < strings.ToLower(report.Projects[j].Name)
		}
		return report.Projects[i].Sessions > report.Projects[j].Sessions
	})
	report.Totals.Projects = len(report.Projects)
	report.Totals.SourceFilesChanged = options.SourceAudit.ChangedFiles
	sort.Slice(report.Models, func(i, j int) bool {
		if report.Models[i].Turns == report.Models[j].Turns {
			if report.Models[i].Harness == report.Models[j].Harness {
				return report.Models[i].Name < report.Models[j].Name
			}
			return report.Models[i].Harness < report.Models[j].Harness
		}
		return report.Models[i].Turns > report.Models[j].Turns
	})
	sort.Slice(report.Providers, func(i, j int) bool { return report.Providers[i].ID < report.Providers[j].ID })
	return report
}

func summarizeProvider(result model.ProviderResult) (ProviderSummary, []ModelSummary, []model.PromptMetric) {
	summary := ProviderSummary{
		ID: result.Harness, Name: result.DisplayName, Status: result.Status,
		Verification: result.VerificationLevel, Limitations: result.Limitations,
		Warnings: result.Warnings, SourceFileCount: len(result.SourceFiles),
		ToolCallsAvailable: result.ToolCallsAvailable,
		Coverage: ProviderCoverage{
			Status: result.Coverage.Status, Confidence: result.Coverage.Confidence,
			EarliestLocalEvidence:  result.Coverage.EarliestLocalEvidence,
			EarliestDetailedRecord: result.Coverage.EarliestDetailedRecord,
			HistoryOnlySessions:    result.Coverage.HistoryOnlySessions,
			UnmaterializedSessions: result.Coverage.UnmaterializedSessions,
			Note:                   result.Coverage.Note,
		},
	}
	projects := make(map[string]struct{})
	promptIDs := make(map[string]struct{})
	callIDs := make(map[string]model.CallMetric)
	modelTurns := make(map[string]int64)
	toolIDs := make(map[string]struct{})
	var textual []model.PromptMetric
	unnamedCall, unnamedTool := 0, 0

	for _, session := range result.Sessions {
		if session.IsChild {
			summary.ChildSessions++
			continue
		}
		if !session.Unanchored && !strings.HasPrefix(session.ActivityBasis, "file modification time") {
			summary.Sessions++
			if !session.TimeUnavailable {
				if summary.SpanStart.IsZero() || session.StartedAt.Before(summary.SpanStart) {
					summary.SpanStart = session.StartedAt
				}
				spanEnd := session.EndedAt
				if spanEnd.IsZero() {
					spanEnd = session.StartedAt
				}
				if summary.SpanEnd.IsZero() || spanEnd.After(summary.SpanEnd) {
					summary.SpanEnd = spanEnd
				}
				if summary.TimeBasis == "" {
					summary.TimeBasis = session.ActivityBasis
				} else if summary.TimeBasis != session.ActivityBasis {
					summary.TimeBasis = "mixed recorded times"
				}
			}
			if session.Project != "" {
				projects[session.Project] = struct{}{}
			}
		}
		for _, prompt := range session.Prompts {
			key := prompt.EventID
			if key == "" {
				key = fmt.Sprintf("_unnamed_prompt_%d", summary.Prompts)
			}
			if _, exists := promptIDs[key]; exists {
				continue
			}
			promptIDs[key] = struct{}{}
			summary.Prompts++
			if prompt.HasText {
				textual = append(textual, prompt)
			}
		}

		if len(session.Calls) > 0 {
			for _, call := range session.Calls {
				for _, id := range call.ToolIDs {
					if id == "" {
						unnamedTool++
						id = fmt.Sprintf("_unnamed_tool_%d", unnamedTool)
					}
					toolIDs[id] = struct{}{}
				}
				key := call.ID
				if key == "" {
					unnamedCall++
					key = fmt.Sprintf("_unnamed_call_%d", unnamedCall)
				}
				if _, exists := callIDs[key]; !exists {
					callIDs[key] = call
				}
			}
			continue
		}
		summary.TokenUsage.Add(session.Usage)
		summary.ToolCalls += session.ToolCalls
		for name, activity := range session.Models {
			modelTurns[name] += activity.Turns
		}
	}

	for _, call := range callIDs {
		summary.TokenUsage.Add(call.Usage)
		if call.Model != "" {
			modelTurns[call.Model]++
		}
	}
	if len(callIDs) > 0 {
		summary.ToolCalls = int64(len(toolIDs))
	}
	summary.Projects = len(projects)
	if summary.Coverage.EarliestLocalEvidence.IsZero() {
		summary.Coverage.EarliestLocalEvidence = summary.SpanStart
	}
	if summary.Coverage.EarliestDetailedRecord.IsZero() && summary.Sessions > 0 {
		for _, session := range result.Sessions {
			if session.IsChild || session.HistoryOnly || session.Unanchored || session.TimeUnavailable || session.StartedAt.IsZero() ||
				strings.HasPrefix(session.ActivityBasis, "file modification time") {
				continue
			}
			if summary.Coverage.EarliestDetailedRecord.IsZero() || session.StartedAt.Before(summary.Coverage.EarliestDetailedRecord) {
				summary.Coverage.EarliestDetailedRecord = session.StartedAt
			}
		}
	}
	if summary.Coverage.Status == "" {
		if result.Status == "unavailable" || result.Status == "unsupported schema" {
			summary.Coverage.Status = "assessment unavailable"
			summary.Coverage.Confidence = "low"
			summary.Coverage.Note = "The configured local source could not be assessed, so neither absence nor completeness can be inferred."
		} else if summary.Sessions == 0 && summary.ChildSessions == 0 {
			summary.Coverage.Status = "no local evidence"
			summary.Coverage.Confidence = "high"
			summary.Coverage.Note = "No supported local records were found for this harness."
		} else {
			summary.Coverage.Status = "completeness unknown"
			summary.Coverage.Confidence = "medium"
			summary.Coverage.Note = "Surviving local records were read, but account-lifetime completeness cannot be established."
		}
	}
	models := make([]ModelSummary, 0, len(modelTurns))
	for name, turns := range modelTurns {
		if name != "" && turns > 0 {
			models = append(models, ModelSummary{Harness: result.Harness, Name: name, Turns: turns})
		}
	}
	return summary, models, textual
}

func coverageLabel(start, end time.Time, location *time.Location) string {
	if start.IsZero() || end.IsZero() {
		return "No local history found"
	}
	start = start.In(location)
	end = end.In(location)
	if supportsCalendarYear(start, end, location) {
		return fmt.Sprintf("%d", start.Year())
	}
	if start.Year() == end.Year() {
		return fmt.Sprintf("%s %d — %s %d, %d", start.Month().String()[:3], start.Day(), end.Month().String()[:3], end.Day(), end.Year())
	}
	return fmt.Sprintf("%s %d, %d — %s %d, %d", start.Month().String()[:3], start.Day(), start.Year(), end.Month().String()[:3], end.Day(), end.Year())
}

func supportsCalendarYear(start, end time.Time, location *time.Location) bool {
	if start.IsZero() || end.IsZero() {
		return false
	}
	start = start.In(location)
	end = end.In(location)
	return start.Year() == end.Year() && start.Month() == time.January && start.Day() <= 7 &&
		end.Month() == time.December && end.Day() >= 25
}

func timezoneLabel(location *time.Location, reference, fallback time.Time) string {
	if name := location.String(); name != "" && name != "Local" {
		return name
	}
	if reference.IsZero() {
		reference = fallback
	}
	_, offsetSeconds := reference.In(location).Zone()
	sign := "+"
	if offsetSeconds < 0 {
		sign = "-"
		offsetSeconds = -offsetSeconds
	}
	return fmt.Sprintf("Local time (UTC%s%02d:%02d at span start)", sign, offsetSeconds/3600, (offsetSeconds%3600)/60)
}

func sortedActivity(counts map[string]int) []DayActivity {
	activity := make([]DayActivity, 0, len(counts))
	for date, count := range counts {
		activity = append(activity, DayActivity{Date: date, Count: count})
	}
	sort.Slice(activity, func(i, j int) bool { return activity[i].Date < activity[j].Date })
	return activity
}

func maxIndex(values []int) int {
	if len(values) == 0 {
		return 0
	}
	index := 0
	for i := 1; i < len(values); i++ {
		if values[i] > values[index] {
			index = i
		}
	}
	return index
}

func lateNightPercent(hours []int) float64 {
	total, late := 0, 0
	for hour, count := range hours {
		total += count
		if hour < 5 {
			late += count
		}
	}
	if total == 0 {
		return 0
	}
	return math.Round(float64(late)*1000/float64(total)) / 10
}

func longestStreak(activity []DayActivity) int {
	longest, current := 0, 0
	var previous time.Time
	for _, day := range activity {
		parsed, err := time.Parse("2006-01-02", day.Date)
		if err != nil {
			continue
		}
		if !previous.IsZero() && parsed.Sub(previous) == 24*time.Hour {
			current++
		} else {
			current = 1
		}
		if current > longest {
			longest = current
		}
		previous = parsed
	}
	return longest
}

func busiestDay(activity []DayActivity) BusiestDay {
	var busiest BusiestDay
	for _, day := range activity {
		if day.Count > busiest.Count {
			busiest = BusiestDay{Date: day.Date, Count: day.Count}
		}
	}
	return busiest
}

func busiestMonth(counts map[string]int) BusiestMonth {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var busiest BusiestMonth
	for _, key := range keys {
		if counts[key] <= busiest.Count {
			continue
		}
		parsed, err := time.Parse("2006-01", key)
		if err != nil {
			continue
		}
		busiest = BusiestMonth{Label: parsed.Format("Jan 2006"), Count: counts[key]}
	}
	return busiest
}

func promptStyle(prompts []model.PromptMetric) PromptStyle {
	if len(prompts) == 0 {
		return PromptStyle{}
	}
	words := make([]int, 0, len(prompts))
	var totalWords, totalCharacters int
	for _, prompt := range prompts {
		words = append(words, prompt.Words)
		totalWords += prompt.Words
		totalCharacters += prompt.Characters
	}
	sort.Ints(words)
	median := float64(words[len(words)/2])
	if len(words)%2 == 0 {
		median = float64(words[len(words)/2-1]+words[len(words)/2]) / 2
	}
	label := "precision prompter"
	switch {
	case median <= 15:
		label = "terse operator"
	case median > 60:
		label = "specification writer"
	}
	return PromptStyle{
		Available: true, Count: len(prompts), MedianWords: median,
		AverageWords:      math.Round(float64(totalWords)*10/float64(len(prompts))) / 10,
		AverageCharacters: math.Round(float64(totalCharacters)*10/float64(len(prompts))) / 10,
		Label:             label,
	}
}
