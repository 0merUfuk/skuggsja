// Package claude reads Claude Code's primary JSONL session histories.
package claude

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/0merUfuk/skuggsja/internal/model"
	"github.com/0merUfuk/skuggsja/internal/provider"
)

const maxRecordBytes = 64 << 20

// Reader discovers sessions below Claude Code's projects directory.
type Reader struct{ ProjectsDir string }

func (Reader) Harness() model.Harness { return model.Claude }
func (Reader) DisplayName() string    { return "Claude Code" }

func (r Reader) Discover(_ context.Context) (provider.Discovery, error) {
	d := provider.Discovery{Harness: model.Claude, Roots: []string{r.ProjectsDir}}
	info, err := os.Stat(r.ProjectsDir)
	if errors.Is(err, os.ErrNotExist) {
		return d, nil
	}
	if err != nil {
		return d, err
	}
	if !info.IsDir() {
		return d, nil
	}
	err = filepath.WalkDir(r.ProjectsDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			return nil
		}
		rel, relErr := filepath.Rel(r.ProjectsDir, path)
		if relErr != nil {
			return relErr
		}
		// Primary sessions are exactly <encoded-project>/<uuid>.jsonl. Nested
		// subagent histories duplicate records and use different semantics.
		if len(strings.Split(rel, string(filepath.Separator))) == 2 {
			d.Files = append(d.Files, path)
		}
		return nil
	})
	sort.Strings(d.Files)
	return d, err
}

func (r Reader) Read(ctx context.Context, d provider.Discovery) model.ProviderResult {
	result := model.ProviderResult{
		Harness:           model.Claude,
		DisplayName:       "Claude Code",
		Status:            "not found",
		VerificationLevel: "real data on macOS",
		SourceFiles:       append([]string(nil), d.Files...),
		Limitations: []string{
			"Nested subagent transcripts are excluded because current Claude Code histories duplicate events across files.",
			"Model events are deduplicated assistant API messages, not a cross-harness turn unit.",
		},
	}
	if len(d.Files) == 0 {
		return result
	}
	result.Status = "supported"

	type parsed struct {
		session  model.Session
		warnings map[string]int
		err      error
	}
	jobs := make(chan string)
	parsedFiles := make(chan parsed)
	workers := min(6, len(d.Files))
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				session, warnings, err := parseFile(ctx, path)
				parsedFiles <- parsed{session: session, warnings: warnings, err: err}
			}
		}()
	}
	go func() {
		defer close(parsedFiles)
		for _, path := range d.Files {
			jobs <- path
		}
		close(jobs)
		wg.Wait()
	}()

	for item := range parsedFiles {
		if item.err != nil {
			result.AddWarning("unreadable_file", "Some session files could not be read and were skipped.")
			continue
		}
		for code, count := range item.warnings {
			message := map[string]string{
				"malformed_record": "Malformed records were skipped without modifying their source files.",
				"oversize_record":  "Oversize records were skipped to keep memory use bounded.",
			}[code]
			for range count {
				result.AddWarning(code, message)
			}
		}
		if !item.session.StartedAt.IsZero() {
			result.Sessions = append(result.Sessions, item.session)
		}
	}
	if len(result.Warnings) > 0 {
		result.Status = "supported with warnings"
	}
	sort.Slice(result.Sessions, func(i, j int) bool {
		if result.Sessions[i].StartedAt.Equal(result.Sessions[j].StartedAt) {
			return result.Sessions[i].ID < result.Sessions[j].ID
		}
		return result.Sessions[i].StartedAt.Before(result.Sessions[j].StartedAt)
	})
	annotateCopiedHistory(&result)
	return result
}

func annotateCopiedHistory(result *model.ProviderResult) {
	seenCalls := make(map[string]model.CallMetric)
	seenPrompts := make(map[string]struct{})
	duplicates, conflicts, promptDuplicates := 0, 0, 0
	for _, session := range result.Sessions {
		for _, call := range session.Calls {
			if call.ID == "" {
				continue
			}
			previous, exists := seenCalls[call.ID]
			if !exists {
				seenCalls[call.ID] = call
				continue
			}
			duplicates++
			if previous.Model != call.Model || previous.Usage != call.Usage {
				conflicts++
			}
		}
		for _, prompt := range session.Prompts {
			if prompt.EventID == "" {
				continue
			}
			if _, exists := seenPrompts[prompt.EventID]; exists {
				promptDuplicates++
			} else {
				seenPrompts[prompt.EventID] = struct{}{}
			}
		}
	}
	if duplicates > 0 {
		result.Warnings = append(result.Warnings, model.Warning{
			Code: "copied_responses", Count: duplicates,
			Message: "Copied fork history was detected and deduplicated by response ID in analytics.",
		})
	}
	if promptDuplicates > 0 {
		result.Warnings = append(result.Warnings, model.Warning{
			Code: "copied_prompts", Count: promptDuplicates,
			Message: "Copied fork history was detected and deduplicated by prompt event ID in analytics.",
		})
	}
	if conflicts > 0 {
		result.Warnings = append(result.Warnings, model.Warning{
			Code: "conflicting_usage", Count: conflicts,
			Message: "Some copied response IDs carried conflicting usage; analytics retain one concrete source record per ID.",
		})
	}
	if len(result.Warnings) > 0 {
		result.Status = "supported with warnings"
	}
}

type record struct {
	UUID                    string          `json:"uuid"`
	Type                    string          `json:"type"`
	Timestamp               string          `json:"timestamp"`
	SessionID               string          `json:"sessionId"`
	CWD                     string          `json:"cwd"`
	IsSidechain             bool            `json:"isSidechain"`
	IsMeta                  bool            `json:"isMeta"`
	IsCompactSummary        bool            `json:"isCompactSummary"`
	IsVisibleTranscriptOnly bool            `json:"isVisibleInTranscriptOnly"`
	SourceToolAssistantUUID string          `json:"sourceToolAssistantUUID"`
	ToolUseResult           json.RawMessage `json:"toolUseResult"`
	Message                 message         `json:"message"`
}

type message struct {
	ID      string          `json:"id"`
	Role    string          `json:"role"`
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
	Usage   *usage          `json:"usage"`
}

type usage struct {
	Input         int64 `json:"input_tokens"`
	Output        int64 `json:"output_tokens"`
	CacheRead     int64 `json:"cache_read_input_tokens"`
	CacheCreation int64 `json:"cache_creation_input_tokens"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
	ID   string `json:"id"`
}

func parseFile(ctx context.Context, path string) (model.Session, map[string]int, error) {
	session := model.Session{Harness: model.Claude, Models: make(map[string]model.ModelActivity), ActivityBasis: "session start"}
	warnings := make(map[string]int)
	seenAssistant := make(map[string]struct{})
	seenTools := make(map[string]struct{})
	err := provider.ForEachLine(ctx, path, maxRecordBytes, func(line []byte, tooLong bool) {
		if tooLong {
			warnings["oversize_record"]++
			return
		}
		var event record
		if err := json.Unmarshal(line, &event); err != nil {
			warnings["malformed_record"]++
			return
		}
		at, err := time.Parse(time.RFC3339Nano, event.Timestamp)
		if err == nil {
			if session.StartedAt.IsZero() || at.Before(session.StartedAt) {
				session.StartedAt = at
			}
			if session.EndedAt.IsZero() || at.After(session.EndedAt) {
				session.EndedAt = at
			}
		}
		if session.ID == "" {
			session.ID = event.SessionID
		}
		if session.Project == "" {
			session.Project = provider.ProjectName(event.CWD)
		}
		session.IsChild = session.IsChild || event.IsSidechain

		switch {
		case event.Type == "user" && event.Message.Role == "user":
			if event.IsMeta || event.IsCompactSummary || event.IsVisibleTranscriptOnly || event.SourceToolAssistantUUID != "" || hasJSONValue(event.ToolUseResult) {
				return
			}
			if metric, ok := promptMetric(event.Message.Content, at); ok {
				metric.EventID = event.UUID
				session.Prompts = append(session.Prompts, metric)
			}
		case event.Type == "assistant" && event.Message.Role == "assistant":
			if event.Message.ID != "" {
				if _, exists := seenAssistant[event.Message.ID]; exists {
					return
				}
				seenAssistant[event.Message.ID] = struct{}{}
			}
			recordedUsage := model.TokenUsage{}
			if event.Message.Usage != nil {
				recordedUsage = model.TokenUsage{
					Available:  true,
					Exact:      true,
					Input:      event.Message.Usage.Input,
					Output:     event.Message.Usage.Output,
					CacheRead:  event.Message.Usage.CacheRead,
					CacheWrite: event.Message.Usage.CacheCreation,
					Source:     "message.usage",
				}
			}
			session.Usage.Add(recordedUsage)
			modelName := provider.SafeLabel(event.Message.Model, 100)
			if modelName != "" {
				activity := session.Models[modelName]
				activity.Turns++
				activity.Usage.Add(recordedUsage)
				session.Models[modelName] = activity
			}
			call := model.CallMetric{ID: event.Message.ID, Model: modelName, Usage: recordedUsage}
			var blocks []contentBlock
			if json.Unmarshal(event.Message.Content, &blocks) == nil {
				for _, block := range blocks {
					if block.Type != "tool_use" {
						continue
					}
					if block.ID == "" {
						session.ToolCalls++
						call.ToolIDs = append(call.ToolIDs, "")
						continue
					}
					if _, exists := seenTools[block.ID]; !exists {
						seenTools[block.ID] = struct{}{}
						session.ToolCalls++
						call.ToolIDs = append(call.ToolIDs, block.ID)
					}
				}
			}
			session.Calls = append(session.Calls, call)
		}
	})
	if session.ID == "" {
		session.ID = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	session.ActivityAt = session.StartedAt
	return session, warnings, err
}

func promptMetric(raw json.RawMessage, at time.Time) (model.PromptMetric, bool) {
	var text string
	hasAttachment := false
	if err := json.Unmarshal(raw, &text); err != nil {
		var blocks []contentBlock
		if json.Unmarshal(raw, &blocks) != nil {
			return model.PromptMetric{}, false
		}
		var parts []string
		for _, block := range blocks {
			if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
				parts = append(parts, block.Text)
			} else if block.Type == "image" || block.Type == "document" {
				hasAttachment = true
			}
		}
		text = strings.Join(parts, "\n")
	}
	if strings.TrimSpace(text) == "" {
		return model.PromptMetric{At: at, HasText: false}, hasAttachment
	}
	words, characters := provider.TextMetric(text)
	return model.PromptMetric{At: at, Words: words, Characters: characters, HasText: true}, true
}

func hasJSONValue(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed != "" && trimmed != "null" && trimmed != "{}"
}
