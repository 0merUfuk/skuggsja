// Package webassets embeds the entire Rewind UI in the Go binary.
package webassets

import "embed"

// Files contains only same-origin, locally served assets.
//
//go:embed index.html styles.css app.js
var Files embed.FS
