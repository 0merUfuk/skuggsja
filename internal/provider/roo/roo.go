// Package roo reads Roo Code's per-task ui_messages.json history.
package roo

import (
	"github.com/0merUfuk/skuggsja/internal/model"
	"github.com/0merUfuk/skuggsja/internal/platform"
	"github.com/0merUfuk/skuggsja/internal/provider"
	"github.com/0merUfuk/skuggsja/internal/provider/vscodetask"
)

// New builds the Roo Code reader. It is the harness's sole entry in the CLI's
// provider registry.
func New(paths platform.Paths) provider.Reader {
	return vscodetask.Reader{Config: vscodetask.Config{
		Harness:           model.Roo,
		DisplayName:       "Roo Code",
		TaskRoots:         paths.RooTaskRoots,
		VerificationLevel: "schema-verified with synthetic fixtures",
		Limitations: []string{
			"Only the per-task tasks/<id>/ui_messages.json history under VS Code and Cursor global storage is read; a custom storage path configured in the extension is not discovered unless set through the environment override.",
			"Roo Code records no per-task model identity in the files read here, so Roo sessions carry no model breakdown.",
			"Token counts are the sums of the task's recorded API request events; tasks whose request events were never written report usage as unavailable.",
			"The layout was confirmed from Roo Code's published source, not a local installation; this machine has no Roo Code history, so non-synthetic data is not yet verified.",
		},
	}}
}
