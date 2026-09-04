// Package cursor reads Cursor's SQLite-backed composer history.
package cursor

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/0merUfuk/skuggsja/internal/model"
	"github.com/0merUfuk/skuggsja/internal/provider"
	"github.com/0merUfuk/skuggsja/internal/sqlitecopy"
)

const (
	legacyHeadersKey = "composer.composerHeaders"
	legacyDataKey    = "composer.composerData"
)

// Reader discovers Cursor's global state database. SQLite never opens this path.
type Reader struct{ DatabasePath string }

func (Reader) Harness() model.Harness { return model.Cursor }
func (Reader) DisplayName() string    { return "Cursor" }

func (r Reader) Discover(_ context.Context) (provider.Discovery, error) {
	d := provider.Discovery{Harness: model.Cursor, Roots: []string{r.DatabasePath}}
	info, err := os.Lstat(r.DatabasePath)
	if errors.Is(err, os.ErrNotExist) {
		return d, nil
	}
	if err != nil {
		return d, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return d, errors.New("Cursor state database is a symbolic link")
	}
	if !info.Mode().IsRegular() {
		return d, errors.New("Cursor state database is not a regular file")
	}
	d.Files = []string{r.DatabasePath}
	return d, nil
}

func (r Reader) Read(ctx context.Context, d provider.Discovery) (result model.ProviderResult) {
	result = model.ProviderResult{
		Harness:           model.Cursor,
		DisplayName:       "Cursor",
		Status:            "not found",
		VerificationLevel: "schema-verified with synthetic fixtures",
		SourceFiles:       append([]string(nil), d.Files...),
		Limitations: []string{
			"Cursor does not persist trustworthy token totals in this schema, so none are reported.",
			"Only the canonical global composer store is counted; the derived conversation-search index is not merged.",
			"Model events are eligible human bubbles with a recorded model name, not a cross-harness turn unit.",
		},
	}
	if len(d.Files) == 0 {
		return result
	}
	database, err := sqlitecopy.Open(ctx, d.Files[0])
	if err != nil {
		result.Status = "unavailable"
		result.AddWarning("sqlite_snapshot_failed", "The live Cursor database could not be copied consistently and was skipped.")
		return result
	}
	defer func() {
		if err := database.Close(); err != nil {
			result.AddWarning("sqlite_snapshot_cleanup_failed", "The private Cursor database copy could not be completely removed.")
			if result.Status == "supported" {
				result.Status = "supported with warnings"
			}
		}
	}()

	sessions, err := readHeaders(ctx, database.DB)
	if err != nil {
		result.Status = "unsupported schema"
		result.AddWarning("unknown_schema", "The Cursor composer schema is not recognized by this version.")
		return result
	}
	byID := make(map[string]*model.Session, len(sessions))
	for i := range sessions {
		byID[sessions[i].ID] = &sessions[i]
	}
	messages := newMessageIndex()
	if err := readComposerMetadata(ctx, database.DB, byID, messages, &result); err != nil {
		result.AddWarning("composer_metadata_unavailable", "Some Cursor project metadata was unavailable.")
	}
	if err := readHumanBubbles(ctx, database.DB, byID, messages, &result); err != nil {
		result.AddWarning("prompt_metrics_unavailable", "Cursor prompt and model metrics could not be read; unverified empty composers were not counted.")
	}

	// A composer header alone may represent a draft or an otherwise empty shell.
	// Count a session only after observing a real human message header or bubble.
	result.Sessions = sessionsWithHumanPrompts(sessions, messages)
	result.Status = "supported"
	if len(result.Warnings) > 0 {
		result.Status = "supported with warnings"
	}
	sort.Slice(result.Sessions, func(i, j int) bool {
		if result.Sessions[i].StartedAt.Equal(result.Sessions[j].StartedAt) {
			return result.Sessions[i].ID < result.Sessions[j].ID
		}
		return result.Sessions[i].StartedAt.Before(result.Sessions[j].StartedAt)
	})
	return result
}

type composerHeader struct {
	ComposerID           string `json:"composerId"`
	CreatedAt            int64  `json:"createdAt"`
	LastUpdatedAt        int64  `json:"lastUpdatedAt"`
	IsSubagent           bool   `json:"isSubagent"`
	IsBestOfNSubcomposer bool   `json:"isBestOfNSubcomposer"`
	IsDraft              bool   `json:"isDraft"`
	IsEphemeral          bool   `json:"isEphemeral"`
	SubagentInfo         *struct {
		ParentComposerID string `json:"parentComposerId"`
	} `json:"subagentInfo"`
}

func readHeaders(ctx context.Context, db *sql.DB) ([]model.Session, error) {
	modern, modernRecognized, modernErr := readModernHeaders(ctx, db)
	legacy, legacyRecognized, legacyErr := readLegacyHeaders(ctx, db)
	if modernErr != nil && legacyErr != nil {
		return nil, errors.Join(modernErr, legacyErr)
	}
	if modernErr != nil {
		if legacyRecognized {
			return legacy, nil
		}
		return nil, modernErr
	}
	if legacyErr != nil {
		if modernRecognized {
			return modern, nil
		}
		return nil, legacyErr
	}
	if !modernRecognized && !legacyRecognized {
		return nil, errors.New("Cursor composer header tables are absent")
	}

	// Modern table rows are canonical; legacy blobs only fill IDs not yet
	// represented there during or before Cursor's migration.
	seen := make(map[string]struct{}, len(modern)+len(legacy))
	merged := make([]model.Session, 0, len(modern)+len(legacy))
	for _, group := range [][]model.Session{modern, legacy} {
		for _, session := range group {
			if _, exists := seen[session.ID]; exists {
				continue
			}
			seen[session.ID] = struct{}{}
			merged = append(merged, session)
		}
	}
	return merged, nil
}

func readModernHeaders(ctx context.Context, db *sql.DB) ([]model.Session, bool, error) {
	columns, err := tableColumns(ctx, db, "composerHeaders")
	if err != nil || len(columns) == 0 {
		return nil, false, err
	}
	if !columns["composerId"] {
		return nil, false, nil
	}

	createdExpression := "0"
	if columns["createdAt"] {
		createdExpression = "COALESCE(createdAt, 0)"
	}
	updatedExpression := "0"
	if columns["lastUpdatedAt"] {
		updatedExpression = "COALESCE(lastUpdatedAt, 0)"
	}
	subagentExpression := "0"
	if columns["isSubagent"] {
		subagentExpression = "COALESCE(isSubagent, 0)"
	}
	valueExpression := "NULL"
	if columns["value"] {
		valueExpression = "value"
	}
	query := fmt.Sprintf(`
		SELECT composerId, %s, %s, %s, %s
		FROM composerHeaders NOT INDEXED
	`, createdExpression, updatedExpression, subagentExpression, valueExpression)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, true, err
	}
	defer rows.Close()

	var sessions []model.Session
	for rows.Next() {
		var id string
		var createdAt, updatedAt int64
		var isSubagent int
		var raw []byte
		if err := rows.Scan(&id, &createdAt, &updatedAt, &isSubagent, &raw); err != nil {
			return nil, true, err
		}
		header := composerHeader{ComposerID: id, CreatedAt: createdAt, LastUpdatedAt: updatedAt, IsSubagent: isSubagent != 0}
		if len(raw) > 0 {
			var stored composerHeader
			if json.Unmarshal(raw, &stored) == nil {
				if header.ComposerID == "" {
					header.ComposerID = stored.ComposerID
				}
				if header.CreatedAt == 0 {
					header.CreatedAt = stored.CreatedAt
				}
				if header.LastUpdatedAt == 0 {
					header.LastUpdatedAt = stored.LastUpdatedAt
				}
				header.IsSubagent = header.IsSubagent || stored.IsSubagent
				header.IsBestOfNSubcomposer = stored.IsBestOfNSubcomposer
				header.IsDraft = stored.IsDraft
				header.IsEphemeral = stored.IsEphemeral
				header.SubagentInfo = stored.SubagentInfo
			}
		}
		if session, ok := sessionFromHeader(header); ok {
			sessions = append(sessions, session)
		}
	}
	return sessions, true, rows.Err()
}

func readLegacyHeaders(ctx context.Context, db *sql.DB) ([]model.Session, bool, error) {
	columns, err := tableColumns(ctx, db, "ItemTable")
	if err != nil || len(columns) == 0 {
		return nil, false, err
	}
	if !columns["key"] || !columns["value"] {
		return nil, false, nil
	}
	rows, err := db.QueryContext(ctx, `
		SELECT key, value FROM ItemTable NOT INDEXED
		WHERE key IN (?, ?)
	`, legacyHeadersKey, legacyDataKey)
	if err != nil {
		return nil, true, err
	}
	defer rows.Close()

	byKey := make(map[string][]model.Session, 2)
	for rows.Next() {
		var key string
		var raw []byte
		if err := rows.Scan(&key, &raw); err != nil {
			return nil, true, err
		}
		var stored struct {
			AllComposers []composerHeader `json:"allComposers"`
		}
		if json.Unmarshal(raw, &stored) != nil {
			continue
		}
		for _, header := range stored.AllComposers {
			if session, ok := sessionFromHeader(header); ok {
				byKey[key] = append(byKey[key], session)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, true, err
	}
	sessions := append([]model.Session(nil), byKey[legacyHeadersKey]...)
	sessions = append(sessions, byKey[legacyDataKey]...)
	return sessions, true, nil
}

func sessionFromHeader(header composerHeader) (model.Session, bool) {
	if header.ComposerID == "" || header.IsDraft || header.IsEphemeral {
		return model.Session{}, false
	}
	createdAt := header.CreatedAt
	if createdAt <= 0 {
		createdAt = header.LastUpdatedAt
	}
	startedAt := unixMilliseconds(createdAt)
	if startedAt.IsZero() {
		return model.Session{}, false
	}
	updatedAt := header.LastUpdatedAt
	if updatedAt <= 0 {
		updatedAt = createdAt
	}
	child := header.IsSubagent || header.IsBestOfNSubcomposer || header.SubagentInfo != nil
	session := model.Session{
		Harness:       model.Cursor,
		ID:            header.ComposerID,
		IsChild:       child,
		StartedAt:     startedAt,
		EndedAt:       unixMilliseconds(updatedAt),
		ActivityAt:    startedAt,
		ActivityBasis: "composer creation",
		Models:        make(map[string]model.ModelActivity),
	}
	if header.SubagentInfo != nil {
		session.ParentID = header.SubagentInfo.ParentComposerID
	}
	return session, true
}

func tableColumns(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	var query string
	switch table {
	case "composerHeaders":
		query = "PRAGMA table_info(composerHeaders)"
	case "cursorDiskKV":
		query = "PRAGMA table_info(cursorDiskKV)"
	case "ItemTable":
		query = "PRAGMA table_info(ItemTable)"
	default:
		return nil, errors.New("unsupported Cursor table name")
	}
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := make(map[string]bool)
	for rows.Next() {
		var sequence, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&sequence, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}

type composerData struct {
	ComposerID          string `json:"composerId"`
	WorkspaceIdentifier struct {
		URI string `json:"uri"`
	} `json:"workspaceIdentifier"`
	FullConversationHeadersOnly []bubbleHeader `json:"fullConversationHeadersOnly"`
}

type bubbleHeader struct {
	BubbleID      string          `json:"bubbleId"`
	Type          int             `json:"type"`
	CreatedAt     json.RawMessage `json:"createdAt"`
	IsDisplayOnly bool            `json:"isDisplayOnly"`
	IsSimulated   bool            `json:"isSimulated"`
}

type messageIndex struct {
	positions map[string]map[string]int
	canonical map[string]bool
	eligible  map[string]map[string]bool
	seen      map[string]map[string]bool
	order     map[string]map[string]int
	times     map[string]map[string]time.Time
}

func newMessageIndex() *messageIndex {
	return &messageIndex{
		positions: make(map[string]map[string]int),
		canonical: make(map[string]bool),
		eligible:  make(map[string]map[string]bool),
		seen:      make(map[string]map[string]bool),
		order:     make(map[string]map[string]int),
		times:     make(map[string]map[string]time.Time),
	}
}

func readComposerMetadata(ctx context.Context, db *sql.DB, sessions map[string]*model.Session, messages *messageIndex, result *model.ProviderResult) error {
	columns, err := tableColumns(ctx, db, "cursorDiskKV")
	if err != nil {
		return err
	}
	if !columns["key"] || !columns["value"] {
		return errors.New("cursorDiskKV is absent or incompatible")
	}
	rows, err := db.QueryContext(ctx, `
		SELECT key, value FROM cursorDiskKV NOT INDEXED
		WHERE substr(key, 1, 13) = 'composerData:'
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var raw []byte
		if err := rows.Scan(&key, &raw); err != nil {
			return err
		}
		var data composerData
		if err := json.Unmarshal(raw, &data); err != nil {
			result.AddWarning("malformed_composer", "Malformed Cursor composer metadata was skipped.")
			continue
		}
		id := strings.TrimPrefix(key, "composerData:")
		if id == "" || (data.ComposerID != "" && data.ComposerID != id) {
			result.AddWarning("mismatched_composer_id", "Cursor composer records with inconsistent IDs were skipped.")
			continue
		}
		session := sessions[id]
		if session == nil {
			continue
		}
		if data.WorkspaceIdentifier.URI != "" {
			session.Project = projectFromURI(data.WorkspaceIdentifier.URI)
		}
		if data.FullConversationHeadersOnly != nil {
			messages.canonical[id] = true
			messages.eligible[id] = make(map[string]bool, len(data.FullConversationHeadersOnly))
			messages.seen[id] = make(map[string]bool, len(data.FullConversationHeadersOnly))
			messages.order[id] = make(map[string]int, len(data.FullConversationHeadersOnly))
			messages.times[id] = make(map[string]time.Time, len(data.FullConversationHeadersOnly))
		}
		for order, header := range data.FullConversationHeadersOnly {
			if header.BubbleID == "" {
				continue
			}
			eligible := header.Type == 1 && !header.IsDisplayOnly && !header.IsSimulated
			messages.eligible[id][header.BubbleID] = eligible
			messages.order[id][header.BubbleID] = order
			messages.times[id][header.BubbleID] = parseFlexibleTime(header.CreatedAt)
		}
	}
	return rows.Err()
}

type bubble struct {
	BubbleID      string          `json:"bubbleId"`
	Type          int             `json:"type"`
	Text          string          `json:"text"`
	CreatedAt     json.RawMessage `json:"createdAt"`
	IsDisplayOnly bool            `json:"isDisplayOnly"`
	IsSimulated   bool            `json:"isSimulated"`
	ModelInfo     struct {
		ModelName string `json:"modelName"`
	} `json:"modelInfo"`
}

func readHumanBubbles(ctx context.Context, db *sql.DB, sessions map[string]*model.Session, messages *messageIndex, result *model.ProviderResult) error {
	columns, err := tableColumns(ctx, db, "cursorDiskKV")
	if err != nil {
		return err
	}
	if !columns["key"] || !columns["value"] {
		return errors.New("cursorDiskKV is absent or incompatible")
	}
	rows, err := db.QueryContext(ctx, `
		SELECT key, value FROM cursorDiskKV NOT INDEXED
		WHERE substr(key, 1, 9) = 'bubbleId:'
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var raw []byte
		if err := rows.Scan(&key, &raw); err != nil {
			return err
		}
		composerID, bubbleID, ok := splitBubbleKey(key)
		if !ok {
			result.AddWarning("malformed_bubble_key", "Cursor message records with invalid keys were skipped.")
			continue
		}
		session := sessions[composerID]
		if session == nil {
			continue
		}
		if messages.canonical[composerID] {
			if !messages.eligible[composerID][bubbleID] {
				continue
			}
			messages.seen[composerID][bubbleID] = true
		}
		var value bubble
		if err := json.Unmarshal(raw, &value); err != nil {
			result.AddWarning("malformed_bubble", "Malformed Cursor message records were skipped.")
			continue
		}
		if value.Type != 1 || value.IsDisplayOnly || value.IsSimulated {
			continue
		}
		if value.BubbleID != "" && value.BubbleID != bubbleID {
			result.AddWarning("mismatched_bubble_id", "Cursor message records with inconsistent IDs were skipped.")
			continue
		}
		at := parseFlexibleTime(value.CreatedAt)
		if at.IsZero() {
			at = messages.times[composerID][bubbleID]
		}
		words, characters := provider.TextMetric(value.Text)
		messages.upsert(session, model.PromptMetric{
			EventID: bubbleID, At: at, Words: words, Characters: characters,
			HasText: strings.TrimSpace(value.Text) != "",
		})
		modelName := provider.SafeLabel(value.ModelInfo.ModelName, 100)
		if modelName != "" {
			activity := session.Models[modelName]
			activity.Turns++
			session.Models[modelName] = activity
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	messages.warnMissingBodies(result)
	return nil
}

func (m *messageIndex) upsert(session *model.Session, prompt model.PromptMetric) {
	positions := m.positions[session.ID]
	if positions == nil {
		positions = make(map[string]int)
		m.positions[session.ID] = positions
	}
	if position, exists := positions[prompt.EventID]; exists {
		existing := session.Prompts[position]
		if prompt.At.IsZero() {
			prompt.At = existing.At
		}
		session.Prompts[position] = prompt
		return
	}
	positions[prompt.EventID] = len(session.Prompts)
	session.Prompts = append(session.Prompts, prompt)
}

func (m *messageIndex) warnMissingBodies(result *model.ProviderResult) {
	for composerID, eligible := range m.eligible {
		for bubbleID, isHuman := range eligible {
			if isHuman && !m.seen[composerID][bubbleID] {
				result.AddWarning("missing_bubble_body", "Cursor human message headers without stored bodies were skipped.")
			}
		}
	}
}

func sessionsWithHumanPrompts(sessions []model.Session, messages *messageIndex) []model.Session {
	filtered := make([]model.Session, 0, len(sessions))
	for _, session := range sessions {
		if len(session.Prompts) == 0 {
			continue
		}
		sort.Slice(session.Prompts, func(i, j int) bool {
			leftOrder, leftOrdered := messages.order[session.ID][session.Prompts[i].EventID]
			rightOrder, rightOrdered := messages.order[session.ID][session.Prompts[j].EventID]
			if leftOrdered && rightOrdered && leftOrder != rightOrder {
				return leftOrder < rightOrder
			}
			if leftOrdered != rightOrdered {
				return leftOrdered
			}
			if session.Prompts[i].At.Equal(session.Prompts[j].At) {
				return session.Prompts[i].EventID < session.Prompts[j].EventID
			}
			return session.Prompts[i].At.Before(session.Prompts[j].At)
		})
		filtered = append(filtered, session)
	}
	return filtered
}

func splitBubbleKey(key string) (composerID, bubbleID string, ok bool) {
	parts := strings.SplitN(key, ":", 3)
	if len(parts) != 3 || parts[0] != "bubbleId" || parts[1] == "" || parts[2] == "" {
		return "", "", false
	}
	return parts[1], parts[2], true
}

func projectFromURI(raw string) string {
	parsed, err := url.Parse(raw)
	if err == nil && parsed.Path != "" {
		return provider.ProjectName(filepath.FromSlash(parsed.Path))
	}
	return provider.ProjectName(raw)
}

func parseFlexibleTime(raw json.RawMessage) time.Time {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		if parsed, err := time.Parse(time.RFC3339Nano, text); err == nil {
			return parsed
		}
	}
	var milliseconds int64
	if json.Unmarshal(raw, &milliseconds) == nil {
		return unixMilliseconds(milliseconds)
	}
	return time.Time{}
}

func unixMilliseconds(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(value).UTC()
}
