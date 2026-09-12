# Changelog

All notable changes to this project will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and releases use [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
