// Package aider reads Aider's per-repository chat history markdown.
//
// Aider has no global store. Each repository gets its own
// .aider.chat.history.md, so discovery looks in the configured roots and in
// the immediate children of each root rather than walking the disk.
package aider

import (
	"context"
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
	historyName  = ".aider.chat.history.md"
	maxLineBytes = 4 << 20
	startPrefix  = "# aider chat started at "
	userPrefix   = "#### "
	timeLayout   = "2006-01-02 15:04:05"
)

// Reader finds history files in each root and in each root's direct children.
// Roots are never reported as audit or source roots: the default root is the
// home directory, which must neither be walked nor treated as a source store.
// Only the history files themselves are audited.
type Reader struct{ Roots []string }

func (Reader) Harness() model.Harness { return model.Aider }
func (Reader) DisplayName() string    { return "Aider" }

// New builds the Aider reader from the machine's discovered paths. It is the
// harness's sole entry in the CLI's provider registry.
func New(paths platform.Paths) provider.Reader {
	return Reader{Roots: paths.AiderRoots}
}

func (r Reader) Discover(_ context.Context) (provider.Discovery, error) {
	d := provider.Discovery{Harness: model.Aider}
	seen := make(map[string]bool)
	add := func(path string) {
		if seen[path] {
			return
		}
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return
		}
		seen[path] = true
		d.Files = append(d.Files, path)
	}
	for _, root := range r.Roots {
		if root == "" {
			continue
		}
		info, err := os.Stat(root)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			add(root)
			continue
		}
		add(filepath.Join(root, historyName))
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		var children []string
		for _, entry := range entries {
			if entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
				children = append(children, entry.Name())
			}
		}
		sort.Strings(children)
		for _, child := range children {
			add(filepath.Join(root, child, historyName))
		}
	}
	return d, nil
}

func (r Reader) Read(ctx context.Context, d provider.Discovery) (result model.ProviderResult) {
	result = model.ProviderResult{
		Harness:           model.Aider,
		DisplayName:       "Aider",
		Status:            "not found",
		VerificationLevel: "schema-verified with synthetic fixtures",
		SourceFiles:       append([]string(nil), d.Files...),
		Limitations: []string{
			"Aider keeps a history file per repository, so only repositories under the configured roots (the home directory and its direct subfolders by default; extend with the SKUGGSJA_AIDER_ROOTS environment variable) are found.",
			"Only the human prompts and session start times written to .aider.chat.history.md are available. Model identity, token usage and tool calls are not recorded there and are unavailable.",
			"Session start times are written without a timezone and are read in this machine's local timezone; prompts carry no timestamps of their own and a session's duration is not measured.",
			"A prompt is a line starting with '#### ' and ending in the two trailing spaces Aider appends to user input, so ordinary level-four headings in an assistant reply are not counted.",
			"The format was confirmed from Aider's published source, not a local installation; this machine has no Aider history, so non-synthetic data is not yet verified.",
		},
	}
	if len(d.Files) == 0 {
		return result
	}
	for fileIndex, path := range d.Files {
		if err := ctx.Err(); err != nil {
			break
		}
		sessions, err := parse(ctx, path, fileIndex, &result)
		if err != nil {
			result.AddWarning("history_unreadable", "An Aider history file could not be read and was skipped.")
			continue
		}
		result.Sessions = append(result.Sessions, sessions...)
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

func parse(ctx context.Context, path string, fileIndex int, result *model.ProviderResult) ([]model.Session, error) {
	project := provider.ProjectName(filepath.Dir(path))
	var sessions []model.Session
	var current *model.Session
	var pending []string
	flush := func() {
		if current == nil || len(pending) == 0 {
			pending = nil
			return
		}
		text := strings.Join(pending, "\n")
		pending = nil
		words, characters := provider.TextMetric(text)
		current.Prompts = append(current.Prompts, model.PromptMetric{
			EventID: current.ID + "#" + strconv.Itoa(len(current.Prompts)),
			Words:   words, Characters: characters, HasText: strings.TrimSpace(text) != "" && text != "<blank>",
		})
	}
	closeSession := func() {
		flush()
		if current != nil && len(current.Prompts) > 0 {
			sessions = append(sessions, *current)
		}
		current = nil
	}
	err := provider.ForEachLine(ctx, path, maxLineBytes, func(line []byte, tooLong bool) {
		if tooLong {
			result.AddWarning("oversize_history_line", "An oversize Aider history line was skipped.")
			return
		}
		text := string(line)
		if strings.HasPrefix(text, startPrefix) {
			closeSession()
			started, parseErr := time.ParseInLocation(timeLayout, strings.TrimSpace(strings.TrimPrefix(text, startPrefix)), time.Local)
			current = &model.Session{
				Harness: model.Aider,
				ID:      strconv.Itoa(fileIndex) + "-" + strconv.Itoa(len(sessions)),
				Project: project, ActivityBasis: "chat session start time",
				Models: make(map[string]model.ModelActivity),
			}
			if parseErr != nil {
				current.TimeUnavailable = true
				result.AddWarning("unreadable_session_start", "An Aider session start time could not be read; the session is counted without a timestamp.")
			} else {
				current.StartedAt = started.UTC()
				current.EndedAt, current.ActivityAt = current.StartedAt, current.StartedAt
			}
			return
		}
		if current == nil {
			return
		}
		if strings.HasPrefix(text, userPrefix) && strings.HasSuffix(text, "  ") {
			pending = append(pending, strings.TrimSuffix(strings.TrimPrefix(text, userPrefix), "  "))
			return
		}
		flush()
	})
	closeSession()
	return sessions, err
}
