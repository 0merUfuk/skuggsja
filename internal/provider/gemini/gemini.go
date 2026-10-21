// Package gemini reads Gemini CLI's per-session chat recordings.
//
// Gemini CLI appends each session as a JSON-lines event log under
// <gemini home>/tmp/<project id>/chats/session-*.jsonl: a metadata header,
// message records, and $patch/$rewindTo/$set edit records. Older releases wrote
// one whole ConversationRecord per .json file; both forms are read.
package gemini

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/0merUfuk/skuggsja/internal/model"
	"github.com/0merUfuk/skuggsja/internal/platform"
	"github.com/0merUfuk/skuggsja/internal/provider"
)

const (
	maxLineBytes = 16 << 20
	maxFileBytes = 128 << 20
)

var hashID = regexp.MustCompile(`^[0-9a-fA-F]{32,}$`)

// Reader discovers recordings under every project folder of Gemini CLI's
// global temp directory.
type Reader struct{ TmpRoot string }

func (Reader) Harness() model.Harness { return model.Gemini }
func (Reader) DisplayName() string    { return "Gemini CLI" }

// New builds the Gemini CLI reader from the machine's discovered paths. It is
// the harness's sole entry in the CLI's provider registry.
func New(paths platform.Paths) provider.Reader {
	return Reader{TmpRoot: paths.GeminiTmpRoot}
}

func (r Reader) Discover(_ context.Context) (provider.Discovery, error) {
	d := provider.Discovery{Harness: model.Gemini}
	if r.TmpRoot == "" {
		return d, nil
	}
	projects, err := os.ReadDir(r.TmpRoot)
	if os.IsNotExist(err) {
		return d, nil
	}
	if err != nil {
		return d, err
	}
	for _, project := range sortedDirs(projects) {
		chats := filepath.Join(r.TmpRoot, project, "chats")
		if info, statErr := os.Stat(chats); statErr == nil && info.IsDir() {
			d.Roots = append(d.Roots, chats)
		}
		d.Files = append(d.Files, recordings(chats)...)
		if entries, readErr := os.ReadDir(chats); readErr == nil {
			for _, sub := range sortedDirs(entries) {
				d.Files = append(d.Files, recordings(filepath.Join(chats, sub))...)
			}
		}
	}
	return d, nil
}

func sortedDirs(entries []os.DirEntry) []string {
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names
}

// recordings lists session files in one folder, preferring .jsonl over a
// legacy .json with the same stem (Gemini CLI normalizes the latter to the
// former when it resumes a session).
func recordings(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	have := make(map[string]bool)
	for _, entry := range entries {
		if name := entry.Name(); strings.HasPrefix(name, "session-") && strings.HasSuffix(name, ".jsonl") {
			have[strings.TrimSuffix(name, ".jsonl")] = true
		}
	}
	var files []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "session-") {
			continue
		}
		stem := strings.TrimSuffix(strings.TrimSuffix(name, ".jsonl"), ".json")
		isJSONL := strings.HasSuffix(name, ".jsonl")
		if !isJSONL && (!strings.HasSuffix(name, ".json") || have[stem]) {
			continue
		}
		path := filepath.Join(dir, name)
		if info, statErr := os.Lstat(path); statErr == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			files = append(files, path)
		}
	}
	return files
}

func (r Reader) Read(ctx context.Context, d provider.Discovery) (result model.ProviderResult) {
	result = model.ProviderResult{
		Harness:            model.Gemini,
		DisplayName:        "Gemini CLI",
		Status:             "not found",
		VerificationLevel:  "schema-verified with synthetic fixtures",
		ToolCallsAvailable: true,
		SourceFiles:        append([]string(nil), d.Files...),
		Limitations: []string{
			"Only Gemini CLI's own chat recordings are read. Antigravity is a separate harness and keeps its sessions elsewhere.",
			"Project names come from Gemini CLI's project folder slug; sessions in a legacy hash-named folder have no project name.",
			"Tool-call counts and token usage come from the model replies recorded in the session; replies that were never recorded are not counted.",
			"The layout was confirmed from Gemini CLI's published source, not a local installation; this machine has no Gemini CLI history, so non-synthetic data is not yet verified.",
		},
	}
	if len(d.Files) == 0 {
		return result
	}
	for _, path := range d.Files {
		if err := ctx.Err(); err != nil {
			break
		}
		rec, ok := load(ctx, path, &result)
		if !ok {
			continue
		}
		if session, keep := toSession(rec, path); keep {
			result.Sessions = append(result.Sessions, session)
		}
	}
	sort.Slice(result.Sessions, func(i, j int) bool {
		return result.Sessions[i].StartedAt.Before(result.Sessions[j].StartedAt)
	})
	if len(result.Sessions) == 0 && len(result.Warnings) > 0 {
		result.Status = "unavailable"
		return result
	}
	result.Status = "supported"
	if len(result.Warnings) > 0 {
		result.Status = "supported with warnings"
	}
	return result
}

type message struct {
	ID        string          `json:"id"`
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Content   json.RawMessage `json:"content"`
	Model     string          `json:"model"`
	Tokens    *struct {
		Input   int64 `json:"input"`
		Output  int64 `json:"output"`
		Cached  int64 `json:"cached"`
		Thought int64 `json:"thoughts"`
		Tool    int64 `json:"tool"`
	} `json:"tokens"`
	ToolCalls []json.RawMessage `json:"toolCalls"`
}

type recording struct {
	SessionID   string
	StartTime   string
	LastUpdated string
	Kind        string
	order       []string
	messages    map[string]*message
}

func newRecording() *recording { return &recording{messages: make(map[string]*message)} }

func (r *recording) put(m *message) {
	if _, exists := r.messages[m.ID]; !exists {
		r.order = append(r.order, m.ID)
	}
	r.messages[m.ID] = m
}

func (r *recording) remove(id string) {
	if _, exists := r.messages[id]; !exists {
		return
	}
	delete(r.messages, id)
	for i, existing := range r.order {
		if existing == id {
			r.order = append(r.order[:i], r.order[i+1:]...)
			return
		}
	}
}

// rewind drops the message with this id and everything after it, or every
// message when the id is unknown, exactly as Gemini CLI's own loader does.
func (r *recording) rewind(id string) {
	for i, existing := range r.order {
		if existing == id {
			for _, dropped := range r.order[i:] {
				delete(r.messages, dropped)
			}
			r.order = r.order[:i]
			return
		}
	}
	r.order, r.messages = nil, make(map[string]*message)
}

func (r *recording) reorder(ids []string) {
	listed := make(map[string]bool, len(ids))
	var tail []string
	for _, id := range ids {
		if _, exists := r.messages[id]; exists && !listed[id] {
			listed[id] = true
			tail = append(tail, id)
		}
	}
	var head []string
	for _, id := range r.order {
		if !listed[id] {
			head = append(head, id)
		}
	}
	r.order = append(head, tail...)
}

func load(ctx context.Context, path string, result *model.ProviderResult) (*recording, bool) {
	rec := newRecording()
	if strings.HasSuffix(path, ".json") {
		var legacy struct {
			SessionID   string     `json:"sessionId"`
			StartTime   string     `json:"startTime"`
			LastUpdated string     `json:"lastUpdated"`
			Kind        string     `json:"kind"`
			Messages    []*message `json:"messages"`
		}
		tooLarge, err := provider.DecodeJSONFile(path, maxFileBytes, &legacy)
		if tooLarge {
			result.AddWarning("oversize_session", "An oversize Gemini CLI session file was skipped.")
			return nil, false
		}
		if err != nil {
			result.AddWarning("malformed_session", "A malformed Gemini CLI session file was skipped.")
			return nil, false
		}
		rec.SessionID, rec.StartTime, rec.LastUpdated, rec.Kind = legacy.SessionID, legacy.StartTime, legacy.LastUpdated, legacy.Kind
		for _, m := range legacy.Messages {
			if m != nil && m.ID != "" {
				rec.put(m)
			}
		}
		return rec, true
	}
	err := provider.ForEachLine(ctx, path, maxLineBytes, func(line []byte, tooLong bool) {
		if tooLong {
			result.AddWarning("oversize_record", "An oversize Gemini CLI record was skipped.")
			return
		}
		if len(strings.TrimSpace(string(line))) == 0 {
			return
		}
		var probe map[string]json.RawMessage
		if json.Unmarshal(line, &probe) != nil {
			result.AddWarning("malformed_record", "A malformed Gemini CLI record was skipped.")
			return
		}
		apply(rec, line, probe)
	})
	if err != nil {
		result.AddWarning("session_unreadable", "A Gemini CLI session file could not be read and was skipped.")
		return nil, false
	}
	return rec, true
}

func apply(rec *recording, line []byte, probe map[string]json.RawMessage) {
	if raw, ok := probe["$rewindTo"]; ok {
		var id string
		if json.Unmarshal(raw, &id) == nil {
			rec.rewind(id)
		}
		return
	}
	if raw, ok := probe["$patch"]; ok {
		var patch struct {
			ID        string          `json:"id"`
			Content   json.RawMessage `json:"content"`
			Updates   []patchItem     `json:"updates"`
			RemoveIDs []string        `json:"removeIds"`
			OrderIDs  []string        `json:"orderIds"`
		}
		if json.Unmarshal(raw, &patch) != nil {
			return
		}
		rec.patch(patchItem{ID: patch.ID, Content: patch.Content})
		for _, update := range patch.Updates {
			rec.patch(update)
		}
		for _, id := range patch.RemoveIDs {
			rec.remove(id)
		}
		if len(patch.OrderIDs) > 0 {
			rec.reorder(patch.OrderIDs)
		}
		return
	}
	if raw, ok := probe["$set"]; ok {
		var set struct {
			StartTime   string `json:"startTime"`
			LastUpdated string `json:"lastUpdated"`
			Kind        string `json:"kind"`
		}
		if json.Unmarshal(raw, &set) == nil {
			rec.mergeMetadata(set.StartTime, set.LastUpdated, set.Kind)
		}
		return
	}
	if _, isMessage := probe["id"]; isMessage {
		var m message
		if json.Unmarshal(line, &m) == nil && m.ID != "" {
			rec.put(&m)
		}
		return
	}
	if _, isHeader := probe["sessionId"]; isHeader {
		var header struct {
			SessionID   string `json:"sessionId"`
			StartTime   string `json:"startTime"`
			LastUpdated string `json:"lastUpdated"`
			Kind        string `json:"kind"`
		}
		if json.Unmarshal(line, &header) == nil {
			if rec.SessionID == "" {
				rec.SessionID = header.SessionID
			}
			rec.mergeMetadata(header.StartTime, header.LastUpdated, header.Kind)
		}
	}
}

type patchItem struct {
	ID      string          `json:"id"`
	Content json.RawMessage `json:"content"`
}

func (r *recording) patch(item patchItem) {
	if item.ID == "" || len(item.Content) == 0 {
		return
	}
	if existing, ok := r.messages[item.ID]; ok {
		existing.Content = item.Content
	}
}

func (r *recording) mergeMetadata(start, updated, kind string) {
	if start != "" {
		r.StartTime = start
	}
	if updated != "" {
		r.LastUpdated = updated
	}
	if kind != "" {
		r.Kind = kind
	}
}

func toSession(rec *recording, path string) (model.Session, bool) {
	id := rec.SessionID
	if id == "" {
		id = strings.TrimSuffix(strings.TrimSuffix(filepath.Base(path), ".jsonl"), ".json")
	}
	session := model.Session{
		Harness:       model.Gemini,
		ID:            id,
		ActivityBasis: "session message timestamps",
		Models:        make(map[string]model.ModelActivity),
		Project:       projectFromPath(path),
		IsChild:       rec.Kind == "subagent",
	}
	if session.IsChild {
		if parent := filepath.Base(filepath.Dir(path)); parent != "chats" {
			session.ParentID = parent
		}
	}
	session.StartedAt = parseTime(rec.StartTime)
	session.EndedAt = parseTime(rec.LastUpdated)
	var usage model.TokenUsage
	for _, messageID := range rec.order {
		m := rec.messages[messageID]
		at := parseTime(m.Timestamp)
		if !at.IsZero() {
			if session.StartedAt.IsZero() || at.Before(session.StartedAt) {
				session.StartedAt = at
			}
			if at.After(session.EndedAt) {
				session.EndedAt = at
			}
		}
		switch m.Type {
		case "user":
			text := contentText(m.Content)
			words, characters := provider.TextMetric(text)
			session.Prompts = append(session.Prompts, model.PromptMetric{
				EventID: id + "#" + messageID, At: at, Words: words, Characters: characters,
				HasText: strings.TrimSpace(text) != "",
			})
		case "gemini":
			session.ToolCalls += int64(len(m.ToolCalls))
			var call model.TokenUsage
			if m.Tokens != nil {
				call = model.TokenUsage{
					Available: true, Exact: true,
					Input: m.Tokens.Input, Output: m.Tokens.Output, CacheRead: m.Tokens.Cached,
					Reasoning: m.Tokens.Thought, Source: "Gemini CLI message token summaries",
				}
				usage.Add(call)
			}
			if label := provider.SafeLabel(m.Model, 80); label != "" {
				activity := session.Models[label]
				activity.Turns++
				activity.Usage.Add(call)
				session.Models[label] = activity
			}
		}
	}
	if len(session.Prompts) == 0 {
		return session, false
	}
	if usage.Available {
		session.Usage = usage
	}
	session.ActivityAt = session.EndedAt
	if session.StartedAt.IsZero() {
		session.TimeUnavailable = true
	}
	return session, true
}

func projectFromPath(path string) string {
	dir := filepath.Dir(path)
	for filepath.Base(dir) != "chats" {
		next := filepath.Dir(dir)
		if next == dir {
			return ""
		}
		dir = next
	}
	name := filepath.Base(filepath.Dir(dir))
	if hashID.MatchString(name) {
		return ""
	}
	return provider.SafeLabel(name, 80)
}

func parseTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

// contentText flattens Gemini's PartListUnion (a string, a part, or an array of
// parts) to its text parts; function calls and inline data contribute nothing.
func contentText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var part struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &part) == nil && part.Text != "" {
		return part.Text
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var b strings.Builder
	for _, p := range parts {
		if p.Text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(p.Text)
	}
	return b.String()
}
