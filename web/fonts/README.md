# Bundled webfonts

`web/fonts` holds the eight webfont files the Rewind UI renders with, so the
report keeps its typographic voice without a remote font request.

| File | Bytes | SHA-256 (prefix) |
| --- | --- | --- |
| `newsreader-italic-latin-var.woff2` | 147060 | `a99fb127682b9af5…` |
| `newsreader-latin-var.woff2` | 131848 | `01817351be3edfc1…` |
| `plex-mono-latin-400.woff2` | 10052 | `c36f509c0a8f9f85…` |
| `plex-mono-latin-600.woff2` | 10120 | `ad4580d8cb4b5f62…` |
| `plex-mono-latin-ext-400.woff2` | 8860 | `f1050dc5317b4343…` |
| `plex-mono-latin-ext-600.woff2` | 8960 | `1b6b18fd0fd240bc…` |
| `plex-sans-latin-ext-var.woff2` | 25868 | `ae1d854fefa1167a…` |
| `plex-sans-latin-var.woff2` | 40240 | `056e4e2459f57a00…` |

Total bundled webfont weight: 383008 bytes in 8 files.

## Source

Both families were retrieved from the Google Fonts CSS API and saved unchanged:

- **Newsreader** (v26) — display serif, variable weight 300–700 with an italic
  variable face. `newsreader-latin-var.woff2`, `newsreader-italic-latin-var.woff2`.
- **IBM Plex Sans** (v23) — interface and body text, variable weight 300–700.
  `plex-sans-latin-var.woff2`, `plex-sans-latin-ext-var.woff2`.
- **IBM Plex Mono** (v19) — labels, figures and data tables, static 400 and 600.
  Four files, `latin` and `latin-ext` subsets.

Only the `latin` and `latin-ext` subsets are bundled; other upstream subsets
were intentionally dropped to keep the binary small. Each `@font-face` in
`web/styles.css` declares the matching `unicode-range`, so a glyph outside these
subsets falls back to `styles.css`'s system stack instead of a remote request.

## Redistribution

The files are the unmodified upstream subsets and are redistributed under the
SIL Open Font License 1.1 (see `OFL.txt`, which is embedded in every build next
to the fonts). They are served from the loopback origin only, out of the binary
that embeds them; the UI never fetches a font from a network origin.
