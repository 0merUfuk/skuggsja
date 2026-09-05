// Package provider defines the two-step discovery/read contract used to audit
// source files before any parser opens them.
package provider

import (
	"context"

	"github.com/0merUfuk/skuggsja/internal/model"
)

// Discovery separates parsed files, audit-only inputs, and configured paths.
// ConfiguredFiles may include absent optional paths so output-safety checks
// cannot create an artifact where a harness may later create source data.
// Paths stay in process memory and are never serialized into the Rewind.
type Discovery struct {
	Harness         model.Harness
	Files           []string
	AuditFiles      []string
	Roots           []string
	ConfiguredFiles []string
	// ProtectedDirectories prohibit output/private-copy creation inside source
	// stores, including SQLite parents. They do not expand discovery or auditing.
	// Readers declare them before fallible inspection, even for absent databases.
	ProtectedDirectories []string
	Meta                 map[string]string
}

// Reader discovers and parses one local harness.
type Reader interface {
	Harness() model.Harness
	DisplayName() string
	Discover(context.Context) (Discovery, error)
	Read(context.Context, Discovery) model.ProviderResult
}
