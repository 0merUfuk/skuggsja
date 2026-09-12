// Package webassets embeds the entire Rewind UI in the Go binary.
package webassets

import "embed"

// Files contains only same-origin, locally served assets: the three UI files,
// the bundled Open Font License webfonts, and their licence text. The report
// renders its typographic voice out of these files and never fetches a font
// from a network origin.
//
//go:embed index.html styles.css app.js fonts/*.woff2 fonts/OFL.txt
var Files embed.FS
