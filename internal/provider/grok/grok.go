// Package grok reads Grok CLI's per-project prompt-history JSONL logs.
package grok

import (
	"context"
	"encoding/json"
	"fmt"
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

const maxLineBytes = 1 << 20 // 1 MiB; a single prompt record has no legitimate reason to exceed this.

// Reader discovers Grok CLI's per-project session directories. Grok keeps one
// subdirectory per working directory it has been run from (the cwd, URL
// path-encoded), each holding its own prompt_history.jsonl.
type Reader struct{ SessionsRoot string }

func (Reader) Harness() model.Harness { return model.Grok }
func (Reader) DisplayName() string    { return "Grok" }

// New builds the Grok reader from the machine's discovered paths. It is the
// harness's sole entry in the CLI's provider registry.
func New(paths platform.Paths) provider.Reader {
	return Reader{SessionsRoot: paths.GrokSessionsRoot}
}

func (r Reader) Discover(_ context.Context) (provider.Discovery, error) {
	d := provider.Discovery{Harness: model.Grok}
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
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		candidate := filepath.Join(r.SessionsRoot, name, "prompt_history.jsonl")
		info, statErr := os.Lstat(candidate)
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil {
			return d, statErr
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			continue
		}
		d.Files = append(d.Files, candidate)
	}
	return d, nil
}

func (r Reader) Read(ctx context.Context, d provider.Discovery) (result model.ProviderResult) {
	result = model.ProviderResult{
		Harness:           model.Grok,
		DisplayName:       "Grok",
		Status:            "not found",
		VerificationLevel: "real data on macOS",
		SourceFiles:       append([]string(nil), d.Files...),
		Limitations: []string{
			"Only human prompt events are available in this file; assistant turns, model identity and token usage are not exposed by Grok's local prompt-history log and remain unavailable rather than measured zero.",
			"A record logged while Grok was in bash mode is counted the same as a chat prompt; the source file does not separate them into different totals.",
			"Sessions are grouped by Grok's own session_id with no separate start/end record, so span is derived from the earliest and latest prompt timestamps seen.",
		},
	}
	if len(d.Files) == 0 {
		return result
	}
	sessions := make(map[string]*model.Session)
	order := make([]string, 0, len(d.Files))
	sawRecord := false
	for _, path := range d.Files {
		err := provider.ForEachLine(ctx, path, maxLineBytes, func(line []byte, tooLong bool) {
			if tooLong {
				result.AddWarning("oversize_prompt_record", "An oversize Grok prompt-history line was skipped.")
				return
			}
			var record promptRecord
			if err := json.Unmarshal(line, &record); err != nil {
				result.AddWarning("malformed_prompt_record", "A malformed Grok prompt-history line was skipped.")
				return
			}
			if record.SessionID == "" {
				result.AddWarning("missing_session_id", "A Grok prompt-history line without a session id was skipped.")
				return
			}
			sawRecord = true
			session, exists := sessions[record.SessionID]
			if !exists {
				session = &model.Session{
					Harness:       model.Grok,
					ID:            record.SessionID,
					ActivityBasis: "prompt timestamps",
					Models:        make(map[string]model.ModelActivity),
				}
				sessions[record.SessionID] = session
				order = append(order, record.SessionID)
			}
			at := parseFlexibleTime(record.Timestamp)
			words, characters := provider.TextMetric(record.Prompt)
			session.Prompts = append(session.Prompts, model.PromptMetric{
				EventID:    fmt.Sprintf("%s#%d", record.SessionID, len(session.Prompts)),
				At:         at,
				Words:      words,
				Characters: characters,
				HasText:    strings.TrimSpace(record.Prompt) != "",
			})
			if !at.IsZero() {
				if session.StartedAt.IsZero() || at.Before(session.StartedAt) {
					session.StartedAt = at
				}
				if at.After(session.EndedAt) {
					session.EndedAt = at
				}
				session.ActivityAt = session.EndedAt
			}
			if session.Project == "" {
				session.Project = provider.ProjectName(cwdFromEncodedDir(filepath.Base(filepath.Dir(path))))
			}
		})
		if err != nil {
			result.AddWarning("prompt_history_unreadable", "One of Grok's prompt-history files could not be read and was skipped.")
		}
	}
	if !sawRecord {
		if len(result.Warnings) > 0 {
			result.Status = "unavailable"
		}
		return result
	}
	result.Sessions = make([]model.Session, 0, len(order))
	for _, id := range order {
		result.Sessions = append(result.Sessions, *sessions[id])
	}
	sort.Slice(result.Sessions, func(i, j int) bool {
		return result.Sessions[i].StartedAt.Before(result.Sessions[j].StartedAt)
	})
	result.Status = "supported"
	if len(result.Warnings) > 0 {
		result.Status = "supported with warnings"
	}
	return result
}

type promptRecord struct {
	IsBash    bool   `json:"is_bash"`
	Prompt    string `json:"prompt"`
	SessionID string `json:"session_id"`
	Timestamp string `json:"timestamp"`
}

// cwdFromEncodedDir reverses Grok's URL path-encoding of the working
// directory it named the session folder after, e.g. "%2FUsers%2Fjane" ->
// "/Users/jane". An undecodable name is passed through unchanged so
// ProjectName can still take its final path component.
func cwdFromEncodedDir(name string) string {
	if decoded, err := decodePercent(name); err == nil {
		return decoded
	}
	return name
}

func decodePercent(s string) (string, error) {
	// url.PathUnescape would also accept "+" as a literal plus, which is what
	// a directory name needs; avoid importing net/url for one call.
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			value, err := strconv.ParseUint(s[i+1:i+3], 16, 8)
			if err != nil {
				return "", err
			}
			b.WriteByte(byte(value))
			i += 2
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String(), nil
}

func parseFlexibleTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.000Z"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC()
		}
	}
	if seconds, err := strconv.ParseFloat(value, 64); err == nil && seconds > 0 {
		whole := int64(seconds)
		nanos := int64((seconds - float64(whole)) * float64(time.Second))
		return time.Unix(whole, nanos).UTC()
	}
	return time.Time{}
}
