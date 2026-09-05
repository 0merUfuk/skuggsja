// Package claude reads Claude Code's primary and supplemental local history evidence.
package claude

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
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

// Reader discovers terminal/IDE histories plus Claude Desktop's embedded local
// agent histories and Claude Code's supplemental prompt/statistics indexes.
type Reader struct {
	// ExtraHomes contains verified native config roots, never paths read from an index.
	ExtraHomes         []string
	ProjectsDir        string
	HistoryFile        string
	StatsFile          string
	GlobalStateFile    string
	DesktopSessionsDir string
	CodeSessionsDir    string
}

func (Reader) Harness() model.Harness { return model.Claude }
func (Reader) DisplayName() string    { return "Claude Code" }

func (r Reader) discoverConfigured(_ context.Context) (provider.Discovery, error) {
	d := provider.Discovery{Harness: model.Claude}
	// Declare configured locations before any filesystem operation can fail.
	// Generate must protect them even when detailed discovery is unavailable.
	for _, root := range []string{r.ProjectsDir, r.DesktopSessionsDir, r.CodeSessionsDir} {
		if root != "" {
			d.Roots = append(d.Roots, root)
		}
	}
	configured := make(map[string]struct{})
	for _, path := range []string{r.HistoryFile, r.StatsFile, r.GlobalStateFile} {
		if path != "" {
			d.ConfiguredFiles = append(d.ConfiguredFiles, path)
			configured[path] = struct{}{}
		}
	}
	if r.ProjectsDir != "" {
		if err := discoverProjectRoot(r.ProjectsDir, &d.Files); err != nil {
			return d, err
		}
		projectIndexes, err := discoverProjectSessionIndexes(r.ProjectsDir)
		if err != nil {
			return d, err
		}
		d.AuditFiles = append(d.AuditFiles, projectIndexes...)
		d.ConfiguredFiles = append(d.ConfiguredFiles, projectIndexes...)
	}
	if r.CodeSessionsDir != "" {
		indexFiles, err := discoverCodeSessionIndexes(r.CodeSessionsDir)
		if err != nil {
			return d, err
		}
		d.AuditFiles = append(d.AuditFiles, indexFiles...)
		d.ConfiguredFiles = append(d.ConfiguredFiles, indexFiles...)
	}
	embeddedRoots, err := embeddedProjectRoots(r.DesktopSessionsDir)
	if err != nil {
		return d, err
	}
	for _, root := range embeddedRoots {
		var embeddedFiles []string
		if err := discoverProjectRoot(root, &embeddedFiles); err != nil {
			return d, err
		}
		// These are Claude Desktop Cowork/local-agent transcripts that happen
		// to use the Claude Code JSONL engine. Audit and report their exclusion,
		// but do not misclassify them as terminal/IDE Claude Code usage.
		d.AuditFiles = append(d.AuditFiles, embeddedFiles...)
		d.ConfiguredFiles = append(d.ConfiguredFiles, embeddedFiles...)
	}
	supplemental := []string{r.HistoryFile, r.StatsFile}
	stateCandidates, err := globalStateCandidates(r.GlobalStateFile)
	if err != nil {
		return d, err
	}
	supplemental = append(supplemental, stateCandidates...)
	seenSupplemental := make(map[string]struct{})
	for _, path := range supplemental {
		if path == "" {
			continue
		}
		if _, exists := seenSupplemental[path]; exists {
			continue
		}
		seenSupplemental[path] = struct{}{}
		if _, exists := configured[path]; !exists {
			d.ConfiguredFiles = append(d.ConfiguredFiles, path)
			configured[path] = struct{}{}
		}
		present, err := optionalRegularFile(path)
		if err != nil {
			return d, err
		}
		if present {
			d.AuditFiles = append(d.AuditFiles, path)
		}
	}
	sort.Strings(d.Roots)
	sort.Strings(d.Files)
	sort.Strings(d.AuditFiles)
	sort.Strings(d.ConfiguredFiles)
	return d, nil
}

func globalStateCandidates(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	candidates := []string{path}
	patterns := []string{
		path + ".backup*",
		path + ".bak-*",
		filepath.Join(filepath.Dir(path), ".claude", "backups", ".claude.json.backup.*"),
		filepath.Join(filepath.Dir(path), "backups", ".claude.json.backup.*"),
		filepath.Join(filepath.Dir(path), ".pencil-cleanup-backup-*", "*-.claude.json.backup"),
	}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, matches...)
	}
	return candidates, nil
}

func discoverCodeSessionIndexes(root string) ([]string, error) {
	if root == "" {
		return nil, nil
	}
	info, err := os.Stat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil || !info.IsDir() {
		return nil, err
	}
	var files []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "local_") || filepath.Ext(entry.Name()) != ".json" {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("Claude Code session index is a symbolic link")
		}
		files = append(files, path)
		return nil
	})
	sort.Strings(files)
	return files, err
}

func discoverProjectRoot(root string, files *[]string) error {
	info, err := os.Stat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("Claude session source is a symbolic link")
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		parts := strings.Split(rel, string(filepath.Separator))
		isRootTranscript := len(parts) == 2
		isChildTranscript := len(parts) >= 4 && parts[2] == "subagents" &&
			strings.HasPrefix(entry.Name(), "agent-")
		if isRootTranscript || isChildTranscript {
			*files = append(*files, path)
		}
		return nil
	})
}

func embeddedProjectRoots(root string) ([]string, error) {
	if root == "" {
		return nil, nil
	}
	info, err := os.Stat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil || !info.IsDir() {
		return nil, err
	}
	var roots []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && entry.Name() == "projects" && filepath.Base(filepath.Dir(path)) == ".claude" {
			roots = append(roots, path)
			return fs.SkipDir
		}
		return nil
	})
	sort.Strings(roots)
	return roots, err
}

func optionalRegularFile(path string) (bool, error) {
	if path == "" {
		return false, nil
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, errors.New("Claude supplemental source is a symbolic link")
	}
	return info.Mode().IsRegular(), nil
}

func (r Reader) Read(ctx context.Context, d provider.Discovery) model.ProviderResult {
	result := model.ProviderResult{
		Harness:           model.Claude,
		DisplayName:       "Claude Code",
		Status:            "not found",
		VerificationLevel: "real data on macOS",
		SourceFiles:       append(append([]string(nil), d.Files...), d.AuditFiles...),
		Limitations: []string{
			"Prompt-history rows are suppressed when the session has detailed history; this avoids ambiguous duplicate/UI-command counts but can omit older prompts in a partial transcript.",
			"Child-agent transcripts are counted separately and excluded from owner prompt, usage, model, project, and rhythm totals.",
			"Claude Desktop Cowork/local-agent transcripts are audited but excluded from Claude Code usage totals.",
			"A session whose primary transcript is absent may still have copied events elsewhere; prompt-history entries themselves add no model, token, tool, or response detail, and displayed text may be abbreviated.",
			"Model events are deduplicated assistant API messages, not a cross-harness turn unit.",
			"Reasoning counts use message.usage.output_tokens_details.thinking_tokens when Claude Code recorded it.",
		},
	}
	if len(result.SourceFiles) == 0 {
		return result
	}
	result.Status = "supported"
	desktopExcluded := 0
	for _, path := range d.AuditFiles {
		if pathWithinRoot(r.DesktopSessionsDir, path) {
			desktopExcluded++
		}
	}
	addWarningCount(&result, "desktop_local_agent_sessions_excluded", claudeWarningMessage("desktop_local_agent_sessions_excluded"), desktopExcluded)

	type parsed struct {
		path              string
		session           model.Session
		ownerPromptStamps []promptEvidence
		earliestRecord    time.Time
		warnings          map[string]int
		err               error
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
				session, ownerPromptStamps, earliestRecord, warnings, err := parseFile(ctx, path)
				parsedFiles <- parsed{path: path, session: session, ownerPromptStamps: ownerPromptStamps, earliestRecord: earliestRecord, warnings: warnings, err: err}
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

	unanchoredIDs := make(map[string]struct{})
	var allPromptEvidence []promptEvidence
	var earliestUnanchored time.Time
	var earliestDetailedRecord time.Time
	var orderedFiles []parsed
	for item := range parsedFiles {
		orderedFiles = append(orderedFiles, item)
	}
	sort.Slice(orderedFiles, func(i, j int) bool { return orderedFiles[i].path < orderedFiles[j].path })
	for _, item := range orderedFiles {
		if item.err != nil {
			result.AddWarning("unreadable_file", "Some session files could not be read and were skipped.")
			continue
		}
		for code, count := range item.warnings {
			addWarningCount(&result, code, claudeWarningMessage(code), count)
		}
		if strings.HasPrefix(item.session.ActivityBasis, "file modification time") {
			item.session.Unanchored = true
			if !item.session.IsChild && item.session.ID != "" {
				unanchoredIDs[item.session.ID] = struct{}{}
			}
			if !item.session.StartedAt.IsZero() && (earliestUnanchored.IsZero() || item.session.StartedAt.Before(earliestUnanchored)) {
				earliestUnanchored = item.session.StartedAt
			}
		}
		if !item.session.IsChild {
			if !item.earliestRecord.IsZero() && (earliestDetailedRecord.IsZero() || item.earliestRecord.Before(earliestDetailedRecord)) {
				earliestDetailedRecord = item.earliestRecord
			}
			allPromptEvidence = append(allPromptEvidence, item.ownerPromptStamps...)
		}
		if !item.session.StartedAt.IsZero() || item.session.TimeUnavailable {
			result.Sessions = append(result.Sessions, item.session)
		}
	}
	result.Sessions = coalesceSessionCopies(result.Sessions, &result)
	// A valid physical copy can restore the anchor absent in another copy.
	unanchoredIDs = make(map[string]struct{})
	earliestUnanchored = time.Time{}
	for _, session := range result.Sessions {
		if !session.Unanchored {
			continue
		}
		if !session.IsChild && session.ID != "" {
			unanchoredIDs[session.ID] = struct{}{}
		}
		earliestUnanchored = earliestNonzero(earliestUnanchored, session.StartedAt)
	}
	result.ToolCallsAvailable = len(result.Sessions) > 0
	sort.Slice(allPromptEvidence, func(i, j int) bool {
		if allPromptEvidence[i].EventID != allPromptEvidence[j].EventID {
			return allPromptEvidence[i].EventID < allPromptEvidence[j].EventID
		}
		if allPromptEvidence[i].Stamp.SessionID != allPromptEvidence[j].Stamp.SessionID {
			return allPromptEvidence[i].Stamp.SessionID < allPromptEvidence[j].Stamp.SessionID
		}
		return allPromptEvidence[i].Stamp.UnixMillis < allPromptEvidence[j].Stamp.UnixMillis
	})
	ownerPromptStamps := make(map[promptStamp]int)
	seenPromptEvidenceIDs := make(map[string]struct{})
	for _, evidence := range allPromptEvidence {
		if evidence.EventID != "" {
			if _, duplicate := seenPromptEvidenceIDs[evidence.EventID]; duplicate {
				continue
			}
			seenPromptEvidenceIDs[evidence.EventID] = struct{}{}
		}
		ownerPromptStamps[evidence.Stamp]++
	}
	earliestDetailed := earliestDetailedRecord
	rootIDs := make(map[string]struct{})
	for _, session := range result.Sessions {
		if !session.IsChild && !session.Unanchored && session.ID != "" {
			rootIDs[session.ID] = struct{}{}
		}
	}
	historySessions := r.readHistorySources(ctx, d, &result)
	reconciledRecords, copiedReconciledRecords := 0, 0
	for _, session := range historySessions {
		_, matchedDetailed := rootIDs[session.ID]
		rootIDs[session.ID] = struct{}{}
		if matchedDetailed {
			reconciledRecords += len(session.Prompts)
			continue
		}
		retained := session.Prompts[:0]
		for _, prompt := range session.Prompts {
			stamp := promptStamp{SessionID: session.ID, UnixMillis: prompt.At.UnixMilli()}
			if survivingCopies := ownerPromptStamps[stamp]; survivingCopies > 0 {
				copiedReconciledRecords++
				ownerPromptStamps[stamp] = survivingCopies - 1
				continue
			}
			retained = append(retained, prompt)
		}
		session.Prompts = retained
		result.Sessions = append(result.Sessions, session)
		result.Coverage.HistoryOnlySessions++
	}
	addWarningCount(&result, "history_records_reconciled", claudeWarningMessage("history_records_reconciled"), reconciledRecords)
	addWarningCount(&result, "copied_history_prompt_records_reconciled", claudeWarningMessage("copied_history_prompt_records_reconciled"), copiedReconciledRecords)
	addWarningCount(&result, "history_only_sessions", claudeWarningMessage("history_only_sessions"), result.Coverage.HistoryOnlySessions)
	unmaterializedIDs := make(map[string]struct{}, len(unanchoredIDs))
	for id := range unanchoredIDs {
		if _, recoveredInHistory := rootIDs[id]; !recoveredInHistory {
			unmaterializedIDs[id] = struct{}{}
		}
	}
	if !earliestUnanchored.IsZero() {
		result.Coverage.EarliestLocalEvidence = earliestUnanchored
	}
	var codeSessionPaths []string
	for _, path := range d.AuditFiles {
		if pathWithinRoot(r.CodeSessionsDir, path) {
			codeSessionPaths = append(codeSessionPaths, path)
		}
	}
	if len(codeSessionPaths) > 0 {
		indexIDs, earliest, opaqueReferences, malformed, oversize := readCodeSessionIndexes(codeSessionPaths)
		addWarningCount(&result, "malformed_code_session_index", claudeWarningMessage("malformed_code_session_index"), malformed)
		addWarningCount(&result, "oversize_code_session_index", claudeWarningMessage("oversize_code_session_index"), oversize)
		if !earliest.IsZero() && (result.Coverage.EarliestLocalEvidence.IsZero() || earliest.Before(result.Coverage.EarliestLocalEvidence)) {
			result.Coverage.EarliestLocalEvidence = earliest
		}
		reconciled, unmaterialized := 0, 0
		for id := range indexIDs {
			if _, exists := rootIDs[id]; exists {
				reconciled++
			} else {
				unmaterializedIDs[id] = struct{}{}
				unmaterialized++
			}
		}
		addWarningCount(&result, "code_session_index_reconciled", claudeWarningMessage("code_session_index_reconciled"), reconciled)
		addWarningCount(&result, "code_session_index_unmaterialized", claudeWarningMessage("code_session_index_unmaterialized"), unmaterialized)
		addWarningCount(&result, "desktop_session_references_unmapped", claudeWarningMessage("desktop_session_references_unmapped"), opaqueReferences)
	}
	{
		statePaths := r.supplementalPaths(d, "state")
		if len(statePaths) > 0 {
			stateIDs, earliest, malformed, oversize := readGlobalStateIndexes(statePaths)
			addWarningCount(&result, "malformed_global_state", claudeWarningMessage("malformed_global_state"), malformed)
			addWarningCount(&result, "oversize_global_state", claudeWarningMessage("oversize_global_state"), oversize)
			if !earliest.IsZero() && (result.Coverage.EarliestLocalEvidence.IsZero() || earliest.Before(result.Coverage.EarliestLocalEvidence)) {
				result.Coverage.EarliestLocalEvidence = earliest
			}
			stateUnmaterialized := 0
			for id := range stateIDs {
				if _, exists := rootIDs[id]; !exists {
					if _, alreadyKnown := unmaterializedIDs[id]; !alreadyKnown {
						unmaterializedIDs[id] = struct{}{}
						stateUnmaterialized++
					}
				}
			}
			addWarningCount(&result, "global_state_only_sessions", claudeWarningMessage("global_state_only_sessions"), stateUnmaterialized)
		}
	}
	projectIndexPaths := r.supplementalPaths(d, "project-index")
	if len(projectIndexPaths) > 0 {
		indexIDs, earliest, malformed, oversize := readProjectSessionIndexes(projectIndexPaths)
		addWarningCount(&result, "malformed_project_session_index", claudeWarningMessage("malformed_project_session_index"), malformed)
		addWarningCount(&result, "oversize_project_session_index", claudeWarningMessage("oversize_project_session_index"), oversize)
		if !earliest.IsZero() && (result.Coverage.EarliestLocalEvidence.IsZero() || earliest.Before(result.Coverage.EarliestLocalEvidence)) {
			result.Coverage.EarliestLocalEvidence = earliest
		}
		newMissing := 0
		for id := range indexIDs {
			if _, recovered := rootIDs[id]; recovered {
				continue
			}
			if _, known := unmaterializedIDs[id]; known {
				continue
			}
			unmaterializedIDs[id] = struct{}{}
			newMissing++
		}
		addWarningCount(&result, "project_index_only_sessions", claudeWarningMessage("project_index_only_sessions"), newMissing)
	}
	result.Coverage.UnmaterializedSessions = len(unmaterializedIDs)
	result.Coverage.EarliestDetailedRecord = earliestDetailed
	recoveredStart := earliestRootSession(result.Sessions)
	for _, evidenceTime := range []time.Time{earliestDetailed, recoveredStart} {
		if !evidenceTime.IsZero() && (result.Coverage.EarliestLocalEvidence.IsZero() || evidenceTime.Before(result.Coverage.EarliestLocalEvidence)) {
			result.Coverage.EarliestLocalEvidence = evidenceTime
		}
	}
	for _, statsPath := range r.supplementalPaths(d, "stats") {
		first, oversize, err := readStatsStart(statsPath)
		if oversize {
			result.AddWarning("oversize_stats_cache", claudeWarningMessage("oversize_stats_cache"))
		} else if err != nil {
			result.AddWarning("malformed_stats_cache", "Claude Code's aggregate statistics cache could not be interpreted.")
		} else if !first.IsZero() {
			if result.Coverage.EarliestLocalEvidence.IsZero() || first.Before(result.Coverage.EarliestLocalEvidence) {
				result.Coverage.EarliestLocalEvidence = first
			}
			if !earliestDetailed.IsZero() && first.Before(earliestDetailed) {
				addWarningCount(&result, "aggregate_history_predates_detail", claudeWarningMessage("aggregate_history_predates_detail"), 1)
			}
		}
	}
	setClaudeCoverage(&result)
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
	sort.Slice(result.Warnings, func(i, j int) bool { return result.Warnings[i].Code < result.Warnings[j].Code })
	return result
}

func containsPath(paths []string, target string) bool {
	if target == "" {
		return false
	}
	for _, path := range paths {
		if path == target {
			return true
		}
	}
	return false
}

func earliestRootSession(sessions []model.Session) time.Time {
	var earliest time.Time
	for _, session := range sessions {
		if session.IsChild || session.StartedAt.IsZero() {
			continue
		}
		if earliest.IsZero() || session.StartedAt.Before(earliest) {
			earliest = session.StartedAt
		}
	}
	return earliest
}

func pathWithinRoot(root, path string) bool {
	if root == "" || path == "" {
		return false
	}
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func addWarningCount(result *model.ProviderResult, code, message string, count int) {
	if count <= 0 {
		return
	}
	for i := range result.Warnings {
		if result.Warnings[i].Code == code {
			result.Warnings[i].Count += count
			return
		}
	}
	result.Warnings = append(result.Warnings, model.Warning{Code: code, Count: count, Message: message})
}

func claudeWarningMessage(code string) string {
	return map[string]string{
		"project_index_only_sessions":              "Some session references survive only in project session indexes; they are coverage evidence and add no usage.",
		"malformed_project_session_index":          "Some project session indexes contain unsupported or malformed evidence; usable references are retained without inventing usage.",
		"oversize_project_session_index":           "Oversize project session indexes were skipped to keep memory use bounded.",
		"malformed_record":                         "Malformed records were skipped without modifying their source files.",
		"oversize_record":                          "Oversize records were skipped to keep memory use bounded.",
		"invalid_record_timestamp":                 "Owner or assistant records with missing or invalid timestamps were excluded from time-based evidence while other countable fields were retained.",
		"physical_session_time_unavailable":        "A physical session was retained, but none of its own records supplied a valid timestamp for span or rhythm metrics.",
		"unparseable_owner_prompt":                 "Otherwise eligible user records with unsupported message content could not contribute owner-prompt metrics.",
		"unparseable_assistant_content":            "Assistant records with unsupported nonempty content could not contribute tool-use blocks.",
		"owner_prompt_missing_id":                  "Owner prompt records without event UUIDs could not be safely deduplicated across copied histories.",
		"assistant_response_missing_id":            "Assistant records without response IDs could not be merged or safely deduplicated across copied histories.",
		"tool_use_missing_id":                      "Tool-use blocks without IDs were counted individually and could not be deduplicated.",
		"duplicate_tool_use":                       "Repeated tool-use IDs inside surviving transcripts were counted once.",
		"assistant_updates_merged":                 "Repeated assistant update records were merged by response ID while retaining distinct tool calls.",
		"assistant_usage_advanced":                 "Repeated assistant update records advanced cumulative output usage; the last complete snapshot was retained.",
		"assistant_usage_completed":                "A repeated assistant update added the first complete usage snapshot; that source-recorded snapshot was retained.",
		"assistant_update_usage_conflict":          "Repeated assistant update records changed non-cumulative usage fields; the last complete snapshot was retained.",
		"assistant_update_model_conflict":          "Repeated assistant update records changed model identity; the last nonempty model was retained.",
		"unanchored_session":                       "A transcript contained no record for its physical session ID; filesystem modification time anchors coverage only, while inherited records remain eligible for content-free deduplication.",
		"malformed_history_record":                 "Malformed prompt-history records were skipped.",
		"oversize_history_record":                  "Oversize prompt-history records were skipped to keep memory use bounded.",
		"history_records_reconciled":               "Prompt-history records for sessions with a surviving detailed transcript were suppressed by session identity; partial transcripts may still omit unique prompts.",
		"copied_history_prompt_records_reconciled": "Prompt-history records matching copied detailed events by session ID and millisecond timestamp were suppressed to avoid double counting.",
		"history_only_sessions":                    "Some sessions have no surviving primary transcript; prompt-history entries add no model, token, tool, or response detail even when copied events survive elsewhere.",
		"aggregate_history_predates_detail":        "Claude Code's aggregate cache reports activity earlier than any surviving detailed transcript.",
		"malformed_global_state":                   "A Claude global-state snapshot could not be interpreted and was skipped.",
		"oversize_global_state":                    "An oversize Claude global-state snapshot was skipped to keep memory use bounded.",
		"global_state_only_sessions":               "Some session IDs survive only in allowlisted Claude global-state metadata and were not counted as usage.",
		"desktop_local_agent_sessions_excluded":    "Claude Desktop Cowork/local-agent transcripts were audited but excluded from Claude Code totals.",
		"meta_prompts_excluded":                    "Claude metadata user records were excluded from owner prompt metrics.",
		"source_tool_prompts_excluded":             "Tool-generated user records carrying sourceToolAssistantUUID were excluded from owner prompt metrics.",
		"tool_result_prompts_excluded":             "Tool-result user records were excluded from owner prompt metrics.",
		"compact_summary_prompts_excluded":         "Compaction-summary user records were excluded from owner prompt metrics.",
		"transcript_only_prompts_excluded":         "Transcript-only user records were excluded from owner prompt metrics.",
		"owner_prompt_records_excluded":            "User-role records matching one or more non-owner filters were excluded; detailed filter counts overlap and must not be summed.",
		"malformed_code_session_index":             "A Claude Desktop Code session index could not be interpreted and was skipped.",
		"oversize_code_session_index":              "An oversize Claude Desktop Code session index was skipped to keep memory use bounded.",
		"oversize_stats_cache":                     "An oversize Claude aggregate statistics cache was skipped to keep memory use bounded.",
		"code_session_index_reconciled":            "Claude Desktop Code session references with surviving local history were reconciled without double counting.",
		"code_session_index_unmaterialized":        "Claude Desktop Code session references without surviving local history were retained as coverage evidence only.",
		"desktop_session_references_unmapped":      "Desktop-native bridge or fork references use a different identity namespace and were not counted as missing Claude Code sessions.",
	}[code]
}

func setClaudeCoverage(result *model.ProviderResult) {
	coverage := &result.Coverage
	if coverage.HistoryOnlySessions > 0 || coverage.UnmaterializedSessions > 0 ||
		(!coverage.EarliestLocalEvidence.IsZero() && coverage.EarliestDetailedRecord.IsZero()) ||
		(!coverage.EarliestLocalEvidence.IsZero() && !coverage.EarliestDetailedRecord.IsZero() &&
			coverage.EarliestLocalEvidence.Before(coverage.EarliestDetailedRecord)) {
		coverage.Status = "known incomplete"
		coverage.Confidence = "high"
		coverage.Note = "Local evidence proves that detailed Claude Code history is incomplete. Counts describe recoverable data, not lifetime usage."
		return
	}
	if hasClaudeLossWarning(result.Warnings) {
		coverage.Status = "coverage assessment incomplete"
		coverage.Confidence = "low"
		coverage.Note = "One or more local sources could not be fully interpreted, so completeness cannot be established. Recovered counts may omit data."
		return
	}
	if coverage.EarliestLocalEvidence.IsZero() {
		coverage.Status = "no local evidence"
		coverage.Confidence = "high"
		coverage.Note = "No supported local Claude Code evidence was found."
		return
	}
	coverage.Status = "completeness unknown"
	coverage.Confidence = "medium"
	coverage.Note = "Surviving local records were read, but account-lifetime completeness cannot be established."
}

func hasClaudeLossWarning(warnings []model.Warning) bool {
	lossCodes := map[string]struct{}{
		"unreadable_file": {}, "malformed_record": {}, "oversize_record": {}, "invalid_record_timestamp": {},
		"physical_session_time_unavailable": {},
		"unparseable_owner_prompt":          {}, "unparseable_assistant_content": {}, "owner_prompt_missing_id": {},
		"assistant_response_missing_id": {}, "unanchored_session": {},
		"unreadable_history": {}, "malformed_history_record": {}, "oversize_history_record": {},
		"malformed_stats_cache": {}, "oversize_stats_cache": {}, "malformed_global_state": {}, "oversize_global_state": {},
		"malformed_code_session_index": {}, "oversize_code_session_index": {},
		"malformed_project_session_index": {}, "oversize_project_session_index": {},
	}
	for _, warning := range warnings {
		if _, exists := lossCodes[warning.Code]; exists && warning.Count > 0 {
			return true
		}
	}
	return false
}

func isGlobalStateCandidate(primary, candidate string) bool {
	if primary == "" || candidate == "" {
		return false
	}
	if candidate == primary || strings.HasPrefix(candidate, primary+".backup") || strings.HasPrefix(candidate, primary+".bak-") {
		return true
	}
	base := filepath.Base(candidate)
	return base != "" && (strings.HasPrefix(base, ".claude.json.backup.") || strings.HasSuffix(base, ".claude.json.backup"))
}

func readGlobalStateIndexes(paths []string) (map[string]struct{}, time.Time, int, int) {
	ids := make(map[string]struct{})
	var earliest time.Time
	malformed := 0
	oversize := 0
	for _, path := range paths {
		var state struct {
			Projects map[string]struct {
				LastSessionID       string `json:"lastSessionId"`
				LastSessionModified int64  `json:"lastSessionModified"`
				LastStartTime       int64  `json:"lastStartTime"`
			} `json:"projects"`
		}
		tooLong, decodeErr := provider.DecodeJSONFile(path, maxRecordBytes, &state)
		if tooLong {
			oversize++
			continue
		}
		if decodeErr != nil {
			malformed++
			continue
		}
		for _, project := range state.Projects {
			if project.LastSessionID != "" {
				ids[project.LastSessionID] = struct{}{}
			}
			for _, millis := range []int64{project.LastSessionModified, project.LastStartTime} {
				if millis <= 0 {
					continue
				}
				at := time.UnixMilli(millis).UTC()
				if earliest.IsZero() || at.Before(earliest) {
					earliest = at
				}
			}
		}
	}
	return ids, earliest, malformed, oversize
}

func readCodeSessionIndexes(paths []string) (map[string]struct{}, time.Time, int, int, int) {
	ids := make(map[string]struct{})
	opaqueIDs := make(map[string]struct{})
	var earliest time.Time
	malformed := 0
	oversize := 0
	for _, path := range paths {
		var index struct {
			CLISessionID       string   `json:"cliSessionId"`
			PriorCLISessionIDs []string `json:"priorCliSessionIds"`
			BridgeSessionIDs   []string `json:"bridgeSessionIds"`
			ForkedFromID       string   `json:"forkedFromSessionId"`
			CreatedAt          int64    `json:"createdAt"`
			LastActivityAt     int64    `json:"lastActivityAt"`
		}
		tooLong, decodeErr := provider.DecodeJSONFile(path, maxRecordBytes, &index)
		if tooLong {
			oversize++
			continue
		}
		if decodeErr != nil {
			malformed++
			continue
		}
		allIDs := append([]string{index.CLISessionID}, index.PriorCLISessionIDs...)
		for _, id := range allIDs {
			if id != "" {
				ids[id] = struct{}{}
			}
		}
		for _, id := range append(index.BridgeSessionIDs, index.ForkedFromID) {
			if id != "" {
				opaqueIDs[id] = struct{}{}
			}
		}
		for _, millis := range []int64{index.CreatedAt, index.LastActivityAt} {
			if millis <= 0 {
				continue
			}
			at := time.UnixMilli(millis).UTC()
			if earliest.IsZero() || at.Before(earliest) {
				earliest = at
			}
		}
	}
	return ids, earliest, len(opaqueIDs), malformed, oversize
}

type historyRecord struct {
	Display   string `json:"display"`
	Project   string `json:"project"`
	SessionID string `json:"sessionId"`
	Timestamp int64  `json:"timestamp"`
}

func parseHistory(ctx context.Context, path string) ([]model.Session, map[string]int, error) {
	byID := make(map[string]*model.Session)
	warnings := make(map[string]int)
	occurrences := make(map[string]int)
	err := provider.ForEachLine(ctx, path, maxRecordBytes, func(line []byte, tooLong bool) {
		if tooLong {
			warnings["oversize_history_record"]++
			return
		}
		var record historyRecord
		if json.Unmarshal(line, &record) != nil || record.SessionID == "" || record.Timestamp <= 0 {
			warnings["malformed_history_record"]++
			return
		}
		at := time.UnixMilli(record.Timestamp).UTC()
		session := byID[record.SessionID]
		if session == nil {
			session = &model.Session{
				Harness: model.Claude, ID: record.SessionID, Project: provider.ProjectName(record.Project),
				Models: make(map[string]model.ModelActivity), ActivityBasis: "prompt history timestamp", HistoryOnly: true,
			}
			byID[record.SessionID] = session
		}
		if session.StartedAt.IsZero() || at.Before(session.StartedAt) {
			session.StartedAt = at
		}
		if session.EndedAt.IsZero() || at.After(session.EndedAt) {
			session.EndedAt = at
		}
		session.ActivityAt = session.StartedAt
		words, characters := provider.TextMetric(record.Display)
		stamp := fmt.Sprintf("history:%s:%d:%x", record.SessionID, record.Timestamp, sha256.Sum256([]byte(record.Display)))
		occurrences[stamp]++
		session.Prompts = append(session.Prompts, model.PromptMetric{
			EventID: fmt.Sprintf("%s:%d", stamp, occurrences[stamp]), At: at,
			Words: words, Characters: characters, HasText: strings.TrimSpace(record.Display) != "",
		})
	})
	if err != nil {
		return nil, warnings, err
	}
	sessions := make([]model.Session, 0, len(byID))
	for _, session := range byID {
		sessions = append(sessions, *session)
	}
	return sessions, warnings, nil
}

func readStatsStart(path string) (time.Time, bool, error) {
	var cache struct {
		FirstSessionDate string `json:"firstSessionDate"`
	}
	tooLong, err := provider.DecodeJSONFile(path, maxRecordBytes, &cache)
	if tooLong || err != nil {
		return time.Time{}, tooLong, err
	}
	if cache.FirstSessionDate == "" {
		return time.Time{}, false, nil
	}
	first, err := time.Parse(time.RFC3339Nano, cache.FirstSessionDate)
	return first, false, err
}

func annotateCopiedHistory(result *model.ProviderResult) {
	seenCalls := make(map[string]model.CallMetric)
	seenPrompts := make(map[string]struct{})
	duplicates, usageConflicts, modelConflicts, promptDuplicates := 0, 0, 0, 0
	for _, session := range result.Sessions {
		if session.IsChild {
			continue
		}
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
			if previous.Usage != call.Usage {
				usageConflicts++
			}
			if previous.Model != call.Model {
				modelConflicts++
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
			Message: "Copied fork history was deduplicated by response ID; tool IDs are unioned while the first deterministic usage/model record is retained.",
		})
	}
	if promptDuplicates > 0 {
		result.Warnings = append(result.Warnings, model.Warning{
			Code: "copied_prompts", Count: promptDuplicates,
			Message: "Copied fork history was detected and deduplicated by prompt event ID in analytics.",
		})
	}
	if usageConflicts > 0 {
		result.Warnings = append(result.Warnings, model.Warning{
			Code: "conflicting_usage", Count: usageConflicts,
			Message: "Some copied response IDs carried conflicting usage; analytics retain one concrete source record per ID.",
		})
	}
	if modelConflicts > 0 {
		result.Warnings = append(result.Warnings, model.Warning{
			Code: "conflicting_copied_models", Count: modelConflicts,
			Message: "Some copied response IDs carried conflicting model labels; analytics retain the first deterministic source record per ID.",
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
	AgentID                 string          `json:"agentId"`
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
	OutputDetails struct {
		Thinking int64 `json:"thinking_tokens"`
	} `json:"output_tokens_details"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
	ID   string `json:"id"`
}

type promptStamp struct {
	SessionID  string
	UnixMillis int64
}

type promptEvidence struct {
	Stamp   promptStamp
	EventID string
}

func parseFile(ctx context.Context, path string) (model.Session, []promptEvidence, time.Time, map[string]int, error) {
	physicalID := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	isChildPath := strings.HasPrefix(physicalID, "agent-") && pathHasComponent(path, "subagents")
	if isChildPath {
		physicalID = strings.TrimPrefix(physicalID, "agent-")
	}
	session := model.Session{
		Harness: model.Claude, ID: physicalID, IsChild: isChildPath,
		ParentID: parentSessionIDFromChildPath(path), Models: make(map[string]model.ModelActivity), ActivityBasis: "physical session record",
	}
	warnings := make(map[string]int)
	var ownerPromptStamps []promptEvidence
	assistantCallIndex := make(map[string]int)
	seenTools := make(map[string]struct{})
	var anchoredStart, anchoredEnd, earliestRecord time.Time
	sawPhysicalRecord := false
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
		at, timestampErr := time.Parse(time.RFC3339Nano, event.Timestamp)
		if timestampErr != nil && ((event.Type == "user" && event.Message.Role == "user") || (event.Type == "assistant" && event.Message.Role == "assistant")) {
			warnings["invalid_record_timestamp"]++
		}
		if timestampErr == nil && !isChildPath && (earliestRecord.IsZero() || at.Before(earliestRecord)) {
			earliestRecord = at
		}
		belongsToPhysical := event.SessionID == physicalID || (isChildPath && (event.AgentID == physicalID || event.AgentID == ""))
		sawPhysicalRecord = sawPhysicalRecord || belongsToPhysical
		if belongsToPhysical && event.IsSidechain {
			session.IsChild = true
		}
		if timestampErr == nil && belongsToPhysical {
			if anchoredStart.IsZero() || at.Before(anchoredStart) {
				anchoredStart = at
			}
			if anchoredEnd.IsZero() || at.After(anchoredEnd) {
				anchoredEnd = at
			}
		}
		if isChildPath && belongsToPhysical {
			session.IsChild = true
			if session.ParentID == "" && event.SessionID != physicalID {
				session.ParentID = event.SessionID
			}
		}
		if belongsToPhysical && session.Project == "" {
			session.Project = provider.ProjectName(event.CWD)
		}
		switch {
		case event.Type == "user" && event.Message.Role == "user":
			excluded := false
			if event.IsMeta {
				warnings["meta_prompts_excluded"]++
				excluded = true
			}
			if event.SourceToolAssistantUUID != "" {
				warnings["source_tool_prompts_excluded"]++
				excluded = true
			}
			if hasJSONValue(event.ToolUseResult) {
				warnings["tool_result_prompts_excluded"]++
				excluded = true
			}
			if event.IsCompactSummary {
				warnings["compact_summary_prompts_excluded"]++
				excluded = true
			}
			if event.IsVisibleTranscriptOnly {
				warnings["transcript_only_prompts_excluded"]++
				excluded = true
			}
			if excluded {
				warnings["owner_prompt_records_excluded"]++
				return
			}
			metric, ok, parseable := promptMetric(event.Message.Content, at)
			if !parseable {
				warnings["unparseable_owner_prompt"]++
			}
			if ok {
				metric.EventID = event.UUID
				session.Prompts = append(session.Prompts, metric)
				if event.UUID == "" {
					warnings["owner_prompt_missing_id"]++
				}
				if event.SessionID != "" && timestampErr == nil {
					ownerPromptStamps = append(ownerPromptStamps, promptEvidence{
						Stamp: promptStamp{SessionID: event.SessionID, UnixMillis: at.UnixMilli()}, EventID: event.UUID,
					})
				}
			}
		case event.Type == "assistant" && event.Message.Role == "assistant":
			recordedUsage := model.TokenUsage{}
			if event.Message.Usage != nil {
				recordedUsage = model.TokenUsage{
					Available:  true,
					Exact:      true,
					Input:      event.Message.Usage.Input,
					Output:     event.Message.Usage.Output,
					CacheRead:  event.Message.Usage.CacheRead,
					CacheWrite: event.Message.Usage.CacheCreation,
					Reasoning:  event.Message.Usage.OutputDetails.Thinking,
					Source:     "message.usage",
				}
			}
			modelName := provider.SafeLabel(event.Message.Model, 100)
			callIndex, duplicate := assistantCallIndex[event.Message.ID]
			if event.Message.ID == "" {
				duplicate = false
				warnings["assistant_response_missing_id"]++
			}
			if !duplicate {
				session.Calls = append(session.Calls, model.CallMetric{ID: event.Message.ID, Model: modelName, Usage: recordedUsage})
				callIndex = len(session.Calls) - 1
				if event.Message.ID != "" {
					assistantCallIndex[event.Message.ID] = callIndex
				}
			} else {
				warnings["assistant_updates_merged"]++
				previous := session.Calls[callIndex]
				if previous.Usage != recordedUsage {
					if !previous.Usage.Available && recordedUsage.Available {
						warnings["assistant_usage_completed"]++
					} else if cumulativeUsageAdvance(previous.Usage, recordedUsage) {
						warnings["assistant_usage_advanced"]++
					} else {
						warnings["assistant_update_usage_conflict"]++
					}
					if recordedUsage.Available {
						session.Calls[callIndex].Usage = recordedUsage
					}
				}
				if modelName != "" && previous.Model != "" && previous.Model != modelName {
					warnings["assistant_update_model_conflict"]++
				}
				if modelName != "" {
					session.Calls[callIndex].Model = modelName
				}
			}
			var blocks []contentBlock
			contentErr := json.Unmarshal(event.Message.Content, &blocks)
			if contentErr != nil && hasJSONValue(event.Message.Content) {
				warnings["unparseable_assistant_content"]++
			}
			if contentErr == nil {
				for _, block := range blocks {
					if block.Type != "tool_use" {
						continue
					}
					if block.ID == "" {
						session.ToolCalls++
						session.Calls[callIndex].ToolIDs = append(session.Calls[callIndex].ToolIDs, "")
						warnings["tool_use_missing_id"]++
						continue
					}
					if _, exists := seenTools[block.ID]; !exists {
						seenTools[block.ID] = struct{}{}
						session.ToolCalls++
						session.Calls[callIndex].ToolIDs = append(session.Calls[callIndex].ToolIDs, block.ID)
					} else {
						warnings["duplicate_tool_use"]++
					}
				}
			}
		}
	})
	// Calls are the canonical per-response representation after repeated update
	// records have been merged. Rebuild totals from them once, avoiding both
	// first-update undercounts and streaming-update double counts.
	session.Usage = model.TokenUsage{}
	session.Models = make(map[string]model.ModelActivity)
	for _, call := range session.Calls {
		session.Usage.Add(call.Usage)
		if call.Model != "" {
			activity := session.Models[call.Model]
			activity.Turns++
			activity.Usage.Add(call.Usage)
			session.Models[call.Model] = activity
		}
	}
	session.StartedAt = anchoredStart
	session.EndedAt = anchoredEnd
	if session.StartedAt.IsZero() {
		if sawPhysicalRecord {
			session.TimeUnavailable = true
			session.ActivityBasis = "physical record timestamp unavailable"
			warnings["physical_session_time_unavailable"]++
		} else if info, statErr := os.Stat(path); statErr == nil {
			session.StartedAt = info.ModTime()
			session.EndedAt = info.ModTime()
			session.ActivityBasis = "file modification time (no physical-session record)"
			session.Unanchored = true
			warnings["unanchored_session"]++
		}
	}
	session.ActivityAt = session.StartedAt
	return session, ownerPromptStamps, earliestRecord, warnings, err
}

func cumulativeUsageAdvance(previous, current model.TokenUsage) bool {
	return previous.Available && current.Available && previous.Exact == current.Exact &&
		previous.Source == current.Source && previous.Input == current.Input &&
		previous.CacheRead == current.CacheRead && previous.CacheWrite == current.CacheWrite &&
		current.Output >= previous.Output && current.Reasoning >= previous.Reasoning
}

func pathHasComponent(path, component string) bool {
	for _, part := range strings.Split(filepath.Clean(path), string(filepath.Separator)) {
		if part == component {
			return true
		}
	}
	return false
}

func parentSessionIDFromChildPath(path string) string {
	parts := strings.Split(filepath.Clean(path), string(filepath.Separator))
	for index, part := range parts {
		if part == "subagents" && index > 0 {
			return parts[index-1]
		}
	}
	return ""
}

func promptMetric(raw json.RawMessage, at time.Time) (model.PromptMetric, bool, bool) {
	var text string
	hasAttachment := false
	if err := json.Unmarshal(raw, &text); err != nil {
		var blocks []contentBlock
		if json.Unmarshal(raw, &blocks) != nil {
			return model.PromptMetric{}, false, false
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
		return model.PromptMetric{At: at, HasText: false}, hasAttachment, true
	}
	words, characters := provider.TextMetric(text)
	return model.PromptMetric{At: at, Words: words, Characters: characters, HasText: true}, true, true
}

func hasJSONValue(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed != "" && trimmed != "null" && trimmed != "{}"
}
