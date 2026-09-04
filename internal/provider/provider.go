// Package provider defines the two-step discovery/read contract used to audit
// source files before any parser opens them.
package provider

import (
	"context"

	"github.com/0merUfuk/skuggsja/internal/model"
)

// Discovery lists only the files a reader intends to open. Paths stay in
// process memory and are never serialized into the generated Rewind.
type Discovery struct {
	Harness model.Harness
	Files   []string
	Roots   []string
	Meta    map[string]string
}

// Reader discovers and parses one local harness.
type Reader interface {
	Harness() model.Harness
	DisplayName() string
	Discover(context.Context) (Discovery, error)
	Read(context.Context, Discovery) model.ProviderResult
}
