// Package continuedev reads Continue.dev's per-session chat files.
package continuedev

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/0merUfuk/skuggsja/internal/model"
	"github.com/0merUfuk/skuggsja/internal/platform"
	"github.com/0merUfuk/skuggsja/internal/provider"
)

const (
	indexName       = "sessions.json"
	maxSessionBytes = 64 << 20
	maxIndexBytes   = 16 << 20
)

// Reader discovers Continue's sessions folder: one <sessionId>.json per chat
// plus a chronological sessions.json index that records creation times.
type Reader struct{ SessionsRoot string }

func (Reader) Harness() model.Harness { return model.Continue }
func (Reader) DisplayName() string    { return "Continue" }

// New builds the Continue reader from the machine's discovered paths. It is
// the harness's sole entry in the CLI's provider registry.
func New(paths platform.Paths) provider.Reader {
	return Reader{SessionsRoot: paths.ContinueSessionsRoot}
}

func (r Reader) Discover(_ context.Context) (provider.Discovery, error) {
	d := provider.Discovery{Harness: model.Continue}
	if r.SessionsRoot == "" {
		return d, nil
	}
	d.Roots = []string{r.SessionsRoot}
	entries, err := os.ReadDir(r.SessionsRoot)
	if os.IsNotExist(err) {
		return d, nil
	}
	if err != nil {
		return d, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == indexName || !strings.HasSuffix(name, ".json") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(r.SessionsRoot, name)
		info, statErr := os.Lstat(path)
		if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			continue
		}
		d.Files = append(d.Files, path)
	}
	index := filepath.Join(r.SessionsRoot, indexName)
	if info, statErr := os.Lstat(index); statErr == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
		d.AuditFiles = []string{index}
	}
	return d, nil
}

func (r Reader) Read(ctx context.Context, d provider.Discovery) (result model.ProviderResult) {
	result = model.ProviderResult{
		Harness:           model.Continue,
		DisplayName:       "Continue",
		Status:            "not found",
		VerificationLevel: "schema-verified with synthetic fixtures",
		SourceFiles:       append([]string(nil), d.Files...),
		Limitations: []string{
			"Continue records no per-message timestamps. A session is placed at the creation time in its sessions.json index entry and its duration is not measured; a session missing from that index has no usable time.",
			"The model shown is the chat model title the user configured for the session, not a provider model identifier.",
			"Token usage is the session's cumulative usage block when Continue wrote one; sessions without it report usage as unavailable.",
			"The layout was confirmed from Continue's published source, not from a local installation; this machine has no Continue history, so non-synthetic data is not yet verified.",
		},
	}
	if len(d.Files) == 0 {
		return result
	}
	created := readCreationTimes(d, &result)
	for _, path := range d.Files {
		if err := ctx.Err(); err != nil {
			break
		}
		var file sessionFile
		tooLarge, err := provider.DecodeJSONFile(path, maxSessionBytes, &file)
		if tooLarge {
			result.AddWarning("oversize_session", "An oversize Continue session file was skipped.")
			continue
		}
		if err != nil {
			result.AddWarning("malformed_session", "A malformed Continue session file was skipped.")
			continue
		}
		id := file.SessionID
		if id == "" {
			id = strings.TrimSuffix(filepath.Base(path), ".json")
		}
		session := model.Session{
			Harness: model.Continue,
			ID:      id,
			Project: provider.ProjectName(file.WorkspaceDirectory),
			Models:  make(map[string]model.ModelActivity),
		}
		var assistantTurns int64
		for index, item := range file.History {
			switch item.Message.Role {
			case "user":
				text := contentText(item.Message.Content)
				words, characters := provider.TextMetric(text)
				session.Prompts = append(session.Prompts, model.PromptMetric{
					EventID:    id + "#" + strconv.Itoa(index),
					Words:      words,
					Characters: characters,
					HasText:    strings.TrimSpace(text) != "",
				})
			case "assistant":
				assistantTurns++
			}
		}
		if len(session.Prompts) == 0 {
			continue
		}
		if at, ok := created[id]; ok {
			session.StartedAt, session.EndedAt, session.ActivityAt = at, at, at
			session.ActivityBasis = "session creation time"
		} else {
			session.TimeUnavailable = true
			result.AddWarning("missing_creation_time", "A Continue session had no creation time in the sessions index; it is counted without a timestamp.")
		}
		if usage := file.Usage; usage != nil {
			session.Usage = model.TokenUsage{
				Available: true, Exact: true,
				Input: usage.PromptTokens, Output: usage.CompletionTokens,
				Source: "Continue session usage block",
			}
			if usage.PromptTokensDetails != nil {
				session.Usage.CacheRead = usage.PromptTokensDetails.CachedTokens
				session.Usage.CacheWrite = usage.PromptTokensDetails.CacheWriteTokens
			}
			if usage.CompletionTokensDetails != nil {
				session.Usage.Reasoning = usage.CompletionTokensDetails.ReasoningTokens
			}
		}
		if title := provider.SafeLabel(file.ChatModelTitle, 80); title != "" {
			activity := model.ModelActivity{Turns: assistantTurns}
			if session.Usage.Available {
				activity.Usage = session.Usage
			}
			session.Models[title] = activity
		}
		result.Sessions = append(result.Sessions, session)
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

func readCreationTimes(d provider.Discovery, result *model.ProviderResult) map[string]time.Time {
	created := make(map[string]time.Time)
	if len(d.AuditFiles) == 0 {
		return created
	}
	var entries []indexEntry
	tooLarge, err := provider.DecodeJSONFile(d.AuditFiles[0], maxIndexBytes, &entries)
	if tooLarge || err != nil {
		result.AddWarning("sessions_index_unreadable", "Continue's sessions index could not be read; sessions are counted without timestamps.")
		return created
	}
	for _, entry := range entries {
		millis, parseErr := strconv.ParseInt(entry.DateCreated, 10, 64)
		if entry.SessionID == "" || parseErr != nil || millis <= 0 {
			continue
		}
		created[entry.SessionID] = time.UnixMilli(millis).UTC()
	}
	return created
}

// contentText flattens Continue's MessageContent (a string or an array of
// typed parts) to its text parts. Image parts contribute nothing.
func contentText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var b strings.Builder
	for _, part := range parts {
		if part.Type == "text" || part.Text != "" {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(part.Text)
		}
	}
	return b.String()
}

type indexEntry struct {
	SessionID   string `json:"sessionId"`
	DateCreated string `json:"dateCreated"`
}

type sessionFile struct {
	SessionID          string        `json:"sessionId"`
	WorkspaceDirectory string        `json:"workspaceDirectory"`
	ChatModelTitle     string        `json:"chatModelTitle"`
	History            []historyItem `json:"history"`
	Usage              *usage        `json:"usage"`
}

type historyItem struct {
	Message struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type usage struct {
	PromptTokens        int64 `json:"promptTokens"`
	CompletionTokens    int64 `json:"completionTokens"`
	PromptTokensDetails *struct {
		CachedTokens     int64 `json:"cachedTokens"`
		CacheWriteTokens int64 `json:"cacheWriteTokens"`
	} `json:"promptTokensDetails"`
	CompletionTokensDetails *struct {
		ReasoningTokens int64 `json:"reasoningTokens"`
	} `json:"completionTokensDetails"`
}
