# Architecture

Skuggsja is a single Go process with three boundaries: source adapters, a content-free normalized model, and a persisted aggregate report. The browser receives only that final report through a loopback-only server.

## Data flow

```text
OS path resolution
        │
        ▼
provider discovery ───────► source-file list (memory only)
        │                            │
        │                            ▼
        │                   before source audit
        ▼
provider readers ─────────► normalized sessions (memory only)
        │                            │
        │                            ▼
        │                    after source audit
        ▼
analytics aggregation ────► Report (content-free contract)
                                  │
                     ┌────────────┴────────────┐
                     ▼                         ▼
       temp-and-rename JSON          loopback `/api/rewind`
                                               │
                                               ▼
                                      embedded HTML/CSS/JS
```

The ordering matters. Every reader must disclose its intended files during `Discover` before `Read` opens them. With the source audit enabled, Skuggsja hashes those files and their containing-directory listings before parsing, repeats the capture afterward, and records only aggregate comparison results.

## Package map

| Package | Responsibility |
| --- | --- |
| `cmd/skuggsja` | Process entry point, signal-aware context, version string, exit behavior |
| `internal/cli` | Cobra command surface, flags, environment overrides, terminal summary |
| `internal/platform` | OS-specific default paths |
| `internal/provider` | Discovery/reader interface, bounded JSONL reading, label sanitization, transient text metrics |
| `internal/provider/{claude,codex,hermes,cursor}` | Harness-specific discovery and parsing |
| `internal/sqlitecopy` | Stable private copies of live SQLite databases and sidecars |
| `internal/model` | Content-free in-memory normalization boundary |
| `internal/audit` | Before/after SHA-256 snapshots and directory-membership comparison |
| `internal/analytics` | Deduplication, aggregation, rhythm, prompt style, and serialized `Report` contract |
| `internal/app` | Generation orchestration, temp-and-rename output, loopback server, security headers |
| `web` | Embedded, dependency-free Rewind UI |

Internal packages deliberately prevent downstream consumers from treating provider source formats as public APIs.

## Dependencies

The runtime dependency surface is deliberately small:

- `github.com/spf13/cobra` provides CLI parsing and help;
- `modernc.org/sqlite` provides a pure-Go SQLite driver for private database copies;
- `github.com/klauspost/compress/zstd` streams compressed Codex rollouts with a decoder-memory bound;
- `golang.org/x/sys/windows` creates and validates private SQLite-copy ACLs on Windows.

The web UI has no package-manager dependencies, remote fonts, remote scripts, or runtime assets outside the binary. Standard-library packages provide JSON parsing, hashing, filesystems, embedded assets, and the loopback HTTP server.

## Generation lifecycle

1. `platform.DefaultPaths` resolves candidate locations without network access.
2. CLI environment overrides replace individual locations.
3. Each provider performs discovery. Missing paths produce an empty discovery rather than an error.
4. The source/output guard rejects a fixed artifact path inside a successfully discovered source root or aliased to a source file.
5. If enabled and at least one discovery file or root was supplied, `audit.Capture` hashes every existing discovered regular file and relevant SQLite sidecar, and inventories existing discovery-root and containing-directory listings. A directory-only snapshot remains unverified because verification requires at least one hashed file.
6. Readers parse each discovery into `model.ProviderResult`. Provider failures become scoped statuses and aggregate warnings where possible.
7. SQLite readers call `sqlitecopy.Open`; the SQLite driver never receives the original database path.
8. A second audit snapshot is compared with the first. An audit failure adds a `system` provider warning rather than suppressing otherwise usable analytics.
9. `analytics.Build` creates the serializable `Report` and drops internal identifiers and source paths.
10. `WriteReport` writes a temporary file beside the destination, syncs it, and replaces the artifact. Unix-like systems rename over the old file; Windows removes the old artifact immediately before rename.
11. Unless `--once` or `--json` was selected, `Serve` binds `127.0.0.1`, serves the in-memory report and embedded assets, and runs until cancellation.

Provider discovery and reads currently run in deterministic provider order. File-oriented readers and the source hasher use at most six workers. Each copied SQLite database is opened with one connection and `PRAGMA query_only = ON` after `PRAGMA quick_check` succeeds.

## Provider adapters

### Claude Code

Claude discovery selects only files shaped as `<projects-root>/<encoded-project>/<session>.jsonl`. Nested transcript files are intentionally excluded. Each selected file is streamed as bounded JSONL.

The adapter derives:

- session start/end from valid record timestamps;
- project basename from `cwd`;
- human prompts from eligible user-message text or supported attachment blocks, excluding metadata, compact summaries, visible-transcript-only records, and tool-result echoes;
- provider-native model events and the source-recorded token ledger from assistant messages;
- distinct tool calls from assistant `tool_use` block IDs.

Assistant responses, prompt events, and tool IDs are deduplicated where stable source IDs exist. Copied fork history generates aggregate warnings; conflicting copies retain one concrete record rather than synthesizing a value.

### Codex

Codex discovery walks active and archived roots for `rollout-*.jsonl` and `rollout-*.jsonl.zst`. Plain and compressed siblings are one logical rollout; the plain file wins when both exist. Compressed input is streamed through a memory-bounded zstd decoder before the same bounded-line parser.

The adapter uses the first `session_meta` record as rollout identity and physical start, detects children from parent/thread metadata, derives project basename from `cwd`, and counts provider-native model events from turn context. The end is the latest valid record timestamp. Prompt extraction is history-mode-aware: legacy histories use human `user_message` events, while paginated histories use completed `UserMessage` items. Unknown modes skip prompt extraction and emit a warning rather than risk double-counting copied user representations. `response_item` records are used for tool calls, not as a second prompt source.

Token usage is the largest/final cumulative `total_token_usage` snapshot recorded for the session. It is exact as a session total but cannot be assigned exactly to individual model events.

### Hermes Agent

Hermes discovers its canonical `state.db`, opens a stable private copy, and queries the `sessions`, `messages`, and `session_model_usage` tables. The adapter uses `git_repo_root`, falling back to `cwd`, for the project basename. Parent IDs identify child sessions.

Per-model usage is preferred. If that table is absent or empty, session-level model and token columns are used as a fallback. An unrecognized core `sessions` schema makes the provider unsupported; unavailable optional metrics become warnings while session totals remain usable.

All Hermes interfaces that write the same state database share these session semantics, so channel and CLI histories are not separated.

### Cursor

Cursor discovers only the canonical global `state.vscdb`. It opens a private copy and feature-detects modern `composerHeaders` plus legacy `ItemTable` header blobs within that database, with modern records taking precedence during deduplication. It does not merge the derived conversation-search database or per-workspace legacy stores.

Composer header records supply identity, creation/update times, and subagent status. Draft and ephemeral headers are excluded, and a composer is counted only after an eligible human bubble body is found. Canonical `fullConversationHeadersOnly` entries constrain eligibility, order, and fallback time; a missing body is skipped with a warning rather than counted. Hydrated eligible human bubbles provide transient text metrics and provider-native model events. Composer metadata is used only to derive a final project-directory name. The adapter does not estimate tokens because the supported source structures do not expose trustworthy totals.

Cursor is schema-verified with synthetic SQLite fixtures and is not real-data verified. Unknown schemas are reported as unsupported rather than guessed.

## In-memory normalization boundary

`internal/model` is intentionally not the persisted schema. It contains only what aggregation needs:

- `Session`: provider, internal IDs for parent/deduplication, child flag, times, activity basis, project basename, prompt metrics, calls, model activity, token fields, and tool-call count;
- `PromptMetric`: internal event ID, time, word count, character count, and whether text existed;
- `CallMetric`: internal response/call ID, model label, token fields, and internal tool IDs;
- `TokenUsage`: availability, exactness, source label, and input/output/cache-read/cache-write/reasoning counts;
- `Warning`: stable code, count, and content-free message.

Raw prompt or response text has no field in this model. Source paths remain in `provider.Discovery` and `ProviderResult.SourceFiles` only long enough to read, audit, and count them. Session, prompt, call, and tool IDs are used in memory and never copied into `analytics.Report`.

## Persisted report contract

The JSON artifact is `analytics.Report`, currently schema version `1`.

| Field | Meaning |
| --- | --- |
| `schema_version`, `product_name`, `generated_at` | Format identity and generation time |
| `coverage` | Earliest start, latest end, local timezone, honest display label, and `calendar_framing` presentation hint |
| `totals` | Root sessions, prompts, unique projects, tool calls, active days, child sessions, primary source-file count, changed-source count |
| `providers` | Per-harness status, verification scope, metrics, span, time basis, token ledger, limitations, warnings, and primary file count |
| `rhythm` | Activity by local date, 24 local hours, Monday-first weekdays, favorite hour, late-night percentage, streak, busiest day/month |
| `prompt_style` | Availability, textual-prompt sample count, median/average words, average characters, and deterministic label |
| `models` | Provider-qualified, provider-native model-event counts (the schema field remains `turns`) |
| `projects` | Project basenames ranked by root-session count |
| `longest_session` | Availability, provider, and rounded duration in minutes |
| `privacy.source_audit` | Verification result, before-snapshot file count, manifest digests, changed-file count, and changed-directory count |
| `methodology` | Human-readable interpretation rules |
| `warnings` | Provider-qualified aggregate warning codes, counts, and fixed messages |

`totals.source_files` counts primary files returned by adapters. `privacy.source_audit.files` can be larger because the audit also includes present SQLite WAL, SHM, or journal sidecars. `totals.source_files_changed` counts changed files only; directory-membership changes are a separate audit field.

### Metric rules

- Root sessions alone contribute prompts, projects, calls, models, tokens, rhythm, coverage, and longest-session statistics. Children are counted separately.
- Activity is placed on each adapter's documented activity time and converted to the process's local timezone.
- Weekday bins are Monday through Sunday. Favorite-hour ties select the earliest hour. Busiest-day and busiest-month ties select the earliest chronological entry.
- Late night is 00:00–04:59 local time.
- A streak is a run of consecutive local calendar dates with activity.
- Prompt words are runs separated by Unicode whitespace; characters are Unicode code points. Prompt-style aggregates include textual prompts only.
- Projects persist only the final cleaned path component.
- Token ledgers remain provider-scoped and cache semantics are not normalized into a global total. A numeric zero can represent recorded zero or an absent/null category in source shapes that do not distinguish the two; the ledger-level availability and exactness flags do not provide per-category presence.
- `coverage.calendar_framing` and a concise year label are used only when a same-year span reaches the first seven and last seven days of that year. This never claims account-wide completeness; local histories may be missing.

## Output and serving

The destination is `<user-cache>/skuggsja/rewind.json`. Before any reader runs, generation rejects a destination inside a discovered source root or aliased to a discovered source file; `clean` performs the same check against configured source locations before deleting. Checks cover cleaned paths, resolved symlinks, and existing file identity, including hard links.

The writer creates and syncs a temporary file beside the destination, then renames it into place. Rename-over-existing provides atomic replacement on platforms that support it; on Windows the prior artifact is removed before rename, so a failed replacement can leave no artifact. On Unix-like systems, the product directory is set to mode `0700` and the temporary/final artifact to mode `0600`. The artifact writer does not currently establish a corresponding protected Windows DACL; this differs from Windows SQLite temporary copies, which fail closed unless their restricted DACL validates.

The server exposes:

- `/` plus `/styles.css` and `/app.js` from Go-embedded files;
- `/api/rewind`, a no-store serialization of the in-memory aggregate;
- `/healthz`, a plain local health response.

It binds IPv4 loopback only. Requests are accepted only when the normalized `Host` is exactly `127.0.0.1`, `localhost`, or `::1`; other hosts receive HTTP 421 to reduce DNS-rebinding exposure. Assets contain no external origins, and the JavaScript performs one same-origin fetch to `/api/rewind`. Responses include a Content Security Policy whose default, connection, script, style, and font sources are self (images additionally allow `data:`), `Referrer-Policy: no-referrer`, MIME-sniffing protection, frame denial, and cross-origin opener isolation.

## Failure behavior

| Condition | Behavior |
| --- | --- |
| Source path missing | Provider status `not found`; run continues |
| Discovery cannot inspect a location | Provider status `unavailable` plus `discovery_failed`; run continues |
| Malformed or oversized JSONL record | Record skipped, aggregate warning emitted |
| Unknown history mode | Ambiguous prompts skipped, warning emitted; other supported metrics continue |
| Artifact overlaps a source root or file | Generation fails before readers run; `clean` refuses deletion |
| SQLite cannot be copied consistently | Provider unavailable; original database is never opened by SQLite |
| Required SQLite schema unknown | Provider status `unsupported schema`; no guessed queries |
| Optional SQLite metric unavailable | Supported session metrics remain, warning emitted |
| Source audit fails | Audit remains unverified and a system warning is added |
| Artifact cannot be written | Generation fails; no server starts |
| Requested loopback port is occupied | An ephemeral loopback port is selected |

This degradation policy favors visible omission over fabricated comparability.
