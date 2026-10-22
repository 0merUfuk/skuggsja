// Package copilot reads GitHub Copilot Chat's SQLite-backed session store
// from inside VS Code's per-extension global storage.
package copilot

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/0merUfuk/skuggsja/internal/model"
	"github.com/0merUfuk/skuggsja/internal/platform"
	"github.com/0merUfuk/skuggsja/internal/provider"
	"github.com/0merUfuk/skuggsja/internal/sqlitecopy"
)

// Reader discovers GitHub Copilot Chat's session-store database. SQLite
// never opens this path; sqlitecopy takes a private, read-only copy first.
type Reader struct{ DatabasePath string }

func (Reader) Harness() model.Harness { return model.Copilot }
func (Reader) DisplayName() string    { return "GitHub Copilot" }

// New builds the GitHub Copilot reader from the machine's discovered paths.
// It is the harness's sole entry in the CLI's provider registry.
func New(paths platform.Paths) provider.Reader {
	return Reader{DatabasePath: paths.CopilotChatDatabase}
}

func (r Reader) Discover(_ context.Context) (provider.Discovery, error) {
	d := provider.Discovery{Harness: model.Copilot}
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
		return d, errors.New("Copilot Chat session store is a symbolic link")
	}
	if !info.Mode().IsRegular() {
		return d, errors.New("Copilot Chat session store is not a regular file")
	}
	d.Files = []string{r.DatabasePath}
	return d, nil
}

func sqlitePaths(path string) []string {
	return []string{path, path + "-wal", path + "-shm", path + "-journal"}
}

func (r Reader) Read(ctx context.Context, d provider.Discovery) (result model.ProviderResult) {
	result = model.ProviderResult{
		Harness:           model.Copilot,
		DisplayName:       "GitHub Copilot",
		Status:            "not found",
		VerificationLevel: "schema-verified with synthetic fixtures",
		SourceFiles:       append([]string(nil), d.Files...),
		Limitations: []string{
			"Only the VS Code (github.copilot-chat extension) session store is read; the separate Copilot CLI's own session-state directory is not covered yet.",
			"This schema does not expose a per-turn model identity or token counts, so Copilot sessions carry no model breakdown and no usage figures.",
			"Only the stable VS Code host's global storage is checked; Copilot Chat running inside VS Code Insiders or a different VS Code-family fork is not counted here.",
			"The real schema was confirmed against a live installation, but that installation had no recorded chat turns yet, so parsing of non-empty sessions is verified against synthetic fixtures rather than observed real content.",
		},
	}
	if len(d.Files) == 0 {
		return result
	}
	database, err := sqlitecopy.Open(ctx, d.Files[0])
	if err != nil {
		result.Status = "unavailable"
		result.AddWarning("sqlite_snapshot_failed", "The live Copilot Chat session store could not be copied consistently and was skipped.")
		return result
	}
	defer func() {
		if err := database.Close(); err != nil {
			result.AddWarning("sqlite_snapshot_cleanup_failed", "The private Copilot Chat session-store copy could not be completely removed.")
			if result.Status == "supported" {
				result.Status = "supported with warnings"
			}
		}
	}()

	sessions, err := readSessions(ctx, database.DB)
	if err != nil {
		result.Status = "unsupported schema"
		result.AddWarning("unknown_schema", "The Copilot Chat session-store schema is not recognized by this version.")
		return result
	}
	byID := make(map[string]*model.Session, len(sessions))
	for i := range sessions {
		byID[sessions[i].ID] = &sessions[i]
	}
	if err := readTurns(ctx, database.DB, byID); err != nil {
		result.AddWarning("prompt_metrics_unavailable", "Copilot Chat prompt metrics could not be read; session totals remain available.")
	}

	result.Sessions = sessionsWithPrompts(sessions)
	result.Status = "supported"
	if len(result.Warnings) > 0 {
		result.Status = "supported with warnings"
	}
	sort.Slice(result.Sessions, func(i, j int) bool {
		return result.Sessions[i].StartedAt.Before(result.Sessions[j].StartedAt)
	})
	return result
}

func readSessions(ctx context.Context, db *sql.DB) ([]model.Session, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, COALESCE(cwd, ''), COALESCE(created_at, ''), COALESCE(updated_at, '')
		FROM sessions
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []model.Session
	for rows.Next() {
		var id, cwd, createdAt, updatedAt string
		if err := rows.Scan(&id, &cwd, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		if id == "" {
			continue
		}
		started := parseTimestamp(createdAt)
		ended := parseTimestamp(updatedAt)
		if ended.IsZero() {
			ended = started
		}
		sessions = append(sessions, model.Session{
			Harness:       model.Copilot,
			ID:            id,
			StartedAt:     started,
			EndedAt:       ended,
			ActivityAt:    started,
			ActivityBasis: "session creation",
			Project:       provider.ProjectName(cwd),
			Models:        make(map[string]model.ModelActivity),
		})
	}
	return sessions, rows.Err()
}

// readTurns reads only the columns needed for prompt metrics. The assistant
// response text is never selected: skuggsja does not persist raw content.
func readTurns(ctx context.Context, db *sql.DB, sessions map[string]*model.Session) error {
	rows, err := db.QueryContext(ctx, `
		SELECT session_id, turn_index, COALESCE(user_message, ''), COALESCE(timestamp, '')
		FROM turns
		ORDER BY session_id, turn_index
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var sessionID, message, timestamp string
		var turnIndex int64
		if err := rows.Scan(&sessionID, &turnIndex, &message, &timestamp); err != nil {
			return err
		}
		session := sessions[sessionID]
		if session == nil {
			continue
		}
		words, characters := provider.TextMetric(message)
		at := parseTimestamp(timestamp)
		session.Prompts = append(session.Prompts, model.PromptMetric{
			EventID:    fmt.Sprintf("%s#%d", sessionID, turnIndex),
			At:         at,
			Words:      words,
			Characters: characters,
			HasText:    strings.TrimSpace(message) != "",
		})
		if !at.IsZero() && at.After(session.EndedAt) {
			session.EndedAt = at
		}
	}
	return rows.Err()
}

func sessionsWithPrompts(sessions []model.Session) []model.Session {
	filtered := make([]model.Session, 0, len(sessions))
	for _, session := range sessions {
		if len(session.Prompts) == 0 {
			continue
		}
		filtered = append(filtered, session)
	}
	return filtered
}

// parseTimestamp accepts the session-store's SQLite default format
// (strftime('%Y-%m-%dT%H:%M:%fZ')) plus standard RFC3339 variants, since a
// future schema version could switch between them.
func parseTimestamp(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	layouts := []string{
		"2006-01-02T15:04:05.000Z",
		time.RFC3339Nano,
		time.RFC3339,
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}
