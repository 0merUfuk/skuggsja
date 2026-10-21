// Package cline reads Cline's per-task ui_messages.json history.
package cline

import (
	"github.com/0merUfuk/skuggsja/internal/model"
	"github.com/0merUfuk/skuggsja/internal/platform"
	"github.com/0merUfuk/skuggsja/internal/provider"
	"github.com/0merUfuk/skuggsja/internal/provider/vscodetask"
)

// New builds the Cline reader. It is the harness's sole entry in the CLI's
// provider registry.
func New(paths platform.Paths) provider.Reader {
	return vscodetask.Reader{Config: vscodetask.Config{
		Harness:            model.Cline,
		DisplayName:        "Cline",
		TaskRoots:          paths.ClineTaskRoots,
		ReadsModelMetadata: true,
		VerificationLevel:  "schema-verified with synthetic fixtures",
		Limitations: []string{
			"Only the per-task tasks/<id>/ui_messages.json history is read (VS Code and Cursor global storage, plus the Cline data directory). Sessions that current SDK-based Cline releases keep only in their own SQLite session store are not read.",
			"A task is attributed to a model only when its task_metadata.json lists exactly one model; tasks that switched models carry no model breakdown.",
			"Token counts are the sums of the task's recorded API request events; tasks whose request events were never written report usage as unavailable.",
			"The layout was confirmed from Cline's published source, not a local installation; this machine has no Cline history, so non-synthetic data is not yet verified.",
		},
	}}
}
