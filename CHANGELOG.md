# Changelog

All notable changes to this project will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and releases use [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/0merUfuk/skuggsja/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/0merUfuk/skuggsja/releases/tag/v0.1.0
