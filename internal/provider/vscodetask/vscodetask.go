// Package vscodetask reads the per-task storage that Cline and Roo Code share:
// tasks/<taskId>/ui_messages.json, an array of timestamped chat events. The two
// extensions differ only in where that folder lives and in how they mark the
// opening prompt, so one parser serves both through Config.
package vscodetask

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
	"github.com/0merUfuk/skuggsja/internal/provider"
)

const (
	uiMessagesFile   = "ui_messages.json"
	metadataFile     = "task_metadata.json"
	maxMessagesBytes = 128 << 20
	maxMetadataBytes = 8 << 20
)

// Config describes one extension family member.
type Config struct {
	Harness           model.Harness
	DisplayName       string
	TaskRoots         []string
	VerificationLevel string
	Limitations       []string
	// ReadsModelMetadata is true when task_metadata.json carries model_usage.
	ReadsModelMetadata bool
}

// Reader reads every task folder under Config.TaskRoots.
type Reader struct{ Config Config }

func (r Reader) Harness() model.Harness { return r.Config.Harness }
func (r Reader) DisplayName() string    { return r.Config.DisplayName }

func (r Reader) Discover(_ context.Context) (provider.Discovery, error) {
	d := provider.Discovery{Harness: r.Config.Harness}
	seen := make(map[string]bool)
	for _, root := range r.Config.TaskRoots {
		if root == "" || seen[root] {
			continue
		}
		seen[root] = true
		d.Roots = append(d.Roots, root)
		entries, err := os.ReadDir(root)
		if os.IsNotExist(err) {
			continue
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
			path := filepath.Join(root, name, uiMessagesFile)
			info, statErr := os.Lstat(path)
			if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				continue
			}
			d.Files = append(d.Files, path)
			meta := filepath.Join(root, name, metadataFile)
			if mi, mErr := os.Lstat(meta); mErr == nil && mi.Mode().IsRegular() && mi.Mode()&os.ModeSymlink == 0 {
				d.AuditFiles = append(d.AuditFiles, meta)
			}
		}
	}
	return d, nil
}

func (r Reader) Read(ctx context.Context, d provider.Discovery) (result model.ProviderResult) {
	result = model.ProviderResult{
		Harness:           r.Config.Harness,
		DisplayName:       r.Config.DisplayName,
		Status:            "not found",
		VerificationLevel: r.Config.VerificationLevel,
		SourceFiles:       append([]string(nil), d.Files...),
		Limitations:       append([]string(nil), r.Config.Limitations...),
	}
	if len(d.Files) == 0 {
		return result
	}
	models := make(map[string][]string)
	if r.Config.ReadsModelMetadata {
		for _, path := range d.AuditFiles {
			var meta struct {
				ModelUsage []struct {
					ModelID string `json:"model_id"`
				} `json:"model_usage"`
			}
			if tooLarge, err := provider.DecodeJSONFile(path, maxMetadataBytes, &meta); tooLarge || err != nil {
				continue
			}
			var ids []string
			for _, usage := range meta.ModelUsage {
				ids = append(ids, usage.ModelID)
			}
			models[filepath.Dir(path)] = ids
		}
	}
	seenTasks := make(map[string]bool)
	for _, path := range d.Files {
		if err := ctx.Err(); err != nil {
			break
		}
		taskDir := filepath.Dir(path)
		taskID := filepath.Base(taskDir)
		if seenTasks[taskID] {
			result.AddWarning("duplicate_task_id", "A task id appeared under more than one storage root; the later copy was skipped.")
			continue
		}
		seenTasks[taskID] = true
		var events []event
		tooLarge, err := provider.DecodeJSONFile(path, maxMessagesBytes, &events)
		if tooLarge {
			result.AddWarning("oversize_task", "An oversize task history was skipped.")
			continue
		}
		if err != nil {
			result.AddWarning("malformed_task", "A malformed task history was skipped.")
			continue
		}
		session, ok := r.session(taskID, events, models[taskDir], &result)
		if ok {
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

func (r Reader) session(taskID string, events []event, modelIDs []string, result *model.ProviderResult) (model.Session, bool) {
	session := model.Session{
		Harness:       r.Config.Harness,
		ID:            taskID,
		ActivityBasis: "task message timestamps",
		Models:        make(map[string]model.ModelActivity),
	}
	hasTaskMarker := false
	for _, e := range events {
		if e.Type == "say" && e.Say == "task" {
			hasTaskMarker = true
			break
		}
	}
	var usage model.TokenUsage
	var requests int64
	for index, e := range events {
		at := time.Time{}
		if e.TS > 0 {
			at = time.UnixMilli(e.TS).UTC()
			if session.StartedAt.IsZero() || at.Before(session.StartedAt) {
				session.StartedAt = at
			}
			if at.After(session.EndedAt) {
				session.EndedAt = at
			}
		}
		if e.Type != "say" || e.Partial {
			continue
		}
		switch e.Say {
		case "task", "user_feedback":
			session.Prompts = append(session.Prompts, promptMetric(taskID, index, at, e.Text))
		case "text":
			if index == 0 && !hasTaskMarker {
				session.Prompts = append(session.Prompts, promptMetric(taskID, index, at, e.Text))
			}
		case "api_req_started", "deleted_api_reqs":
			var info apiRequest
			if json.Unmarshal([]byte(e.Text), &info) != nil {
				result.AddWarning("malformed_api_request_record", "An API request record with unreadable token counts was skipped.")
				continue
			}
			if e.Say == "api_req_started" {
				requests++
			}
			usage.Add(model.TokenUsage{
				Available: true, Exact: true,
				Input: info.TokensIn, Output: info.TokensOut,
				CacheRead: info.CacheReads, CacheWrite: info.CacheWrites,
				Source: "task API request records",
			})
		}
	}
	if len(session.Prompts) == 0 {
		return session, false
	}
	session.ActivityAt = session.EndedAt
	if session.StartedAt.IsZero() {
		session.TimeUnavailable = true
	}
	if usage.Available {
		session.Usage = usage
	}
	if id := singleModel(modelIDs); id != "" {
		activity := model.ModelActivity{Turns: requests}
		if usage.Available {
			activity.Usage = usage
		}
		session.Models[id] = activity
	}
	return session, true
}

func promptMetric(taskID string, index int, at time.Time, text string) model.PromptMetric {
	words, characters := provider.TextMetric(text)
	return model.PromptMetric{
		EventID: taskID + "#" + strconv.Itoa(index), At: at,
		Words: words, Characters: characters, HasText: strings.TrimSpace(text) != "",
	}
}

// singleModel returns the one model id a task used, or "" when the metadata
// lists none or several: mid-task model switches cannot be attributed per call.
func singleModel(ids []string) string {
	distinct := ""
	for _, id := range ids {
		id = provider.SafeLabel(id, 80)
		if id == "" {
			continue
		}
		if distinct != "" && distinct != id {
			return ""
		}
		distinct = id
	}
	return distinct
}

type event struct {
	TS      int64  `json:"ts"`
	Type    string `json:"type"`
	Say     string `json:"say"`
	Text    string `json:"text"`
	Partial bool   `json:"partial"`
}

type apiRequest struct {
	TokensIn    int64 `json:"tokensIn"`
	TokensOut   int64 `json:"tokensOut"`
	CacheWrites int64 `json:"cacheWrites"`
	CacheReads  int64 `json:"cacheReads"`
}
