# Changelog

All notable changes to this project will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and releases use [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.3.2] - 2026-09-15

### Fixed

- The folio rail had no background of its own, so every full-bleed band and rule painted straight through it: the inverse privacy band turned the rail dark and dropped its labels near-invisible, and the masthead's bottom hairline struck through the first folio. The rail now carries the page's own paper ground inside the 80 rem block; the page's already-painted full-bleed bands and rules are hidden behind it at every scroll offset, exactly as the mockups show.
- Claude Code's own synthetic-turn sentinel (`<synthetic>`) was passed through as if it were a model, reaching a top-two "model" slot under Claude Code on real local history. It is a transcript sentinel, not a model: it is now excluded from the model ranking while its token usage and turns remain counted in the session's totals, and a new parser-note warning (`synthetic_model_records_excluded`) accounts for the excluded records so nothing disappears silently.
- Cursor's mark was a hand-drawn three-face cube; it is now Cursor's own published mark (simple-icons, CC0-1.0), matching the treatment Claude Code and Codex already had. Hermes Agent has no public brand to source from and remains mockup-derived, but its inner triangle is now a hollow outline at a heavier stroke weight instead of a filled face, matching the supplied design asset's nested-triangle geometry.

### Changed

- The four shipped harness providers are now assembled from a small registry in `internal/cli/cli.go` instead of a hard-coded construction list. Each provider package exposes its own `New(platform.Paths) provider.Reader` constructor; supporting a harness beyond the four shipped today means writing its provider package (with fixtures, following the pattern in `internal/provider/claude`) and appending one factory to the registry — no other file changes. Provider ordering, terminal output and the UI behave exactly as before; an unrecognized harness id already rendered safely (the neutral series colour and the `unknown` diamond glyph), and that fallback now has a direct test.

## [0.3.1] - 2026-09-14

### Changed

- The chapter spine on wide viewports is one numbered folio per section hung on a single axis: an origin bead at the head, a line and a closing tick into each folio, a filled bead on the folio you are reading and a terminal diamond at the foot. The rail takes the whole left margin and hangs every folio on the centre of its own box, so every numeral and label shares one line of centres at every captured width, and the numerals are serif ink rather than signal colour. The folio numerals and labels take the rail's own display scale, matching the size the supplied mockups give them rather than body-text scale.
- Harness marks are the published monochrome geometry rather than hand-drawn approximations: Claude Code and Codex carry the upstream monochrome paths, and Hermes Agent and Cursor are drawn from the notched triangle and three-face cube the supplied design assets show. Marks are drawn in ink; the recorded value carries the harness colour.
- Below 48 rem the chapter list wraps into a table of contents instead of a horizontal strip that clipped one folio mid-word with no scroll affordance.

### Fixed

- The reading bead on the active folio was drawn at the wrong end of its line. The folio link was `position: static`, so the absolutely positioned marker resolved against the list item and landed above the line instead of at its foot beside the numeral.
- The projects-by-tool ledger drew every meter in the neutral default series colour, so the four harnesses that are colour-coded in the usage key and the model index lost that identity in the one table that names them. Each row now carries its own harness scope.
- The Cursor and Hermes meter fills measured 1.57:1 and 1.58:1 against the meter track and Codex 2.90:1, all below the 3:1 non-text minimum, so those bars read as empty track. Meters now use a per-harness bar tone that holds at least 3:1 against the track; the identity hues the bubbles use are unchanged.
- The hero recovered-sessions note wrapped at its desktop measure on narrow viewports, which left a line beginning with a separator glyph. Below 48 rem the note takes the width it has.
- The terminal diamond was drawn half outside the viewport. The fixed rail pinned it to the very bottom edge with `bottom: calc(var(--folio-dot) / -2)`, so the closing mark of the spine was cut in half at every scroll position. The foot now reserves the closing line, the mark and the same air the head bead gets, and the mark's air is measured from its rotated bounding box.

## [0.3.0] - 2026-09-14

### Changed

- The Rewind interface now implements the supplied design assets end to end. Above 80 rem a fixed chapter spine numbers and names all eight sections; the masthead carries a `SKUGGSJA / MIRROR` imprint and a boxed `LOCAL ARTIFACT · NO NETWORK` stamp; and every chapter opens with a numeral folio, a serif headline, a deck and one hairline. The hero became a ledger: the recovered-session figure and its chart beside four proof cells split by vertical rules, each with a note and a grey mini-histogram. Harness identity is drawn as one warm scale of glyphs — a Claude Code starburst, nested Codex hexagons, a Hermes triangle and a neutral Cursor cube — beside the usage ledger and every source row. Rhythm gained an eight-step heat ramp with a labelled legend over a weekday-by-date grid; prompts gained a median figure, a facts ledger, an explicit "prompt text is not stored" panel and an interpretation band; projects gained a ranked local-path list beside a projects-by-tool ledger and a longest-session aside; the report boundary became a light four-cell brief above a full-bleed inverse band; and method became a six-cell definition grid above the existing expandable wells. The source-status line is one compact ledger row and the new panels replaced the v0.2.1 group surfaces. Layout, type scale, spacing, chart marks and section treatments changed; the aggregate, the read-only language, every state and every control behave as before.

### Added

- The hero chart resolves weekly buckets once the recorded span exceeds 62 days and switches to a logarithmic axis when the record is both skewed and large enough to need one — at least four non-zero buckets, a peak bucket of at least 20 sessions and a peak at least eight times the median — drawing grid lines at powers of ten and naming the scale, the peak week and its share in the caption. A linear axis cannot show a span whose busiest week holds 4,636 of 5,465 recorded sessions.
- Proof cells carry measured mini-histograms: the active-days cell plots weekly active days, and the projects-by-tool ledger and every source row carry a session meter.

### Fixed

- The projects-by-tool ledger sat 40 px inside its own column header and its `Projects` header never lined up with its values. The list had inherited the browser's default `ol` indent and decimal markers, and header and rows sized their columns independently. The list is reset and both now share one column template, so every header sits directly over its values.
- The hero proof figures were sized from the viewport rather than their own strip, so the five-digit prompt count overflowed its content box between roughly 1150 px and 1920 px. Browser verification failed on `dd#proof-prompts` at 1280 px with a 147 px scroll width against a 136 px box. The figures and the `READ-ONLY` stamp now follow the strip's own width, which keeps the four cells equal and their values inside them at every captured width.
- `READ-ONLY` broke across the hyphen into two lines wherever its clamped size exceeded the cell. It is one line at every captured width now.
- The masthead imprint forced its artifact stamp and the `SKUGGSJA / MIRROR` mark into two lines each below roughly 480 px. Below 30 rem the imprint stacks instead of sharing a row with the wordmark.
- The files-observed line in the source-boundary band kept its near-white colour when the band repaints to white for print, so the one sentence naming the read-only inputs printed at 1.1:1 contrast. Every text colour in that band resets for print now, and the sentence measures 21:1.
- The hero proof strip is a conforming description list again. Each grouping element now holds one term and its description, with the figure, the mini-histogram and the note inside the description rather than as siblings of it. Verified against the Nu HTML checker: three errors before, none after, with the strip's measured geometry unchanged.
- The active-days mini-histogram described its slice as the largest buckets in its accessible label, but it draws the earliest ones. On a 401-week span the label read `Largest 96 of 401`; it reads `Earliest 96 of 401` now.

### Notes

- The supplied mockups show panels the aggregate cannot populate: per-harness recent-activity strips, a prompt token-size distribution, an example prompt and a redaction table. The artifact carries no per-provider daily series, no prompt text and no redaction records, so those panels were replaced with what the aggregate does measure rather than filled with invented values. The four-cell boundary brief, the definition grid and the measured mini-histograms are those replacements.

## [0.2.1] - 2026-09-13

### Changed

- Separators in the Rewind interface are now carried by tone, air and elevation instead of rules. The masthead and source-status band, the chapter bands, the usage and model panels, the coverage notices, the rhythm, prompt, projects and method sections and the colophon group their content with a background tone, one low shadow and wider spacing; the stylesheet's side-border declarations drop from 63 to 8 and its `--rule` token uses from 56 to 11. The hairlines that remain are deliberate marks — series ticks, margin markers, meter tracks, the folio-nav underline and the forced-colours legibility outline — and the section colour treatments, the sealed inverse report boundary, the type scale and every control and state are unchanged.

### Fixed

- The new grouped panels inset the narrowest layout, which pushed the shortest harness circle label to 11.9 CSS px against the documented 12 px floor at a 320 px viewport and squeezed model and project identifiers into mid-token breaks. Below 30 rem the panel inset tightens and a rank row stacks its value and meter under the name so identifiers stay whole.

## [0.2.0] - 2026-09-13

### Changed

- Rebuilt the Rewind interface around a printed-instrument art direction: full-bleed bands with one inset content column, chapter folios with numerals and a margin rail, hairline data tables in place of cards, a single sealed inverse band for the report boundary, and one signal colour over bone paper and blue-black ink. Layout, type scale, spacing, chart marks and disclosure affordances changed; the aggregate, the read-only language, every state and every control behave as before.
- The report renders with bundled Open Font License webfonts (Newsreader, IBM Plex Sans, IBM Plex Mono; latin and latin-ext subsets) that are embedded in the binary, served from loopback only, and shipped with their licence text. Unsupported glyphs fall back to the system stack, and no font is ever fetched from a network origin.

### Fixed

- Release verification resolves the unpublished draft from the release list instead of the tag endpoint, which GitHub answers with 404 for drafts. Post-publication package verification accepts the stable tag when the built `metadata.json` is not a published asset.
- The hero session figure no longer clips its own content box, so its full value is visible at every captured width.
- Weekday meter rows keep both the day label and the value wide enough for their content, so a heavy weekday can no longer widen the mobile layout or overlap the meter.
- Disclosure summaries in Method keep the expand marker beside its label and the warning count at the trailing edge instead of stranding the marker against an empty column.
- Browser verification captures full-page DPR-2 evidence in vertical tiles. A single 2x capture that exceeds Chromium's raster budget closed the DevTools socket mid-run, which made the last full-page width the verifier reached look like a product failure.
- Browser verification records browser-internal child targets (for example a Chrome component extension's background service worker) separately from product scope, and launches with `--disable-component-extensions-with-background-pages` so an unrelated component cannot fail an otherwise clean run.
- Packaging verification names every bundled webfont and the licence in each released executable, its regression fixture builds a real `web/fonts` tree, and a new case proves the comparison fails when a checkout's webfont bytes differ from the packaged payload.

## [0.1.2] - 2026-09-12

### Added

- `generator_version` in the persisted artifact, recording the producing executable (`dev` marks a source build).

### Fixed

- Hermes compaction copies of one stored prompt (identical session, exact timestamp and content) now count once instead of once per physical row. Rows that are live at the same time are never merged.
- Project ordering is deterministic when two project names differ only by case and have equal root-session counts.

### Changed

- Document the Hermes tool-call total as its stored active-transcript counter, which compaction, rewind or transcript replacement can lower.

## [0.1.1] - 2026-09-06

### Fixed

- Use singular units for one session or prompt in harness-chart hover details, chart accessibility labels and weekday meters.

### Changed

- Verify native Linux ARM64 and Windows ARM64 targets in CI, with explicit runner and toolchain architecture assertions.

## [0.1.0] - 2026-09-06

### Added

- Six-platform release archives, a checksum-derived Homebrew formula for macOS/Linux, and workflows for build provenance and tap synchronization.
- Reusable CI gates for macOS/Linux/Windows tests and installed-binary checks, package validation, workflow linting and vulnerability checks.
- Isolated installation smoke tests covering first use, synthetic counts, localhost assets/API/CSP, cleanup and reinstall/removal without reading real histories.
- Interactive harness circle chart sized by recorded sessions or prompts, with exact bar fallbacks and keyboard/touch detail. Model activity remains provider-scoped because model-level sessions/prompts are unavailable.
- Local-first Rewind generation for Claude Code, Codex, Hermes Agent, and Cursor histories.
- Per-provider root/child session, prompt, project, tool-call, and model-event summaries where exposed, plus available source-recorded token ledgers and aggregate rhythm and longest-session metrics.
- Mode-aware Codex rollout parsing for plain JSONL and zstd-compressed histories.
- Private, stable SQLite copies for Hermes, Cursor, and Codex supplemental databases, including live-WAL handling and read-only/query-only provider queries.
- Default before/after SHA-256 audit with configured absence states, recursive source-root/source-containing-directory membership, and bounded capture/rediscovery stabilization.
- Content-free versioned JSON artifact written with a synced same-directory temporary file and rename to the OS user-cache directory.
- Loopback-only embedded web report with strict response headers and no remote assets.
- Provider-scoped tool-call availability so unsupported Cursor calls are never presented as measured zero.
- Loading, error, empty, sparse, and dense report states with accessible responsive layouts, plus a visible per-harness coverage card/banner distinguishing recoverable local records from lifetime usage.
- CLI controls for JSON stdout and generate-only operation, browser launch, loopback port, source-audit opt-out, cleanup, and version output.
- Environment overrides for nonstandard provider history locations.
- Synthetic parser, aggregation, audit, SQLite-copy, server, and embedded-asset tests.
- Claude nested child-session discovery, supplemental history/state/project-index reconciliation, copied-history diagnostics, streaming-response merging, and recorded thinking-token support.
- Codex supplemental history/index/state/catalog evidence and exact boundary-validated pagination stitching.
- Per-provider coverage assessments that distinguish recovered local records from account-lifetime usage.
- An always-visible read-only source-access guarantee in CLI, JSON and UI, with neutral concurrent-source activity kept separate from release-only equality verification.
- An opt-in release verifier that performs up to eight direct generation windows with fresh per-attempt manifests and aggregates, stops at the first complete equality result, and can explicitly retain a private hash/inventory snapshot of its self-hosted Codex store while excluding only that store from release equality.
- Verification-only serving of retained aggregates and direct-CDP Chrome Headless checks for rendered values, responsive screenshots, and observed requests without rereading source histories.

### Changed

- Compact editorial UI with a bounded system-font type scale, 1280px content limit, semantic harness colors, expandable model lists and dense source records. Removed viewport-sized display typography, rotated decoration and scroll reveals.
- Require Go 1.27.1 or newer for current standard-library security fixes.
- Report schema 3 removes the global `totals.tool_calls` field. Native tool-call counts and availability remain on each provider; unavailable counts remain labeled unavailable.
- Model-event lists are grouped by harness, with ranking positions and meter scales local to each group. The overview no longer names a cross-provider leading model.
- Release equality verification starts directly without an idle preflight. Comparisons that detect activity remain in the evidence record rather than being overwritten by a later attempt.
- Long-session typography and mobile provider grids retain complete real values without horizontal clipping; desktop and mobile behavior is checked in actual Chrome.
- Release verification can explicitly measure Claude plus Cursor after a failed complete comparison, retaining separate scope evidence and labeling Hermes live equality unmeasured while ingesting all original providers.

### Fixed

- Correct Windows private SQLite-copy file URIs and verify native DACL, locked-SHM and installed-binary behavior without weakening equality checks.

- Ranked-list disclosures now show their open/closed state, offer a collapse control at the end of long lists, and return keyboard focus to the summary. Single counts use singular units in visible text and accessible labels.
- Cancellation before report persistence preserves the previous complete artifact and cleans the private workspace; later readers are not started after cancellation.
- Keyboard retries restore focus to the visible result or retry control, and chapter navigation stays hidden in loading, error and empty states.

### Security

- Protect configured supplemental sources even when transcript discovery fails.

- Raw prompt/response content, absolute source paths, and internal source identifiers are excluded from the persisted report.
- SQLite libraries open only private temporary copies, never the original history database.
- Private workspace destinations are checked against source roots, files and protected SQLite parents before creation, including when discovery fails; copy destinations are passed through context rather than a process-global environment change.
- SQLite copies reject symlink sources and use private Unix modes or a validated protected Windows DACL; synthetic Windows/x64 runtime verification passes.
- Generation and cleanup reject overlapping source roots, artifact-directory-contained source files, and path/symlink/hard-link/case aliases, including paths returned with discovery errors.
- Source observation commits configured missing paths and attempts bounded stabilization against the captured set; persistent churn remains parseable with a neutral unavailable observation and no source-integrity warning.
- The aggregate directory uses private Unix modes or a validated protected Windows DACL; synthetic Windows/x64 runtime verification passes.
- Runtime assets and code perform no outbound requests after installation; build and install networking remain outside that guarantee.
- macOS runtime isolation covers all four adapters and observes/denies external socket destinations with a calibrated verification-only guard.

### Known limitations

- Codex’s paginated-history database is audited but not interpreted; presence is an explicit coverage limitation.

- All current real-data verification is macOS-only. Cursor is schema-verified with synthetic data and is not real-data verified.
- Arbitrary archived or nested account/profile homes are not guessed automatically. Claude includes configured/canonical homes and immediate native `.claude-*` siblings; explicit overrides cover known alternates. Codex traverses its configured/default home, including recovery histories.
- Provider history schemas are upstream implementation details and may require adapter updates.
- The local report server has no authentication or encryption and is not intended for remote exposure.
- Abrupt termination can leave a private SQLite copy in the OS temporary directory.

[Unreleased]: https://github.com/0merUfuk/skuggsja/compare/v0.1.2...HEAD
[0.1.2]: https://github.com/0merUfuk/skuggsja/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/0merUfuk/skuggsja/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/0merUfuk/skuggsja/releases/tag/v0.1.0
