// Package model defines the privacy-safe boundary between ingestion and analytics.
// Raw prompts and responses must never enter these types.
package model

import "time"

// Harness identifies an agent whose local history can be read.
type Harness string

const (
	Claude Harness = "claude"
	Codex  Harness = "codex"
	Hermes Harness = "hermes"
	Cursor Harness = "cursor"
)

// PromptMetric is all that survives after a raw prompt is processed in memory.
type PromptMetric struct {
	EventID    string
	At         time.Time
	Words      int
	Characters int
	HasText    bool
}

// TokenUsage contains exact source-recorded counts. Available remains false when
// a harness does not expose compatible fields.
type TokenUsage struct {
	Available  bool   `json:"available"`
	Exact      bool   `json:"exact"`
	Input      int64  `json:"input"`
	Output     int64  `json:"output"`
	CacheRead  int64  `json:"cache_read"`
	CacheWrite int64  `json:"cache_write"`
	Reasoning  int64  `json:"reasoning"`
	Source     string `json:"source"`
}

// Add combines compatible usage records from the same harness.
func (u *TokenUsage) Add(other TokenUsage) {
	if !other.Available {
		return
	}
	if !u.Available {
		*u = other
		return
	}
	u.Available = true
	u.Exact = u.Exact && other.Exact
	u.Input += other.Input
	u.Output += other.Output
	u.CacheRead += other.CacheRead
	u.CacheWrite += other.CacheWrite
	u.Reasoning += other.Reasoning
	if u.Source == "" {
		u.Source = other.Source
	}
}

// ModelActivity records comparable model-turn counts and, when exposed, usage.
type ModelActivity struct {
	Turns int64
	Usage TokenUsage
}

// CallMetric is a content-free API response/tool-call record. Stable source IDs
// permit global deduplication when a harness copies history during forks.
type CallMetric struct {
	ID      string
	Model   string
	Usage   TokenUsage
	ToolIDs []string
}

// Session is the normalized, content-free representation of a source session.
type Session struct {
	Harness       Harness
	ID            string
	ParentID      string
	IsChild       bool
	StartedAt     time.Time
	EndedAt       time.Time
	ActivityAt    time.Time
	ActivityBasis string
	Project       string
	Prompts       []PromptMetric
	Calls         []CallMetric
	Models        map[string]ModelActivity
	Usage         TokenUsage
	ToolCalls     int64
}

// Warning is deliberately aggregate-only so source paths or content cannot leak.
type Warning struct {
	Code    string `json:"code"`
	Count   int    `json:"count"`
	Message string `json:"message"`
}

// ProviderResult is one harness's normalized output and operational metadata.
type ProviderResult struct {
	Harness           Harness
	DisplayName       string
	Status            string
	VerificationLevel string
	Sessions          []Session
	SourceFiles       []string
	Warnings          []Warning
	Limitations       []string
}

// AddWarning increments a warning without retaining record-specific details.
func (r *ProviderResult) AddWarning(code, message string) {
	for i := range r.Warnings {
		if r.Warnings[i].Code == code {
			r.Warnings[i].Count++
			return
		}
	}
	r.Warnings = append(r.Warnings, Warning{Code: code, Count: 1, Message: message})
}
