// Package hermes reads Hermes Agent's canonical state.db through a private copy.
package hermes

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/0merUfuk/skuggsja/internal/model"
	"github.com/0merUfuk/skuggsja/internal/provider"
	"github.com/0merUfuk/skuggsja/internal/sqlitecopy"
)

// Reader discovers Hermes Agent's canonical state database.
type Reader struct{ DatabasePath string }

func (Reader) Harness() model.Harness { return model.Hermes }
func (Reader) DisplayName() string    { return "Hermes Agent" }

func (r Reader) Discover(_ context.Context) (provider.Discovery, error) {
	d := provider.Discovery{Harness: model.Hermes}
	if r.DatabasePath == "" {
		return d, nil
	}
	d.ConfiguredFiles = sqlitePaths(r.DatabasePath)
	d.ProtectedDirectories = []string{filepath.Dir(r.DatabasePath)}
	info, err := os.Lstat(r.DatabasePath)
	if errors.Is(err, os.ErrNotExist) {
		return d, nil
	}
	if err != nil {
		return d, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return d, errors.New("Hermes database is a symbolic link")
	}
	if !info.Mode().IsRegular() {
		return d, errors.New("Hermes database is not a regular file")
	}
	d.Files = []string{r.DatabasePath}
	return d, nil
}

func sqlitePaths(path string) []string {
	return []string{path, path + "-wal", path + "-shm", path + "-journal"}
}

func (r Reader) Read(ctx context.Context, d provider.Discovery) (result model.ProviderResult) {
	result = model.ProviderResult{
		Harness:           model.Hermes,
		DisplayName:       "Hermes Agent",
		Status:            "not found",
		VerificationLevel: "real data on macOS",
		SourceFiles:       append([]string(nil), d.Files...),
		Limitations: []string{
			"All Hermes interfaces share session semantics in state.db; channel and CLI sessions are included together.",
			"Model events are source-recorded API calls across main and auxiliary tasks, not a cross-harness turn unit.",
			"Tool calls are Hermes's stored active-transcript counter; compaction, rewind or transcript replacement can lower it, so it is not a cumulative lifetime ledger.",
			"Prompts count stored user messages: byte-identical compaction copies of one session, timestamp and content count once, and rows that are live at the same time are never merged.",
		},
	}
	if len(d.Files) == 0 {
		return result
	}
	database, err := sqlitecopy.Open(ctx, d.Files[0])
	if err != nil {
		result.Status = "unavailable"
		result.AddWarning("sqlite_snapshot_failed", "The live Hermes database could not be copied consistently and was skipped.")
		return result
	}
	defer func() {
		if err := database.Close(); err != nil {
			result.AddWarning("sqlite_snapshot_cleanup_failed", "The private Hermes database copy could not be completely removed.")
			if result.Status == "supported" {
				result.Status = "supported with warnings"
			}
		}
	}()

	sessions, sessionFallbacks, err := readSessions(ctx, database.DB)
	if err != nil {
		result.Status = "unsupported schema"
		result.AddWarning("unknown_schema", "The Hermes database schema is not recognized by this version.")
		return result
	}
	result.Status = "supported"
	result.ToolCallsAvailable = true
	byID := make(map[string]*model.Session, len(sessions))
	for i := range sessions {
		byID[sessions[i].ID] = &sessions[i]
	}

	if err := readPrompts(ctx, database.DB, byID); err != nil {
		result.AddWarning("prompt_metrics_unavailable", "Hermes prompt-length metrics could not be read; session totals remain available.")
	}
	_, err = readModelUsage(ctx, database.DB, byID)
	if err != nil {
		result.AddWarning("model_usage_unavailable", "Hermes per-model usage is unavailable in this schema.")
	}
	applySessionUsageFallback(sessions, sessionFallbacks)
	if len(result.Warnings) > 0 {
		result.Status = "supported with warnings"
	}
	result.Sessions = sessions
	sort.Slice(result.Sessions, func(i, j int) bool {
		return result.Sessions[i].StartedAt.Before(result.Sessions[j].StartedAt)
	})
	return result
}

type sessionFallback struct {
	model      string
	apiCalls   int64
	input      int64
	output     int64
	cacheRead  int64
	cacheWrite int64
	reasoning  int64
}

func readSessions(ctx context.Context, db *sql.DB) ([]model.Session, map[string]sessionFallback, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, COALESCE(parent_session_id, ''), started_at,
		       COALESCE(ended_at, last_activity_at, started_at),
		       COALESCE(git_repo_root, ''), COALESCE(cwd, ''),
		       COALESCE(tool_call_count, 0), COALESCE(model, ''),
		       COALESCE(api_call_count, 0), COALESCE(input_tokens, 0),
		       COALESCE(output_tokens, 0), COALESCE(cache_read_tokens, 0),
		       COALESCE(cache_write_tokens, 0), COALESCE(reasoning_tokens, 0)
		FROM sessions
	`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	fallbacks := make(map[string]sessionFallback)
	var sessions []model.Session
	for rows.Next() {
		var (
			session                model.Session
			started, ended         float64
			repoRoot, cwd, modelID string
			fallback               sessionFallback
		)
		if err := rows.Scan(
			&session.ID, &session.ParentID, &started, &ended, &repoRoot, &cwd,
			&session.ToolCalls, &modelID, &fallback.apiCalls, &fallback.input,
			&fallback.output, &fallback.cacheRead, &fallback.cacheWrite, &fallback.reasoning,
		); err != nil {
			return nil, nil, err
		}
		session.Harness = model.Hermes
		session.IsChild = session.ParentID != ""
		session.StartedAt = unixSeconds(started)
		session.EndedAt = unixSeconds(ended)
		session.ActivityAt = session.StartedAt
		session.ActivityBasis = "session start"
		session.Project = provider.ProjectName(repoRoot)
		if session.Project == "" {
			session.Project = provider.ProjectName(cwd)
		}
		session.Models = make(map[string]model.ModelActivity)
		fallback.model = provider.SafeLabel(modelID, 100)
		fallbacks[session.ID] = fallback
		sessions = append(sessions, session)
	}
	return sessions, fallbacks, rows.Err()
}

// readPrompts records one metric per stored prompt event. Hermes compaction
// rewrites the same message into fresh physical rows whose session, timestamp
// and content are byte-exact copies, so the stored event — not the row id — is
// the identity. Rows that are live at the same time are never merged: a group
// holding several active rows is counted once per active row.
func readPrompts(ctx context.Context, db *sql.DB, sessions map[string]*model.Session) error {
	rows, err := db.QueryContext(ctx, `
		SELECT session_id, timestamp, content,
		       SUM(CASE WHEN active = 1 THEN 1 ELSE 0 END)
		FROM messages
		WHERE role = 'user' AND COALESCE(_compressed_summary, 0) = 0
		  AND content IS NOT NULL AND content != ''
		GROUP BY session_id, timestamp, content
		ORDER BY MIN(id)
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var sessionID, content string
		var timestamp float64
		var liveRows int64
		if err := rows.Scan(&sessionID, &timestamp, &content, &liveRows); err != nil {
			return err
		}
		session := sessions[sessionID]
		if session == nil {
			continue
		}
		words, characters := provider.TextMetric(content)
		identity := promptIdentity(sessionID, timestamp, content)
		count := liveRows
		if count < 1 {
			count = 1
		}
		for index := int64(0); index < count; index++ {
			eventID := identity
			if count > 1 {
				eventID = fmt.Sprintf("%s#%d", identity, index)
			}
			session.Prompts = append(session.Prompts, model.PromptMetric{
				EventID: eventID, At: unixSeconds(timestamp),
				Words: words, Characters: characters, HasText: true,
			})
		}
	}
	return rows.Err()
}

// promptIdentity is a transient, content-free key for one stored prompt event.
// The digest keeps the key bounded without putting raw text in the model.
func promptIdentity(sessionID string, timestamp float64, content string) string {
	digest := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x:%x:%s", math.Float64bits(timestamp), digest[:8], sessionID)
}

func readModelUsage(ctx context.Context, db *sql.DB, sessions map[string]*model.Session) (int, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT session_id, model, COALESCE(api_call_count, 0),
		       COALESCE(input_tokens, 0), COALESCE(output_tokens, 0),
		       COALESCE(cache_read_tokens, 0), COALESCE(cache_write_tokens, 0),
		       COALESCE(reasoning_tokens, 0)
		FROM session_model_usage
	`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var sessionID, modelID string
		var calls, input, output, cacheRead, cacheWrite, reasoning int64
		if err := rows.Scan(&sessionID, &modelID, &calls, &input, &output, &cacheRead, &cacheWrite, &reasoning); err != nil {
			return count, err
		}
		session := sessions[sessionID]
		if session == nil {
			continue
		}
		usage := model.TokenUsage{
			Available: true, Exact: true, Input: input, Output: output,
			CacheRead: cacheRead, CacheWrite: cacheWrite, Reasoning: reasoning,
			Source: "session_model_usage",
		}
		session.Usage.Add(usage)
		modelName := provider.SafeLabel(modelID, 100)
		if modelName != "" {
			activity := session.Models[modelName]
			activity.Turns += calls
			activity.Usage.Add(usage)
			session.Models[modelName] = activity
		}
		count++
	}
	return count, rows.Err()
}

func applySessionUsageFallback(sessions []model.Session, fallbacks map[string]sessionFallback) {
	for i := range sessions {
		if sessions[i].Usage.Available {
			continue
		}
		fallback := fallbacks[sessions[i].ID]
		usage := model.TokenUsage{
			Available: true, Exact: true, Input: fallback.input, Output: fallback.output,
			CacheRead: fallback.cacheRead, CacheWrite: fallback.cacheWrite,
			Reasoning: fallback.reasoning, Source: "sessions token columns",
		}
		sessions[i].Usage = usage
		if fallback.model != "" {
			sessions[i].Models[fallback.model] = model.ModelActivity{Turns: fallback.apiCalls, Usage: usage}
		}
	}
}

func unixSeconds(value float64) time.Time {
	seconds := int64(value)
	nanos := int64((value - float64(seconds)) * float64(time.Second))
	return time.Unix(seconds, nanos).UTC()
}
