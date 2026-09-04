// Package codex reads Codex rollout JSONL histories.
package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/0merUfuk/skuggsja/internal/model"
	"github.com/0merUfuk/skuggsja/internal/provider"
	"github.com/klauspost/compress/zstd"
)

const maxRecordBytes = 64 << 20
const maxDecoderMemory = 256 << 20

// Reader discovers active and archived rollout files.
type Reader struct {
	SessionsDir string
	ArchivedDir string
}

func (Reader) Harness() model.Harness { return model.Codex }
func (Reader) DisplayName() string    { return "Codex" }

func (r Reader) Discover(_ context.Context) (provider.Discovery, error) {
	d := provider.Discovery{Harness: model.Codex, Roots: []string{r.SessionsDir, r.ArchivedDir}}
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
	sort.Strings(d.Files)
	return d, nil
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
		SourceFiles:       append([]string(nil), d.Files...),
		Limitations: []string{
			"Model events are unique turn-context IDs; exact session token totals cannot be attributed to individual models.",
		},
	}
	if len(d.Files) == 0 {
		return result
	}
	result.Status = "supported"
	result.ToolCallsAvailable = true

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

	byID := make(map[string]model.Session)
	for item := range parsedFiles {
		if item.err != nil {
			result.AddWarning("unreadable_file", "Some rollout files could not be read and were skipped.")
			continue
		}
		for code, count := range item.warnings {
			message := map[string]string{
				"malformed_record": "Malformed records were skipped without modifying their source files.",
				"oversize_record":  "Oversize records were skipped to keep memory use bounded.",
				"unknown_history_mode": "A rollout used an unknown history mode; its prompts were skipped " +
					"rather than risking duplicate counts.",
			}[code]
			for range count {
				result.AddWarning(code, message)
			}
		}
		if item.session.StartedAt.IsZero() {
			continue
		}
		key := item.session.ID
		if key == "" {
			key = item.session.StartedAt.String()
		}
		previous, exists := byID[key]
		if !exists || prefer(item.session, previous) {
			byID[key] = item.session
		}
	}
	for _, session := range byID {
		result.Sessions = append(result.Sessions, session)
	}
	if len(result.Warnings) > 0 {
		result.Status = "supported with warnings"
	}
	sort.Slice(result.Sessions, func(i, j int) bool {
		return result.Sessions[i].StartedAt.Before(result.Sessions[j].StartedAt)
	})
	return result
}

func prefer(candidate, current model.Session) bool {
	if candidate.EndedAt.After(current.EndedAt) {
		return true
	}
	return len(candidate.Prompts) > len(current.Prompts)
}

type record struct {
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

func parseFile(ctx context.Context, path string) (model.Session, map[string]int, error) {
	session := model.Session{Harness: model.Codex, Models: make(map[string]model.ModelActivity), ActivityBasis: "session start"}
	warnings := make(map[string]int)
	seenTurns := make(map[string]struct{})
	seenCalls := make(map[string]struct{})
	seenPrompts := make(map[string]struct{})
	bestTokens := tokenUsage{}
	identitySet := false
	historyMode := "legacy"
	appendPrompt := func(metric model.PromptMetric, ok bool, eventID string) {
		if !ok {
			return
		}
		if eventID != "" {
			if _, exists := seenPrompts[eventID]; exists {
				return
			}
			seenPrompts[eventID] = struct{}{}
			metric.EventID = eventID
		}
		session.Prompts = append(session.Prompts, metric)
	}
	err := forEachLine(ctx, path, maxRecordBytes, func(line []byte, tooLong bool) {
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
			// The first session_meta identifies this physical rollout. Forked
			// histories can copy older records after it, so those records must not
			// move the physical session start backwards.
			if event.Type == "session_meta" && !identitySet && session.StartedAt.IsZero() {
				session.StartedAt = at
			}
			if session.EndedAt.IsZero() || at.After(session.EndedAt) {
				session.EndedAt = at
			}
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
	case "function_call", "custom_tool_call", "local_shell_call", "mcp_tool_call":
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

func forEachLine(ctx context.Context, path string, maxBytes int, fn func(line []byte, tooLong bool)) error {
	f, err := os.Open(path) // Intentionally read-only.
	if err != nil {
		return err
	}
	defer f.Close()

	var reader io.Reader = f
	if strings.HasSuffix(path, ".jsonl.zst") {
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
