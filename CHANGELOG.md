# Changelog

All notable changes to this project will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and future releases are intended to follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html). There is no tagged release yet.

## [Unreleased]

### Added

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
- Schema-v2 provider coverage assessments that distinguish recovered local records from account-lifetime usage.

### Security

- Protect configured supplemental sources even when transcript discovery fails.

- Raw prompt/response content, absolute source paths, and internal source identifiers are excluded from the persisted report.
- SQLite libraries open only private temporary copies, never the original history database.
- SQLite copies reject symlink sources and use private Unix modes or a validated protected Windows DACL; Windows runtime verification remains pending.
- Generation and cleanup reject overlapping source roots, artifact-directory-contained source files, and path/symlink/hard-link/case aliases, including paths returned with discovery errors.
- Source auditing commits configured missing paths and attempts bounded stabilization against the captured set; persistent churn remains parseable but explicitly unverified.
- The aggregate directory uses private Unix modes or a validated protected Windows DACL; Windows runtime verification remains pending.
- Runtime assets and code perform no outbound requests after installation; build and install networking remain outside that guarantee.
- macOS runtime isolation covers all four adapters and observes/denies external socket destinations with a calibrated verification-only guard.

### Known limitations

- Codex’s paginated-history database is audited but not interpreted; presence is an explicit coverage limitation.

- All current real-data verification is macOS-only. Cursor is schema-verified with synthetic data and is not real-data verified.
- Arbitrary alternate account/profile storage homes are not guessed automatically; one configured/default Claude and Codex home is traversed per run, with explicit overrides available for known alternates.
- Provider history schemas are upstream implementation details and may require adapter updates.
- The local report server has no authentication or encryption and is not intended for remote exposure.
- Abrupt termination can leave a private SQLite copy in the OS temporary directory.
