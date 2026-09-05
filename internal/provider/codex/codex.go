// Package codex reads Codex rollout JSONL histories.
package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/0merUfuk/skuggsja/internal/model"
	"github.com/0merUfuk/skuggsja/internal/provider"
	"github.com/0merUfuk/skuggsja/internal/sqlitecopy"
	"github.com/klauspost/compress/zstd"
)

const maxRecordBytes = 64 << 20
const maxDecoderMemory = 256 << 20

// Reader discovers active and archived rollout files.
type Reader struct {
	SessionsDir           string
	ArchivedDir           string
	HistoryFile           string
	SessionIndexFile      string
	ExternalImportsFile   string
	StateDatabase         string
	CatalogDatabase       string
	ThreadHistoryDatabase string
}

func (Reader) Harness() model.Harness { return model.Codex }
func (Reader) DisplayName() string    { return "Codex" }

func (r Reader) Discover(_ context.Context) (provider.Discovery, error) {
	d := provider.Discovery{Harness: model.Codex, Roots: []string{r.SessionsDir, r.ArchivedDir}}
	// Keep every configured source protected even if an earlier discovery step
	// fails before reaching supplemental indexes or their SQLite sidecars.
	for _, path := range []string{r.HistoryFile, r.SessionIndexFile, r.ExternalImportsFile} {
		if path != "" {
			d.ConfiguredFiles = append(d.ConfiguredFiles, path)
		}
	}
	for _, path := range []string{r.StateDatabase, r.CatalogDatabase, r.ThreadHistoryDatabase} {
		if path != "" {
			d.ConfiguredFiles = append(d.ConfiguredFiles, codexSQLitePaths(path)...)
		}
	}
	selected := make(map[string]string)
	for _, root := range d.Roots {
		info, err := os.Stat(root)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return d, err
		}
		if !info.IsDir() {
			continue
		}
		if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !entry.IsDir() && isRolloutFile(entry.Name()) {
				if entry.Type()&os.ModeSymlink != 0 {
					return errors.New("Codex rollout source is a symbolic link")
				}
				plainPath := plainRolloutPath(path)
				current, exists := selected[plainPath]
				if !exists || isPlainRollout(path) && !isPlainRollout(current) {
					selected[plainPath] = path
				}
			}
			return nil
		}); err != nil {
			return d, err
		}
	}
	for _, path := range selected {
		d.Files = append(d.Files, path)
	}
	for _, path := range []string{r.HistoryFile, r.SessionIndexFile, r.ExternalImportsFile} {
		if path == "" {
			continue
		}
		present, err := optionalRegularFile(path)
		if err != nil {
			return d, err
		}
		if present {
			d.AuditFiles = append(d.AuditFiles, path)
		}
	}
	if r.StateDatabase != "" {
		present, err := optionalRegularFile(r.StateDatabase)
		if err != nil {
			return d, err
		}
		if present {
			d.AuditFiles = append(d.AuditFiles, r.StateDatabase)
		}
	}
	if r.CatalogDatabase != "" {
		present, err := optionalRegularFile(r.CatalogDatabase)
		if err != nil {
			return d, err
		}
		if present {
			d.AuditFiles = append(d.AuditFiles, r.CatalogDatabase)
		}
	}
	if r.ThreadHistoryDatabase != "" {
		present, err := optionalRegularFile(r.ThreadHistoryDatabase)
		if err != nil {
			return d, err
		}
		if present {
			d.AuditFiles = append(d.AuditFiles, r.ThreadHistoryDatabase)
		}
	}
	sort.Strings(d.Files)
	sort.Strings(d.AuditFiles)
	sort.Strings(d.ConfiguredFiles)
	return d, nil
}

func codexSQLitePaths(path string) []string {
	return []string{path, path + "-wal", path + "-shm", path + "-journal"}
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
		return false, errors.New("Codex supplemental source is a symbolic link")
	}
	return info.Mode().IsRegular(), nil
}

func isRolloutFile(name string) bool {
	return strings.HasPrefix(name, "rollout-") &&
		(strings.HasSuffix(name, ".jsonl") || strings.HasSuffix(name, ".jsonl.zst"))
}

func isPlainRollout(path string) bool {
	return strings.HasSuffix(path, ".jsonl")
}

func plainRolloutPath(path string) string {
	return strings.TrimSuffix(path, ".zst")
}

func (r Reader) Read(ctx context.Context, d provider.Discovery) model.ProviderResult {
	result := model.ProviderResult{
		Harness:           model.Codex,
		DisplayName:       "Codex",
		Status:            "not found",
		VerificationLevel: "real data on macOS",
		SourceFiles:       append(append([]string(nil), d.Files...), d.AuditFiles...),
		Limitations: []string{
			"Prompt-history rows are suppressed when the session has detailed history; this avoids ambiguous duplicate/UI-command counts but can omit older prompts in a partial transcript.",
			"Model events are unique turn-context IDs; exact session token totals cannot be attributed to individual models.",
			"Prompt-history-only sessions have no recoverable model, token, tool-call, response, or project detail.",
			"Remote ChatGPT catalog rows and account/workspace host keys are coverage context only and are excluded from local usage totals.",
		},
	}
	if len(result.SourceFiles) == 0 {
		return result
	}
	result.Status = "supported"
	if containsPath(d.AuditFiles, r.ThreadHistoryDatabase) {
		result.AddWarning("unparsed_thread_history_database", codexWarningMessage("unparsed_thread_history_database"))
	}

	plans, planWarnings := planSegments(ctx, d.Files)
	for code, count := range planWarnings {
		addWarningCount(&result, code, codexWarningMessage(code), count)
	}
	type parsed struct {
		path     string
		stitchID string
		session  model.Session
		warnings map[string]int
		err      error
	}
	jobs := make(chan segmentPlan)
	parsedFiles := make(chan parsed)
	workers := min(6, len(plans))
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for plan := range jobs {
				session, warnings, err := parseFile(ctx, plan.Path, plan.MaxOrdinalExclusive, plan.MaxBytesExclusive)
				parsedFiles <- parsed{path: plan.Path, stitchID: plan.StitchID, session: session, warnings: warnings, err: err}
			}
		}()
	}
	go func() {
		defer close(parsedFiles)
		for _, plan := range plans {
			jobs <- plan
		}
		close(jobs)
		wg.Wait()
	}()

	byID := make(map[string]model.Session)
	parsedItems := make([]parsed, 0, len(plans))
	for item := range parsedFiles {
		parsedItems = append(parsedItems, item)
	}
	sort.Slice(parsedItems, func(i, j int) bool {
		if (parsedItems[i].stitchID != "") != (parsedItems[j].stitchID != "") {
			return parsedItems[i].stitchID != ""
		}
		return parsedItems[i].path < parsedItems[j].path
	})
	stitchedResults := make(map[string]bool)
	for _, item := range parsedItems {
		if item.err != nil {
			result.AddWarning("unreadable_file", "Some rollout files could not be read and were skipped.")
			continue
		}
		for code, count := range item.warnings {
			addWarningCount(&result, code, codexWarningMessage(code), count)
		}
		if item.session.StartedAt.IsZero() {
			continue
		}
		key := item.session.ID
		if key == "" {
			key = item.session.StartedAt.String()
		}
		previous, exists := byID[key]
		if !exists {
			byID[key] = item.session
		} else if item.stitchID != "" && stitchedResults[key] {
			var duplicatePrompts int
			byID[key], duplicatePrompts = mergeSegments(previous, item.session)
			addWarningCount(&result, "duplicate_stitched_prompt", codexWarningMessage("duplicate_stitched_prompt"), duplicatePrompts)
		} else {
			addWarningCount(&result, "unvalidated_same_id_rollout_suppressed", codexWarningMessage("unvalidated_same_id_rollout_suppressed"), 1)
			if !stitchedResults[key] && prefer(item.session, previous) {
				byID[key] = item.session
			}
		}
		if item.stitchID != "" {
			stitchedResults[key] = true
		}
	}
	earliestDetailed := earliestDetailedRoot(byID)
	result.ToolCallsAvailable = len(byID) > 0
	if containsPath(d.AuditFiles, r.HistoryFile) {
		historySessions, historyWarnings, err := parseHistory(ctx, r.HistoryFile)
		if err != nil {
			result.AddWarning("unreadable_history", "Codex's prompt-history index could not be read.")
		} else {
			for code, count := range historyWarnings {
				addWarningCount(&result, code, codexWarningMessage(code), count)
			}
			reconciledRecords := 0
			for _, session := range historySessions {
				if existing, ok := byID[session.ID]; ok {
					reconciledRecords += len(session.Prompts)
					byID[session.ID] = existing
					continue
				}
				byID[session.ID] = session
				result.Coverage.HistoryOnlySessions++
			}
			addWarningCount(&result, "history_records_reconciled", codexWarningMessage("history_records_reconciled"), reconciledRecords)
			addWarningCount(&result, "history_only_sessions", codexWarningMessage("history_only_sessions"), result.Coverage.HistoryOnlySessions)
		}
	}
	result.Coverage.EarliestDetailedRecord = earliestDetailed
	result.Coverage.EarliestLocalEvidence = earliestRoot(byID)
	unmaterializedIDs := make(map[string]struct{})
	if containsPath(d.AuditFiles, r.SessionIndexFile) {
		indexIDs, earliest, warnings, err := parseSessionIndex(ctx, r.SessionIndexFile)
		if err != nil {
			result.AddWarning("unreadable_session_index", "Codex's session index could not be read.")
		} else {
			for code, count := range warnings {
				addWarningCount(&result, code, codexWarningMessage(code), count)
			}
			if !earliest.IsZero() && (result.Coverage.EarliestLocalEvidence.IsZero() || earliest.Before(result.Coverage.EarliestLocalEvidence)) {
				result.Coverage.EarliestLocalEvidence = earliest
			}
			indexOnly := 0
			for id := range indexIDs {
				if _, exists := byID[id]; !exists {
					unmaterializedIDs[id] = struct{}{}
					indexOnly++
				}
			}
			addWarningCount(&result, "index_only_sessions", codexWarningMessage("index_only_sessions"), indexOnly)
		}
	}
	if containsPath(d.AuditFiles, r.ExternalImportsFile) {
		importIDs, earliest, referencedSources, oversize, err := parseExternalImports(r.ExternalImportsFile)
		if oversize {
			result.AddWarning("oversize_external_imports", codexWarningMessage("oversize_external_imports"))
		} else if err != nil {
			result.AddWarning("malformed_external_imports", "Codex's external-session import index could not be interpreted.")
		} else {
			if !earliest.IsZero() && (result.Coverage.EarliestLocalEvidence.IsZero() || earliest.Before(result.Coverage.EarliestLocalEvidence)) {
				result.Coverage.EarliestLocalEvidence = earliest
			}
			unmaterializedImports := 0
			for id := range importIDs {
				if _, exists := byID[id]; !exists {
					unmaterializedIDs[id] = struct{}{}
					unmaterializedImports++
				}
			}
			addWarningCount(&result, "external_import_source_references", codexWarningMessage("external_import_source_references"), referencedSources)
			addWarningCount(&result, "unmaterialized_import_sessions", codexWarningMessage("unmaterialized_import_sessions"), unmaterializedImports)
		}
	}
	if containsPath(d.AuditFiles, r.StateDatabase) {
		evidence, err := readStateIndex(ctx, r.StateDatabase, r.SessionsDir, r.ArchivedDir)
		if err != nil {
			result.AddWarning("state_index_unavailable", "Codex's thread-state database could not be copied and queried safely.")
		} else {
			if !evidence.Earliest.IsZero() && (result.Coverage.EarliestLocalEvidence.IsZero() || evidence.Earliest.Before(result.Coverage.EarliestLocalEvidence)) {
				result.Coverage.EarliestLocalEvidence = evidence.Earliest
			}
			stateOnly := 0
			for id := range evidence.IDs {
				if _, exists := byID[id]; !exists {
					unmaterializedIDs[id] = struct{}{}
					stateOnly++
				}
			}
			addWarningCount(&result, "state_only_sessions", codexWarningMessage("state_only_sessions"), stateOnly)
			addWarningCount(&result, "missing_state_rollout_paths", codexWarningMessage("missing_state_rollout_paths"), evidence.MissingRolloutPaths)
			addWarningCount(&result, "uninspectable_state_rollout_paths", codexWarningMessage("uninspectable_state_rollout_paths"), evidence.UninspectableRolloutPaths)
			discoveredPaths := make(map[string]struct{}, len(d.Files))
			for _, path := range d.Files {
				discoveredPaths[normalizedLexicalPath(path)] = struct{}{}
			}
			existingUndiscovered := 0
			for _, path := range evidence.ExistingRolloutPaths {
				if _, discovered := discoveredPaths[normalizedLexicalPath(path)]; !discovered {
					existingUndiscovered++
				}
			}
			addWarningCount(&result, "existing_state_rollouts_not_discovered", codexWarningMessage("existing_state_rollouts_not_discovered"), existingUndiscovered)
			addWarningCount(&result, "state_rollouts_outside_configured_roots", codexWarningMessage("state_rollouts_outside_configured_roots"), evidence.OutsideConfiguredRoots)
		}
	}
	if containsPath(d.AuditFiles, r.CatalogDatabase) {
		evidence, err := readCatalogIndex(ctx, r.CatalogDatabase)
		if err != nil {
			result.AddWarning("catalog_index_unavailable", "Codex's app catalog database could not be copied and queried safely.")
		} else {
			if !evidence.EarliestLocal.IsZero() && (result.Coverage.EarliestLocalEvidence.IsZero() || evidence.EarliestLocal.Before(result.Coverage.EarliestLocalEvidence)) {
				result.Coverage.EarliestLocalEvidence = evidence.EarliestLocal
			}
			localCatalogOnly := 0
			for id := range evidence.LocalIDs {
				if _, exists := byID[id]; !exists {
					localCatalogOnly++
					if _, alreadyKnown := unmaterializedIDs[id]; !alreadyKnown {
						unmaterializedIDs[id] = struct{}{}
					}
				}
			}
			addWarningCount(&result, "account_workspace_catalogs", codexWarningMessage("account_workspace_catalogs"), evidence.AccountWorkspaceHosts)
			addWarningCount(&result, "remote_catalog_sessions_excluded", codexWarningMessage("remote_catalog_sessions_excluded"), evidence.RemoteSessions)
			addWarningCount(&result, "local_catalog_only_sessions", codexWarningMessage("local_catalog_only_sessions"), localCatalogOnly)
		}
	}
	result.Coverage.UnmaterializedSessions = len(unmaterializedIDs)
	setCodexCoverage(&result)
	for _, session := range byID {
		result.Sessions = append(result.Sessions, session)
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
	sort.Slice(result.Warnings, func(i, j int) bool { return result.Warnings[i].Code < result.Warnings[j].Code })
	return result
}

type segmentPlan struct {
	Path                string
	MaxOrdinalExclusive *int64
	MaxBytesExclusive   *int64
	StitchID            string
}

type segmentInfo struct {
	Path         string
	ID           string
	FirstOrdinal *int64
	HistoryBase  *historyBase
}

func planSegments(ctx context.Context, paths []string) ([]segmentPlan, map[string]int) {
	plans := make([]segmentPlan, 0, len(paths))
	planByPath := make(map[string]int, len(paths))
	groups := make(map[string][]segmentInfo)
	warnings := make(map[string]int)
	for _, path := range paths {
		plans = append(plans, segmentPlan{Path: path})
		planByPath[path] = len(plans) - 1
		info, err := inspectSegment(ctx, path)
		if err != nil {
			warnings["history_chain_inspection_failed"]++
			continue
		}
		if info.ID != "" {
			groups[info.ID] = append(groups[info.ID], info)
		}
	}
	for id, group := range groups {
		if len(group) < 2 {
			if len(group) == 1 && group[0].HistoryBase != nil {
				warnings["missing_history_base"]++
			}
			continue
		}
		sort.Slice(group, func(i, j int) bool {
			if group[i].FirstOrdinal == nil && group[j].FirstOrdinal == nil {
				return group[i].Path < group[j].Path
			}
			if group[i].FirstOrdinal == nil {
				return true
			}
			if group[j].FirstOrdinal == nil {
				return false
			}
			if *group[i].FirstOrdinal == *group[j].FirstOrdinal {
				return group[i].Path < group[j].Path
			}
			return *group[i].FirstOrdinal < *group[j].FirstOrdinal
		})
		valid := group[0].HistoryBase == nil && group[0].FirstOrdinal != nil
		for index := 1; valid && index < len(group); index++ {
			current := group[index]
			base := current.HistoryBase
			valid = current.FirstOrdinal != nil && base != nil && base.ThreadID == id &&
				base.EndOrdinalExclusive == *current.FirstOrdinal &&
				validateHistoryBoundary(group[index-1].Path, base.EndByteOffset, base.EndOrdinalExclusive)
			if valid {
				boundary := base.EndOrdinalExclusive
				byteBoundary := base.EndByteOffset
				planIndex := planByPath[group[index-1].Path]
				plans[planIndex].MaxOrdinalExclusive = &boundary
				plans[planIndex].MaxBytesExclusive = &byteBoundary
			}
		}
		if valid {
			for _, info := range group {
				planIndex := planByPath[info.Path]
				plans[planIndex].StitchID = id
			}
			warnings["stitched_history_segments"] += len(group) - 1
		} else {
			warnings["ambiguous_history_chain_suppressed"] += len(group) - 1
			for _, info := range group {
				planIndex := planByPath[info.Path]
				plans[planIndex].MaxOrdinalExclusive = nil
				plans[planIndex].MaxBytesExclusive = nil
			}
		}
	}
	return plans, warnings
}

func inspectSegment(ctx context.Context, path string) (segmentInfo, error) {
	line, err := firstRolloutLine(ctx, path)
	if err != nil {
		return segmentInfo{}, err
	}
	var event record
	if err := json.Unmarshal(line, &event); err != nil {
		return segmentInfo{}, err
	}
	if event.Type != "session_meta" {
		return segmentInfo{Path: path}, nil
	}
	id := event.Payload.ID
	if id == "" {
		id = event.Payload.SessionID
	}
	return segmentInfo{Path: path, ID: id, FirstOrdinal: event.Ordinal, HistoryBase: event.Payload.HistoryBase}, nil
}

func firstRolloutLine(ctx context.Context, path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var source io.Reader = f
	if strings.HasSuffix(path, ".jsonl.zst") {
		decoder, err := zstd.NewReader(f, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(maxDecoderMemory))
		if err != nil {
			return nil, err
		}
		defer decoder.Close()
		source = decoder
	}
	reader := bufio.NewReaderSize(io.LimitReader(source, maxRecordBytes+1), 256*1024)
	line, err := reader.ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(line) > maxRecordBytes {
		return nil, errors.New("first rollout record exceeds size limit")
	}
	return bytesTrimLineEnding(line), nil
}

func bytesTrimLineEnding(line []byte) []byte {
	line = bytes.TrimSuffix(line, []byte{'\n'})
	return bytes.TrimSuffix(line, []byte{'\r'})
}

func validateHistoryBoundary(path string, offset, ordinal int64) bool {
	if offset < 0 || strings.HasSuffix(path, ".zst") {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || offset > info.Size() {
		return false
	}
	if offset > 0 {
		previous := []byte{0}
		if _, err := f.ReadAt(previous, offset-1); err != nil || previous[0] != '\n' {
			return false
		}
	}
	if offset == 0 {
		if ordinal != 0 {
			return false
		}
	} else if previousOrdinal, ok := ordinalBeforeOffset(f, offset); !ok || previousOrdinal != ordinal-1 {
		return false
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return false
	}
	if offset == info.Size() {
		return true
	}
	line, err := bufio.NewReaderSize(io.LimitReader(f, maxRecordBytes+1), 256*1024).ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false
	}
	if len(line) > maxRecordBytes {
		return false
	}
	var event record
	return json.Unmarshal(bytesTrimLineEnding(line), &event) == nil && event.Ordinal != nil && *event.Ordinal == ordinal
}

func ordinalBeforeOffset(file *os.File, offset int64) (int64, bool) {
	window := int64(maxRecordBytes + 2)
	start := offset - window
	if start < 0 {
		start = 0
	}
	buffer := make([]byte, int(offset-start))
	if _, err := file.ReadAt(buffer, start); err != nil {
		return 0, false
	}
	buffer = bytes.TrimSuffix(buffer, []byte{'\n'})
	buffer = bytes.TrimSuffix(buffer, []byte{'\r'})
	lineStart := bytes.LastIndexByte(buffer, '\n') + 1
	if start > 0 && lineStart == 0 {
		return 0, false
	}
	line := buffer[lineStart:]
	if len(line) == 0 || len(line) > maxRecordBytes {
		return 0, false
	}
	var event record
	if json.Unmarshal(bytesTrimLineEnding(line), &event) != nil || event.Ordinal == nil {
		return 0, false
	}
	return *event.Ordinal, true
}

func mergeSegments(left, right model.Session) (model.Session, int) {
	if left.StartedAt.IsZero() || (!right.StartedAt.IsZero() && right.StartedAt.Before(left.StartedAt)) {
		left.StartedAt = right.StartedAt
		left.ActivityAt = right.ActivityAt
	}
	if right.EndedAt.After(left.EndedAt) {
		left.EndedAt = right.EndedAt
	}
	if left.Project == "" {
		left.Project = right.Project
	}
	left.IsChild = left.IsChild || right.IsChild
	if left.ParentID == "" {
		left.ParentID = right.ParentID
	}
	seenPrompts := make(map[string]struct{}, len(left.Prompts))
	duplicatePrompts := 0
	for _, prompt := range left.Prompts {
		if prompt.EventID != "" {
			seenPrompts[prompt.EventID] = struct{}{}
		}
	}
	for _, prompt := range right.Prompts {
		if prompt.EventID != "" {
			if _, exists := seenPrompts[prompt.EventID]; exists {
				duplicatePrompts++
				continue
			}
			seenPrompts[prompt.EventID] = struct{}{}
		}
		left.Prompts = append(left.Prompts, prompt)
	}
	left.ToolCalls += right.ToolCalls
	left.Usage.Add(right.Usage)
	for name, activity := range right.Models {
		current := left.Models[name]
		current.Turns += activity.Turns
		current.Usage.Add(activity.Usage)
		left.Models[name] = current
	}
	return left, duplicatePrompts
}

func prefer(candidate, current model.Session) bool {
	if candidate.EndedAt.After(current.EndedAt) {
		return true
	}
	return len(candidate.Prompts) > len(current.Prompts)
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

func pathWithinEitherRoot(path string, roots ...string) bool {
	for _, root := range roots {
		if root == "" {
			continue
		}
		relative, err := filepath.Rel(root, path)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
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

func codexWarningMessage(code string) string {
	return map[string]string{
		"malformed_record":                        "Malformed records were skipped without modifying their source files.",
		"oversize_record":                         "Oversize records were skipped to keep memory use bounded.",
		"invalid_timestamp":                       "Records with missing or invalid timestamps were excluded from time-based metrics; other countable metadata was retained.",
		"missing_or_invalid_session_meta":         "A rollout lacked trustworthy identity-and-time session metadata and was excluded from usage totals.",
		"duplicate_prompt_event":                  "Repeated prompt event IDs inside a rollout were counted once.",
		"duplicate_turn_context":                  "Repeated turn-context IDs inside a rollout were counted once.",
		"duplicate_tool_call":                     "Repeated tool-call IDs inside a rollout were counted once.",
		"unknown_history_mode":                    "A rollout used an unknown history mode; its prompts were skipped rather than risking duplicate counts.",
		"history_chain_inspection_failed":         "A rollout's segment metadata could not be inspected; ordinary single-file parsing was retained.",
		"stitched_history_segments":               "Paginated rollout segments were boundary-validated and stitched instead of dropping an earlier segment.",
		"ambiguous_history_chain_suppressed":      "Same-ID rollout files failed history-chain validation; one preferred physical file was retained and conflicting detail may be omitted.",
		"unvalidated_same_id_rollout_suppressed":  "An additional same-ID rollout outside a validated chain was suppressed after a deterministic preference comparison.",
		"duplicate_stitched_prompt":               "A prompt event repeated across validated history segments and was counted once by event ID.",
		"missing_history_base":                    "A paginated continuation referenced a base rollout that was not found; only the surviving partial segment was read.",
		"malformed_history_record":                "Malformed prompt-history records were skipped.",
		"oversize_history_record":                 "Oversize prompt-history records were skipped to keep memory use bounded.",
		"history_records_reconciled":              "Prompt-history records for sessions with surviving rollouts were suppressed by session identity; partial rollouts may still omit unique prompts.",
		"history_only_sessions":                   "Some sessions survive only in Codex's prompt-history index and lack rollout detail.",
		"malformed_session_index_record":          "Malformed session-index records were skipped.",
		"oversize_session_index_record":           "Oversize session-index records were skipped to keep memory use bounded.",
		"index_only_sessions":                     "Some indexed sessions have neither a surviving rollout nor a prompt-history record and were not counted as usage.",
		"external_import_source_references":       "External-session source-path strings were retained as aggregate reference evidence without probing untrusted paths.",
		"oversize_external_imports":               "An oversize external-session import index was skipped to keep memory use bounded.",
		"unmaterialized_import_sessions":          "Some imported thread IDs have no surviving Codex rollout and were not counted as usage.",
		"state_index_unavailable":                 "Codex's thread-state database could not be copied and queried safely.",
		"state_only_sessions":                     "Some thread-state rows have neither a surviving rollout nor a prompt-history record and were not counted as usage.",
		"missing_state_rollout_paths":             "Codex's thread-state database references rollout paths that no longer exist.",
		"uninspectable_state_rollout_paths":       "Some rollout paths referenced by Codex's thread-state database could not be inspected.",
		"existing_state_rollouts_not_discovered":  "Some existing rollout paths referenced by Codex state were not ingested by configured discovery rules.",
		"state_rollouts_outside_configured_roots": "Some state-referenced rollout paths are outside configured Codex roots; they were not opened and their existence was not inferred.",
		"catalog_index_unavailable":               "Codex's app catalog database could not be copied and queried safely.",
		"unparsed_thread_history_database":        "A local paginated thread-history database was audited but is not interpreted; its records are excluded from recovered usage.",
		"account_workspace_catalogs":              "Distinct ChatGPT account/workspace catalog hosts were observed; local rollouts cannot be attributed to them.",
		"remote_catalog_sessions_excluded":        "Remote ChatGPT catalog rows were excluded from local Codex usage totals.",
		"local_catalog_only_sessions":             "Some local app-catalog sessions have no surviving rollout or prompt-history record and were retained as coverage evidence only.",
	}[code]
}

func earliestDetailedRoot(sessions map[string]model.Session) time.Time {
	var earliest time.Time
	for _, session := range sessions {
		if session.IsChild || session.HistoryOnly || session.StartedAt.IsZero() {
			continue
		}
		if earliest.IsZero() || session.StartedAt.Before(earliest) {
			earliest = session.StartedAt
		}
	}
	return earliest
}

func earliestRoot(sessions map[string]model.Session) time.Time {
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

func setCodexCoverage(result *model.ProviderResult) {
	coverage := &result.Coverage
	if coverage.HistoryOnlySessions > 0 || coverage.UnmaterializedSessions > 0 ||
		hasWarningCode(result.Warnings, "missing_history_base") ||
		(!coverage.EarliestLocalEvidence.IsZero() && !coverage.EarliestDetailedRecord.IsZero() &&
			coverage.EarliestLocalEvidence.Before(coverage.EarliestDetailedRecord)) {
		coverage.Status = "known incomplete"
		coverage.Confidence = "high"
		coverage.Note = "Local evidence proves that some Codex rollout detail is missing or could not be fully read. Counts describe recoverable data, not lifetime usage."
		return
	}
	if hasCodexLossWarning(result.Warnings) {
		coverage.Status = "coverage assessment incomplete"
		coverage.Confidence = "low"
		coverage.Note = "One or more local sources could not be fully interpreted, so completeness cannot be established. Recovered counts may omit data."
		return
	}
	if coverage.EarliestLocalEvidence.IsZero() {
		coverage.Status = "no local evidence"
		coverage.Confidence = "high"
		coverage.Note = "No supported local Codex evidence was found."
		return
	}
	coverage.Status = "completeness unknown"
	coverage.Confidence = "medium"
	coverage.Note = "Surviving local records were read, but account-lifetime completeness cannot be established."
}

func hasCodexLossWarning(warnings []model.Warning) bool {
	lossCodes := map[string]struct{}{
		"unreadable_file": {}, "malformed_record": {}, "oversize_record": {}, "invalid_timestamp": {}, "missing_or_invalid_session_meta": {}, "unknown_history_mode": {},
		"history_chain_inspection_failed": {}, "ambiguous_history_chain_suppressed": {}, "unvalidated_same_id_rollout_suppressed": {}, "missing_history_base": {},
		"unreadable_history": {}, "malformed_history_record": {}, "oversize_history_record": {},
		"unreadable_session_index": {}, "malformed_session_index_record": {}, "oversize_session_index_record": {},
		"malformed_external_imports": {}, "oversize_external_imports": {}, "state_index_unavailable": {}, "uninspectable_state_rollout_paths": {},
		"catalog_index_unavailable": {}, "unparsed_thread_history_database": {},
		"existing_state_rollouts_not_discovered": {}, "state_rollouts_outside_configured_roots": {},
	}
	for _, warning := range warnings {
		if _, exists := lossCodes[warning.Code]; exists && warning.Count > 0 {
			return true
		}
	}
	return false
}

func hasWarningCode(warnings []model.Warning, code string) bool {
	for _, warning := range warnings {
		if warning.Code == code {
			return true
		}
	}
	return false
}

type promptHistoryRecord struct {
	SessionID string `json:"session_id"`
	Text      string `json:"text"`
	Timestamp int64  `json:"ts"`
}

func parseHistory(ctx context.Context, path string) ([]model.Session, map[string]int, error) {
	byID := make(map[string]*model.Session)
	warnings := make(map[string]int)
	lineNumber := 0
	err := provider.ForEachLine(ctx, path, maxRecordBytes, func(line []byte, tooLong bool) {
		lineNumber++
		if tooLong {
			warnings["oversize_history_record"]++
			return
		}
		var record promptHistoryRecord
		if json.Unmarshal(line, &record) != nil || record.SessionID == "" || record.Timestamp <= 0 {
			warnings["malformed_history_record"]++
			return
		}
		at := time.Unix(record.Timestamp, 0).UTC()
		session := byID[record.SessionID]
		if session == nil {
			session = &model.Session{
				Harness: model.Codex, ID: record.SessionID, Models: make(map[string]model.ModelActivity),
				ActivityBasis: "prompt history timestamp", HistoryOnly: true,
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
		text := strings.TrimSpace(record.Text)
		if isInjectedContext(text) {
			return
		}
		words, characters := provider.TextMetric(text)
		session.Prompts = append(session.Prompts, model.PromptMetric{
			EventID: fmt.Sprintf("history:%s:%d", record.SessionID, lineNumber), At: at,
			Words: words, Characters: characters, HasText: text != "",
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

type sessionIndexRecord struct {
	ID        string `json:"id"`
	UpdatedAt string `json:"updated_at"`
}

func parseSessionIndex(ctx context.Context, path string) (map[string]struct{}, time.Time, map[string]int, error) {
	ids := make(map[string]struct{})
	warnings := make(map[string]int)
	var earliest time.Time
	err := provider.ForEachLine(ctx, path, maxRecordBytes, func(line []byte, tooLong bool) {
		if tooLong {
			warnings["oversize_session_index_record"]++
			return
		}
		var record sessionIndexRecord
		if json.Unmarshal(line, &record) != nil || record.ID == "" {
			warnings["malformed_session_index_record"]++
			return
		}
		at, parseErr := time.Parse(time.RFC3339Nano, record.UpdatedAt)
		if parseErr != nil {
			warnings["malformed_session_index_record"]++
			return
		}
		ids[record.ID] = struct{}{}
		if earliest.IsZero() || at.Before(earliest) {
			earliest = at
		}
	})
	return ids, earliest, warnings, err
}

type stateIndexEvidence struct {
	IDs                       map[string]struct{}
	ExistingRolloutPaths      []string
	Earliest                  time.Time
	MissingRolloutPaths       int
	UninspectableRolloutPaths int
	OutsideConfiguredRoots    int
}

func readStateIndex(ctx context.Context, path string, roots ...string) (stateIndexEvidence, error) {
	evidence := stateIndexEvidence{IDs: make(map[string]struct{})}
	database, err := sqlitecopy.Open(ctx, path)
	if err != nil {
		return evidence, err
	}
	rows, queryErr := database.DB.QueryContext(ctx, `
		SELECT id, rollout_path, COALESCE(created_at_ms, created_at * 1000)
		FROM threads
	`)
	if queryErr != nil {
		return evidence, errors.Join(queryErr, database.Close())
	}
	for rows.Next() {
		var id, rolloutPath string
		var createdAtMillis int64
		if err := rows.Scan(&id, &rolloutPath, &createdAtMillis); err != nil {
			return evidence, errors.Join(err, rows.Close(), database.Close())
		}
		if id != "" {
			evidence.IDs[id] = struct{}{}
		}
		if createdAtMillis > 0 {
			at := time.UnixMilli(createdAtMillis).UTC()
			if evidence.Earliest.IsZero() || at.Before(evidence.Earliest) {
				evidence.Earliest = at
			}
		}
		if rolloutPath == "" {
			continue
		}
		resolvedPath := rolloutPath
		if !filepath.IsAbs(resolvedPath) {
			resolvedPath = filepath.Join(filepath.Dir(path), resolvedPath)
		}
		resolvedPath = filepath.Clean(resolvedPath)
		if unsafeSpecialPath(resolvedPath) || !pathWithinEitherRoot(resolvedPath, roots...) {
			evidence.OutsideConfiguredRoots++
			continue
		}
		switch inspectPathWithinRoots(resolvedPath, roots...) {
		case pathMissing:
			evidence.MissingRolloutPaths++
		case pathUninspectable:
			evidence.UninspectableRolloutPaths++
		case pathRegular:
			evidence.ExistingRolloutPaths = append(evidence.ExistingRolloutPaths, resolvedPath)
		}
	}
	readErr := rows.Err()
	rowsCloseErr := rows.Close()
	databaseCloseErr := database.Close()
	return evidence, errors.Join(readErr, rowsCloseErr, databaseCloseErr)
}

type pathInspection uint8

const (
	pathMissing pathInspection = iota
	pathUninspectable
	pathRegular
)

func inspectPathWithinRoots(path string, roots ...string) pathInspection {
	root := ""
	for _, candidate := range roots {
		if candidate != "" && pathWithinEitherRoot(path, candidate) && len(candidate) > len(root) {
			root = filepath.Clean(candidate)
		}
	}
	if root == "" {
		return pathUninspectable
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return pathUninspectable
	}
	rootInfo, rootErr := os.Lstat(root)
	if errors.Is(rootErr, os.ErrNotExist) {
		return pathMissing
	}
	if rootErr != nil || rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return pathUninspectable
	}
	current := root
	if relative == "." {
		return pathUninspectable
	}
	components := strings.Split(relative, string(filepath.Separator))
	for index, component := range components {
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			return pathMissing
		}
		if statErr != nil || info.Mode()&os.ModeSymlink != 0 {
			return pathUninspectable
		}
		if index < len(components)-1 && !info.IsDir() {
			return pathUninspectable
		}
		if index == len(components)-1 && !info.Mode().IsRegular() {
			return pathUninspectable
		}
	}
	return pathRegular
}

func unsafeSpecialPath(path string) bool {
	normalized := strings.ReplaceAll(path, "/", `\`)
	return strings.HasPrefix(normalized, `\\`) || strings.HasPrefix(normalized, `\\?\`) || strings.HasPrefix(normalized, `\\.\`)
}

func normalizedLexicalPath(path string) string {
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return filepath.Clean(path)
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return strings.ToLower(absolute)
	}
	return absolute
}

type catalogIndexEvidence struct {
	LocalIDs              map[string]struct{}
	EarliestLocal         time.Time
	AccountWorkspaceHosts int
	RemoteSessions        int
}

func readCatalogIndex(ctx context.Context, path string) (catalogIndexEvidence, error) {
	evidence := catalogIndexEvidence{LocalIDs: make(map[string]struct{})}
	database, err := sqlitecopy.Open(ctx, path)
	if err != nil {
		return evidence, err
	}
	rows, queryErr := database.DB.QueryContext(ctx, `
		SELECT c.thread_id, c.source_created_at, h.host_id, h.host_kind
		FROM local_thread_catalog AS c
		JOIN local_thread_catalog_hosts AS h ON h.host_id = c.host_id
	`)
	if queryErr != nil {
		return evidence, errors.Join(queryErr, database.Close())
	}
	accountHosts := make(map[string]struct{})
	for rows.Next() {
		var threadID, hostID, hostKind string
		var createdAt float64
		if err := rows.Scan(&threadID, &createdAt, &hostID, &hostKind); err != nil {
			return evidence, errors.Join(err, rows.Close(), database.Close())
		}
		switch hostKind {
		case "chatgpt":
			accountHosts[hostID] = struct{}{}
			evidence.RemoteSessions++
		case "local":
			if threadID != "" {
				evidence.LocalIDs[threadID] = struct{}{}
			}
			if createdAt > 0 {
				seconds := int64(createdAt)
				at := time.Unix(seconds, int64((createdAt-float64(seconds))*1e9)).UTC()
				if evidence.EarliestLocal.IsZero() || at.Before(evidence.EarliestLocal) {
					evidence.EarliestLocal = at
				}
			}
		}
	}
	evidence.AccountWorkspaceHosts = len(accountHosts)
	readErr := rows.Err()
	rowsCloseErr := rows.Close()
	databaseCloseErr := database.Close()
	return evidence, errors.Join(readErr, rowsCloseErr, databaseCloseErr)
}

func parseExternalImports(path string) (map[string]struct{}, time.Time, int, bool, error) {
	var index struct {
		Records []struct {
			SourcePath       string `json:"source_path"`
			ImportedThreadID string `json:"imported_thread_id"`
			ImportedAt       int64  `json:"imported_at"`
		} `json:"records"`
	}
	tooLong, err := provider.DecodeJSONFile(path, maxRecordBytes, &index)
	if tooLong || err != nil {
		return nil, time.Time{}, 0, tooLong, err
	}
	ids := make(map[string]struct{})
	var earliest time.Time
	referencedSources := 0
	for _, record := range index.Records {
		if record.ImportedThreadID != "" {
			ids[record.ImportedThreadID] = struct{}{}
		}
		if record.ImportedAt > 0 {
			at := time.Unix(record.ImportedAt, 0).UTC()
			if earliest.IsZero() || at.Before(earliest) {
				earliest = at
			}
		}
		if record.SourcePath != "" {
			referencedSources++
		}
	}
	return ids, earliest, referencedSources, false, nil
}

type record struct {
	Ordinal   *int64  `json:"ordinal"`
	Timestamp string  `json:"timestamp"`
	Type      string  `json:"type"`
	Payload   payload `json:"payload"`
}

type payload struct {
	Type           string          `json:"type"`
	ID             string          `json:"id"`
	SessionID      string          `json:"session_id"`
	ParentThreadID string          `json:"parent_thread_id"`
	HistoryMode    string          `json:"history_mode"`
	HistoryBase    *historyBase    `json:"history_base"`
	Source         json.RawMessage `json:"source"`
	CWD            string          `json:"cwd"`
	Model          string          `json:"model"`
	TurnID         string          `json:"turn_id"`
	CallID         string          `json:"call_id"`
	Message        string          `json:"message"`
	Images         json.RawMessage `json:"images"`
	LocalImages    json.RawMessage `json:"local_images"`
	Audio          json.RawMessage `json:"audio"`
	LocalAudio     json.RawMessage `json:"local_audio"`
	Item           *turnItem       `json:"item"`
	Info           *tokenInfo      `json:"info"`
}

type historyBase struct {
	ThreadID            string `json:"thread_id"`
	EndByteOffset       int64  `json:"end_byte_offset"`
	EndOrdinalExclusive int64  `json:"end_ordinal_exclusive"`
}

type turnItem struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	Content json.RawMessage `json:"content"`
}

type tokenInfo struct {
	Total tokenUsage `json:"total_token_usage"`
}

type tokenUsage struct {
	Input      int64 `json:"input_tokens"`
	Cached     int64 `json:"cached_input_tokens"`
	CacheWrite int64 `json:"cache_write_input_tokens"`
	Output     int64 `json:"output_tokens"`
	Reasoning  int64 `json:"reasoning_output_tokens"`
	Total      int64 `json:"total_tokens"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func parseFile(ctx context.Context, path string, maxOrdinalExclusive, maxBytesExclusive *int64) (model.Session, map[string]int, error) {
	session := model.Session{Harness: model.Codex, Models: make(map[string]model.ModelActivity), ActivityBasis: "session start"}
	warnings := make(map[string]int)
	seenTurns := make(map[string]struct{})
	seenCalls := make(map[string]struct{})
	seenPrompts := make(map[string]struct{})
	bestTokens := tokenUsage{}
	identitySet := false
	validSessionMeta := false
	historyMode := "legacy"
	appendPrompt := func(metric model.PromptMetric, ok bool, eventID string) {
		if !ok {
			return
		}
		if eventID != "" {
			if _, exists := seenPrompts[eventID]; exists {
				warnings["duplicate_prompt_event"]++
				return
			}
			seenPrompts[eventID] = struct{}{}
			metric.EventID = eventID
		}
		session.Prompts = append(session.Prompts, metric)
	}
	err := forEachLine(ctx, path, maxRecordBytes, maxBytesExclusive, func(line []byte, tooLong bool) {
		if tooLong {
			warnings["oversize_record"]++
			return
		}
		var event record
		if err := json.Unmarshal(line, &event); err != nil {
			warnings["malformed_record"]++
			return
		}
		if maxOrdinalExclusive != nil && event.Ordinal != nil && *event.Ordinal >= *maxOrdinalExclusive {
			return
		}
		at, timestampErr := time.Parse(time.RFC3339Nano, event.Timestamp)
		if timestampErr == nil {
			// The first session_meta identifies this physical rollout. Forked
			// histories can copy older records after it, so those records must not
			// move the physical session start backwards.
			if event.Type == "session_meta" && !identitySet && session.StartedAt.IsZero() {
				session.StartedAt = at
			}
			if session.EndedAt.IsZero() || at.After(session.EndedAt) {
				session.EndedAt = at
			}
		} else {
			warnings["invalid_timestamp"]++
		}

		switch event.Type {
		case "session_meta":
			// Forks and replays may contain copied metadata. The first metadata
			// record identifies the rollout file itself; later copies do not.
			if identitySet {
				return
			}
			identitySet = true
			if event.Payload.ID != "" {
				session.ID = event.Payload.ID
			} else if event.Payload.SessionID != "" {
				session.ID = event.Payload.SessionID
			}
			validSessionMeta = timestampErr == nil && session.ID != ""
			session.ParentID = event.Payload.ParentThreadID
			session.IsChild = event.Payload.ParentThreadID != "" ||
				(event.Payload.ID != "" && event.Payload.SessionID != "" && event.Payload.ID != event.Payload.SessionID)
			session.Project = provider.ProjectName(event.Payload.CWD)
			switch strings.ToLower(event.Payload.HistoryMode) {
			case "", "legacy":
				historyMode = "legacy"
			case "paginated":
				historyMode = "paginated"
			default:
				historyMode = "unknown"
				warnings["unknown_history_mode"]++
			}
		case "turn_context":
			if session.Project == "" {
				session.Project = provider.ProjectName(event.Payload.CWD)
			}
			modelName := provider.SafeLabel(event.Payload.Model, 100)
			if modelName == "" {
				return
			}
			if event.Payload.TurnID != "" {
				if _, exists := seenTurns[event.Payload.TurnID]; exists {
					warnings["duplicate_turn_context"]++
					return
				}
				seenTurns[event.Payload.TurnID] = struct{}{}
			}
			activity := session.Models[modelName]
			activity.Turns++
			session.Models[modelName] = activity
		case "response_item":
			// User response items are raw model-history records and duplicate the
			// presentation events below. Retain response_item only as the canonical
			// source for tool-call identities.
			if isToolCall(event.Payload.Type) {
				id := event.Payload.CallID
				if id == "" {
					id = event.Payload.ID
				}
				if id == "" {
					session.ToolCalls++
				} else if _, exists := seenCalls[id]; !exists {
					seenCalls[id] = struct{}{}
					session.ToolCalls++
				} else {
					warnings["duplicate_tool_call"]++
				}
			}
		case "event_msg":
			switch event.Payload.Type {
			case "token_count":
				if event.Payload.Info == nil {
					return
				}
				candidate := event.Payload.Info.Total
				if candidate.Total >= bestTokens.Total {
					bestTokens = candidate
				}
			case "user_message":
				if historyMode == "legacy" {
					eventID := event.Payload.ID
					if eventID == "" {
						eventID = event.Payload.TurnID
					}
					metric, ok := legacyPromptMetric(event.Payload, at)
					appendPrompt(metric, ok, eventID)
				}
			case "item_completed":
				if historyMode == "paginated" && event.Payload.Item != nil &&
					isUserMessageItem(event.Payload.Item.Type) {
					metric, ok := contentPromptMetric(event.Payload.Item.Content, at)
					appendPrompt(metric, ok, event.Payload.Item.ID)
				}
			}
		}
	})
	if !validSessionMeta {
		warnings["missing_or_invalid_session_meta"]++
		session.StartedAt = time.Time{}
	}
	if session.ID == "" {
		session.ID = rolloutStem(path)
	}
	if bestTokens.Total > 0 || bestTokens.Input > 0 || bestTokens.Output > 0 {
		session.Usage = model.TokenUsage{
			Available:  true,
			Exact:      true,
			Input:      bestTokens.Input,
			Output:     bestTokens.Output,
			CacheRead:  bestTokens.Cached,
			CacheWrite: bestTokens.CacheWrite,
			Reasoning:  bestTokens.Reasoning,
			Source:     "event_msg.payload.info.total_token_usage",
		}
	}
	session.ActivityAt = session.StartedAt
	return session, warnings, err
}

func isToolCall(kind string) bool {
	switch kind {
	case "function_call", "custom_tool_call", "local_shell_call", "mcp_tool_call", "web_search_call", "tool_search_call":
		return true
	default:
		return false
	}
}

func legacyPromptMetric(event payload, at time.Time) (model.PromptMetric, bool) {
	hasAttachment := hasJSONContent(event.Images) || hasJSONContent(event.LocalImages) ||
		hasJSONContent(event.Audio) || hasJSONContent(event.LocalAudio)
	text := strings.TrimSpace(event.Message)
	if text == "" || isInjectedContext(text) {
		return model.PromptMetric{At: at, HasText: false}, hasAttachment
	}
	words, characters := provider.TextMetric(text)
	return model.PromptMetric{At: at, Words: words, Characters: characters, HasText: true}, true
}

func contentPromptMetric(raw json.RawMessage, at time.Time) (model.PromptMetric, bool) {
	var blocks []contentBlock
	if json.Unmarshal(raw, &blocks) != nil {
		var text string
		if json.Unmarshal(raw, &text) != nil || strings.TrimSpace(text) == "" || isInjectedContext(text) {
			return model.PromptMetric{}, false
		}
		words, characters := provider.TextMetric(text)
		return model.PromptMetric{At: at, Words: words, Characters: characters, HasText: true}, true
	}
	parts := make([]string, 0, len(blocks))
	hasAttachment := false
	for _, block := range blocks {
		switch block.Type {
		case "image", "input_image", "local_image", "audio", "input_audio", "local_audio":
			hasAttachment = true
			continue
		case "text", "input_text":
		default:
			continue
		}
		if isInjectedContext(block.Text) {
			continue
		}
		if strings.TrimSpace(block.Text) != "" {
			parts = append(parts, block.Text)
		}
	}
	if len(parts) == 0 {
		return model.PromptMetric{At: at, HasText: false}, hasAttachment
	}
	text := strings.Join(parts, "\n")
	words, characters := provider.TextMetric(text)
	return model.PromptMetric{At: at, Words: words, Characters: characters, HasText: true}, true
}

func isUserMessageItem(kind string) bool {
	return kind == "UserMessage" || kind == "user_message"
}

func hasJSONContent(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed != "" && trimmed != "null" && trimmed != "[]" && trimmed != "{}" && trimmed != `""`
}

func rolloutStem(path string) string {
	name := filepath.Base(path)
	name = strings.TrimSuffix(name, ".zst")
	return strings.TrimSuffix(name, ".jsonl")
}

func forEachLine(ctx context.Context, path string, maxBytes int, maxSourceBytes *int64, fn func(line []byte, tooLong bool)) error {
	f, err := os.Open(path) // Intentionally read-only.
	if err != nil {
		return err
	}
	defer f.Close()

	var reader io.Reader = f
	if strings.HasSuffix(path, ".jsonl.zst") {
		if maxSourceBytes != nil {
			return errors.New("compressed rollout cannot be truncated at a byte boundary")
		}
		decoder, err := zstd.NewReader(
			f,
			zstd.WithDecoderConcurrency(1),
			zstd.WithDecoderMaxMemory(maxDecoderMemory),
		)
		if err != nil {
			return err
		}
		defer decoder.Close()
		reader = decoder
	} else if maxSourceBytes != nil {
		reader = io.LimitReader(f, *maxSourceBytes)
	}
	return forEachReaderLine(ctx, reader, maxBytes, fn)
}

func forEachReaderLine(ctx context.Context, source io.Reader, maxBytes int, fn func(line []byte, tooLong bool)) error {
	reader := bufio.NewReaderSize(source, 256*1024)
	line := make([]byte, 0, min(maxBytes, 256*1024))
	tooLong := false
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		fragment, prefix, readErr := reader.ReadLine()
		if !tooLong {
			if len(line)+len(fragment) > maxBytes {
				line = line[:0]
				tooLong = true
			} else {
				line = append(line, fragment...)
			}
		}
		if !prefix {
			if len(line) > 0 || tooLong {
				fn(line, tooLong)
			}
			line = line[:0]
			tooLong = false
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
	}
}

func isInjectedContext(text string) bool {
	trimmed := strings.TrimSpace(text)
	for _, prefix := range []string{
		"<environment_context>",
		"<codex_internal_context ",
		"<permissions instructions>",
		"<collaboration_mode>",
	} {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}
