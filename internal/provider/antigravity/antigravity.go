// Package antigravity reads Google Antigravity's conversation index and
// optional per-conversation JSONL transcripts.
//
// Antigravity keeps one protobuf-backed SQLite file per conversation. Those
// step payloads have no public schema and are never decoded here. What is
// read instead is the app's own conversation_summaries table, the full-text
// index it maintains over user steps, and—when present—the plain JSONL step
// transcript at brain/<conversation_id>/.system_generated/logs/transcript.jsonl,
// which exposes per-step timestamps, token usage, and tool-call counts without
// touching protobuf payloads or walking bundled node_modules.
package antigravity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/0merUfuk/skuggsja/internal/model"
	"github.com/0merUfuk/skuggsja/internal/platform"
	"github.com/0merUfuk/skuggsja/internal/provider"
	"github.com/0merUfuk/skuggsja/internal/sqlitecopy"
)

const (
	summariesFile      = "conversation_summaries.db"
	maxChunkBytes      = 8 << 20
	maxTranscriptBytes = 8 << 20
)

// Reader discovers Antigravity's conversation-summaries database. SQLite never
// opens this path; sqlitecopy takes a private, read-only copy first.
type Reader struct{ Root string }

func (Reader) Harness() model.Harness { return model.Antigravity }
func (Reader) DisplayName() string    { return "Antigravity" }

// New builds the Antigravity reader from the machine's discovered paths. It is
// the harness's sole entry in the CLI's provider registry.
func New(paths platform.Paths) provider.Reader {
	return Reader{Root: paths.AntigravityRoot}
}

func (r Reader) Discover(_ context.Context) (provider.Discovery, error) {
	d := provider.Discovery{Harness: model.Antigravity}
	if r.Root == "" {
		return d, nil
	}
	database := filepath.Join(r.Root, summariesFile)
	// The root also holds the application's bundled node_modules and logs; it is
	// protected from output but deliberately not walked by the source audit.
	d.ConfiguredFiles = []string{database, database + "-wal", database + "-shm", database + "-journal"}
	d.ProtectedDirectories = []string{r.Root}
	info, err := os.Lstat(database)
	if errors.Is(err, os.ErrNotExist) {
		return d, nil
	}
	if err != nil {
		return d, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return d, errors.New("Antigravity conversation summaries is a symbolic link")
	}
	if !info.Mode().IsRegular() {
		return d, errors.New("Antigravity conversation summaries is not a regular file")
	}
	d.Files = append(d.Files, database)
	for _, transcript := range discoverTranscripts(r.Root) {
		d.Files = append(d.Files, transcript)
	}
	return d, nil
}

func discoverTranscripts(root string) []string {
	brainDir := filepath.Join(root, "brain")
	info, err := os.Lstat(brainDir)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsDir() {
		return nil
	}
	entries, err := os.ReadDir(brainDir)
	if err != nil {
		return nil
	}
	var ids []string
	for _, entry := range entries {
		if entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			ids = append(ids, entry.Name())
		}
	}
	sort.Strings(ids)
	var files []string
	for _, id := range ids {
		sysDir := filepath.Join(brainDir, id, ".system_generated")
		if sInfo, sErr := os.Lstat(sysDir); sErr != nil || sInfo.Mode()&os.ModeSymlink != 0 || !sInfo.Mode().IsDir() {
			continue
		}
		logsDir := filepath.Join(sysDir, "logs")
		if lInfo, lErr := os.Lstat(logsDir); lErr != nil || lInfo.Mode()&os.ModeSymlink != 0 || !lInfo.Mode().IsDir() {
			continue
		}
		transcript := filepath.Join(logsDir, "transcript.jsonl")
		if tInfo, tErr := os.Lstat(transcript); tErr == nil && tInfo.Mode()&os.ModeSymlink == 0 && tInfo.Mode().IsRegular() {
			files = append(files, transcript)
		}
	}
	return files
}

func (r Reader) Read(ctx context.Context, d provider.Discovery) (result model.ProviderResult) {
	result = model.ProviderResult{
		Harness:           model.Antigravity,
		DisplayName:       "Antigravity",
		Status:            "not found",
		VerificationLevel: "real data on macOS (one local conversation)",
		SourceFiles:       append([]string(nil), d.Files...),
		Limitations: []string{
			"Per-step protobuf payloads are not decoded; model names are not exposed. When brain/<id>/.system_generated/logs/transcript.jsonl is present, token usage, tool-call counts, per-prompt timestamps, and session span are read from it.",
			"Prompts are the user steps recorded in Antigravity's own search index; their sizes come from the indexed text and can include small separators. A conversation missing from that index contributes a session but no prompts.",
			"When a conversation has no local JSONL transcript, Antigravity exposes only its last-modified time and last-user-input timestamp, so its duration, token usage, and tool-call count are unavailable.",
			"Only the Antigravity application store (~/.gemini/antigravity) is read. Plain Gemini CLI sessions are a separate harness, and the Antigravity desktop editor's Electron profile is not read.",
			"The layout was confirmed against one local conversation; other Antigravity versions may use a different layout and are reported as an unsupported schema rather than guessed.",
		},
	}
	if len(d.Files) == 0 {
		return result
	}
	database, err := sqlitecopy.Open(ctx, d.Files[0])
	if err != nil {
		result.Status = "unavailable"
		result.AddWarning("sqlite_snapshot_failed", "The live Antigravity conversation index could not be copied consistently and was skipped.")
		return result
	}
	defer func() {
		if err := database.Close(); err != nil {
			result.AddWarning("sqlite_snapshot_cleanup_failed", "The private Antigravity conversation-index copy could not be completely removed.")
			if result.Status == "supported" {
				result.Status = "supported with warnings"
			}
		}
	}()

	sessions, lastInputs, err := readSessions(ctx, database.DB)
	if err != nil {
		result.Status = "unsupported schema"
		result.AddWarning("unknown_schema", "The Antigravity conversation-index schema is not recognized by this version.")
		return result
	}
	byID := make(map[string]*model.Session, len(sessions))
	for i := range sessions {
		byID[sessions[i].ID] = &sessions[i]
	}
	if err := readPrompts(ctx, database.DB, byID, lastInputs, &result); err != nil {
		result.AddWarning("prompt_metrics_unavailable", "Antigravity prompt records could not be read; session totals remain available.")
	}
	for _, path := range d.Files[1:] {
		if ctx.Err() != nil {
			break
		}
		enrichFromTranscript(ctx, path, byID, &result)
	}

	for _, session := range sessions {
		if len(session.Prompts) == 0 {
			continue
		}
		result.Sessions = append(result.Sessions, session)
	}
	sort.Slice(result.Sessions, func(i, j int) bool {
		return result.Sessions[i].EndedAt.Before(result.Sessions[j].EndedAt)
	})
	result.Status = "supported"
	if len(result.Warnings) > 0 {
		result.Status = "supported with warnings"
	}
	return result
}

// lastInput is a conversation's last-user-input step and time. The prompt pass
// stamps that one prompt with its real time; every other prompt keeps an
// unknown time because Antigravity does not expose one.
type lastInput struct {
	step int
	at   time.Time
}

func readSessions(ctx context.Context, db *sql.DB) ([]model.Session, map[string]lastInput, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT conversation_id, COALESCE(last_modified_time, ''), COALESCE(workspace_uris, ''),
		       COALESCE(parent_conversation_id, ''), COALESCE(nesting_depth, 0),
		       COALESCE(last_user_input_time, ''), COALESCE(last_user_input_step_index, -1)
		FROM conversation_summaries
	`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var sessions []model.Session
	inputs := make(map[string]lastInput)
	for rows.Next() {
		var id, modified, workspaces, parent, inputTime string
		var depth, inputStep int
		if err := rows.Scan(&id, &modified, &workspaces, &parent, &depth, &inputTime, &inputStep); err != nil {
			return nil, nil, err
		}
		if id == "" {
			continue
		}
		at := parseTimestamp(modified)
		session := model.Session{
			Harness:       model.Antigravity,
			ID:            id,
			ParentID:      parent,
			IsChild:       parent != "" || depth > 0,
			StartedAt:     at,
			EndedAt:       at,
			ActivityAt:    at,
			ActivityBasis: "conversation last-modified time",
			Project:       projectFromWorkspaces(workspaces),
			Models:        make(map[string]model.ModelActivity),
		}
		if at.IsZero() {
			session.TimeUnavailable = true
		}
		if inputStep >= 0 {
			inputs[id] = lastInput{step: inputStep, at: parseTimestamp(inputTime)}
		}
		sessions = append(sessions, session)
	}
	return sessions, inputs, rows.Err()
}

func readPrompts(ctx context.Context, db *sql.DB, sessions map[string]*model.Session, inputs map[string]lastInput, result *model.ProviderResult) error {
	rows, err := db.QueryContext(ctx, `
		SELECT conversation_id, chunk_index, user_step_byte_offsets, user_body, LENGTH(user_body)
		FROM conversations_fts
		WHERE CAST(chunk_index AS INTEGER) >= 0
		ORDER BY conversation_id, CAST(chunk_index AS INTEGER)
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var chunk int
		var offsets []byte
		var body sql.NullString
		var bodyLength int64
		if err := rows.Scan(&id, &chunk, &offsets, &body, &bodyLength); err != nil {
			return err
		}
		session, ok := sessions[id]
		if !ok {
			continue
		}
		if bodyLength > maxChunkBytes {
			result.AddWarning("oversize_index_chunk", "An oversize Antigravity index chunk was skipped.")
			continue
		}
		marks, valid := parseOffsets(offsets)
		if !valid {
			result.AddWarning("malformed_prompt_index", "An Antigravity prompt index entry was malformed and was skipped.")
			continue
		}
		text := []byte(body.String)
		sliceable := offsetsFit(marks, len(text))
		if !sliceable && len(marks) > 0 {
			result.AddWarning("prompt_text_slices_invalid", "Antigravity prompt byte offsets did not fit the indexed text; those prompts are counted without size metrics.")
		}
		for i, mark := range marks {
			metric := model.PromptMetric{EventID: id + "#" + strconv.Itoa(mark.step)}
			if input, found := inputs[id]; found && input.step == mark.step {
				metric.At = input.at
			}
			if sliceable {
				end := len(text)
				if i+1 < len(marks) {
					end = marks[i+1].offset
				}
				segment := text[mark.offset:end]
				if utf8.Valid(segment) {
					metric.Words, metric.Characters = provider.TextMetric(string(segment))
					metric.HasText = strings.TrimSpace(string(segment)) != ""
				}
			}
			session.Prompts = append(session.Prompts, metric)
		}
	}
	return rows.Err()
}

type mark struct{ step, offset int }

// parseOffsets reads the index's user_step_byte_offsets JSON: an array of
// [stepIndex, byteOffset] pairs ordered by step.
func parseOffsets(raw []byte) ([]mark, bool) {
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return nil, true
	}
	var pairs [][2]int
	if err := json.Unmarshal(raw, &pairs); err != nil {
		return nil, false
	}
	marks := make([]mark, 0, len(pairs))
	for _, pair := range pairs {
		marks = append(marks, mark{step: pair[0], offset: pair[1]})
	}
	sort.SliceStable(marks, func(i, j int) bool { return marks[i].step < marks[j].step })
	return marks, true
}

func offsetsFit(marks []mark, length int) bool {
	previous := 0
	for _, m := range marks {
		if m.offset < previous || m.offset > length {
			return false
		}
		previous = m.offset
	}
	return true
}

func projectFromWorkspaces(raw string) string {
	var uris []string
	if err := json.Unmarshal([]byte(raw), &uris); err != nil {
		return ""
	}
	for _, uri := range uris {
		parsed, err := url.Parse(uri)
		if err != nil || parsed.Scheme != "file" {
			continue
		}
		if name := provider.ProjectName(parsed.Path); name != "" {
			return name
		}
	}
	return ""
}

func parseTimestamp(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05.999999999Z07:00", time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

// transcriptStep decodes only metadata fields from one JSONL step line; raw
// content, thinking, and tool call arguments are never decoded into memory.
type transcriptStep struct {
	StepIndex       *int              `json:"step_index"`
	Type            string            `json:"type"`
	CreatedAt       string            `json:"created_at"`
	InputTokens     int64             `json:"input_tokens"`
	OutputTokens    int64             `json:"output_tokens"`
	CacheReadTokens int64             `json:"cache_read_tokens"`
	ToolCalls       []json.RawMessage `json:"tool_calls"`
}

func conversationIDFromTranscriptPath(path string) string {
	logsDir := filepath.Dir(path)
	if filepath.Base(logsDir) != "logs" {
		return ""
	}
	sysDir := filepath.Dir(logsDir)
	if filepath.Base(sysDir) != ".system_generated" {
		return ""
	}
	convDir := filepath.Dir(sysDir)
	if filepath.Base(filepath.Dir(convDir)) != "brain" {
		return ""
	}
	return filepath.Base(convDir)
}

func enrichFromTranscript(ctx context.Context, path string, sessions map[string]*model.Session, result *model.ProviderResult) {
	id := conversationIDFromTranscriptPath(path)
	session, ok := sessions[id]
	if !ok {
		return
	}
	var (
		earliest, latest time.Time
		usage            model.TokenUsage
		toolCalls        int64
		stepTimes        = make(map[int]time.Time)
		hasSteps         bool
	)
	err := provider.ForEachLine(ctx, path, maxTranscriptBytes, func(line []byte, tooLong bool) {
		if tooLong {
			result.AddWarning("oversize_transcript_line", "An oversize Antigravity transcript record was skipped.")
			return
		}
		if len(strings.TrimSpace(string(line))) == 0 {
			return
		}
		var step transcriptStep
		if json.Unmarshal(line, &step) != nil {
			result.AddWarning("malformed_transcript_line", "A malformed Antigravity transcript record was skipped.")
			return
		}
		hasSteps = true
		at := parseTimestamp(step.CreatedAt)
		if !at.IsZero() {
			if earliest.IsZero() || at.Before(earliest) {
				earliest = at
			}
			if latest.IsZero() || at.After(latest) {
				latest = at
			}
			if step.StepIndex != nil {
				stepTimes[*step.StepIndex] = at
			}
		}
		if step.Type == "PLANNER_RESPONSE" {
			toolCalls += int64(len(step.ToolCalls))
			if step.InputTokens > 0 || step.OutputTokens > 0 || step.CacheReadTokens > 0 {
				usage.Add(model.TokenUsage{
					Available: true,
					Exact:     true,
					Input:     step.InputTokens,
					Output:    step.OutputTokens,
					CacheRead: step.CacheReadTokens,
					Source:    "Antigravity step transcript token counts",
				})
			}
		}
	})
	if err != nil {
		result.AddWarning("transcript_unreadable", "An Antigravity step transcript could not be read and was skipped.")
		return
	}
	if !hasSteps {
		return
	}
	result.ToolCallsAvailable = true
	session.ToolCalls += toolCalls
	if usage.Available {
		session.Usage.Add(usage)
	}
	if !earliest.IsZero() {
		if session.StartedAt.IsZero() || earliest.Before(session.StartedAt) {
			session.StartedAt = earliest
		}
		if session.EndedAt.IsZero() || latest.After(session.EndedAt) {
			session.EndedAt = latest
		}
		session.ActivityAt = session.StartedAt
		session.ActivityBasis = "transcript step timestamps"
		session.TimeUnavailable = false
	}
	for i := range session.Prompts {
		prefix := id + "#"
		if !strings.HasPrefix(session.Prompts[i].EventID, prefix) {
			continue
		}
		stepNum, convErr := strconv.Atoi(strings.TrimPrefix(session.Prompts[i].EventID, prefix))
		if convErr != nil {
			continue
		}
		if at, found := stepTimes[stepNum]; found && !at.IsZero() {
			session.Prompts[i].At = at
		}
	}
}
