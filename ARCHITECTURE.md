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
        │                optional before observation
        ▼
provider readers ─────────► normalized sessions (memory only)
        │                            │
        │                            ▼
        │                 optional after observation
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

The ordering matters. Every reader must disclose parsed files, audit-only files, configured optional files, roots, and protected source directories during `Discover` before `Read` opens them. Source handles are read-only (`O_RDONLY`); output and private SQLite-copy destinations must be separate from the declared sources. This architectural guarantee applies on every run and does not depend on snapshot equality or production OS sandboxing.

With optional source activity observation enabled, Skuggsja captures the audit input set and discovers again. A match pairs the retained before snapshot with the exact set sent to readers; a mismatch retries boundedly. Persistent churn permits best-effort parsing of the latest discovery and records observation as unavailable, without creating a system warning or source-integrity failure. `Discovery.ProtectedDirectories` participates only in output and temporary-workspace write guards; it never adds those directories' contents to discovery, usage, or source observation.

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
| `internal/app` | Generation orchestration, source/output and private-workspace guards, temp-and-rename output, loopback server, security headers |
| `web` | Embedded, dependency-free Rewind UI |

Internal packages deliberately prevent downstream consumers from treating provider source formats as public APIs.

## Dependencies

The runtime dependency surface is deliberately small:

- `github.com/spf13/cobra` provides CLI parsing and help;
- `modernc.org/sqlite` provides a pure-Go SQLite driver for private database copies;
- `github.com/klauspost/compress/zstd` streams compressed Codex rollouts with a decoder-memory bound;
- `golang.org/x/sys/windows` creates and validates private SQLite-copy ACLs on Windows.

The web UI has no package-manager dependencies, remote fonts, remote scripts, or runtime assets outside the binary. Standard-library packages provide JSON parsing, hashing, filesystems, embedded assets, and the loopback HTTP server.

`scripts/verify-runtime-offline.sh` builds disposable inputs that exercise all four provider adapters, then runs generation and serving with remote sockets denied by macOS Seatbelt and observed/blocked by a verification-only DYLD guard. Its calibrated Go probe must produce an external-attempt record; Skuggsja must produce none while every embedded route is retrieved over loopback. The helper binaries and interposer are development evidence only and are not part of release archives.

`scripts/verify-live-source-protection.sh` additionally applies a verification-only OS write-denial policy outside its private work area and calibrates that policy using disposable controls. These release tools test the architectural contract; the production binary does not drop its OS permissions or install this sandbox.

## Generation lifecycle

1. `platform.DefaultPaths` resolves candidate locations without network access.
2. CLI environment overrides replace individual locations.
3. Each provider performs discovery. A missing source yields no parsed/audit-only file, while the declared missing root or configured optional path remains part of observation. SQLite parent directories are declared as protected directories before fallible inspection, even for absent databases.
4. The source/output guard checks every declared root, protected directory, parsed file, audit-only file, and configured optional path even after a discovery error. It rejects shared directories, containment in either direction, symlink/case aliases, existing hard-link aliases, and intermediate-component symlink aliases.
5. Generation chooses a random `skuggsja-run-*` workspace under the selected temporary parent and checks its prospective path against the same protected inputs before creating it. `sqlitecopy.WithTempDir` passes that destination through context without changing process environment. Rediscovery rechecks workspace separation if the input set changes.
6. If enabled and at least one parsed, audit-only, configured, or root path was supplied, observation hashes every present declared file and relevant SQLite sidecar, records absence for configured optional paths, recursively inventories existing discovery roots, and inventories nearest existing containing directories. Protected directories do not expand this audit set. Discovery then runs again. A changed set restarts capture; repeated churn records observation as unavailable.
7. Readers parse the stable discovery set paired with the retained before snapshot, or the latest discovery after bounded persistent churn, into `model.ProviderResult`. Provider failures become scoped statuses and aggregate warnings where possible; optional observation is independent of those outcomes.
8. SQLite readers call `sqlitecopy.Open`, which independently rejects a temporary-copy parent inside its own source directory. Only private copies are passed to SQLite for recovery, integrity checking, and provider queries; original files are opened with raw read-only handles.
9. A second observation snapshot is compared with the first when the before phase succeeded. Changed files and directory listings are neutral activity information. Capture failure, incomplete observation, and disabled observation do not add a system provider or warning and do not fail generation.
10. `analytics.Build` creates the serializable `Report`, records read-only source access separately from observation status, and drops internal identifiers and source paths.
11. `WriteReport` writes a temporary file beside the destination, syncs it, and renames it over the previous artifact. Private SQLite copies and their generation workspace are removed on normal completion and handled errors.
12. Unless `--once` or `--json` was selected, `Serve` binds `127.0.0.1`, serves the in-memory report and embedded assets, and runs until cancellation.

Provider discovery and reads currently run in deterministic provider order. File-oriented readers and the source hasher use at most six workers. Each copied SQLite database is opened with one connection and `PRAGMA query_only = ON` after `PRAGMA quick_check` succeeds.

## Provider adapters

### Claude Code

Claude discovery selects direct `<projects-root>/<encoded-project>/<session>.jsonl` histories and nested `.../<session>/subagents/agent-*.jsonl` child histories. The nested path/filename establishes a child; matching `agentId`/`sessionId` records anchor its own time. A direct transcript with matching physical records marked `isSidechain:true` is also a child; copied foreign sidechain records cannot reclassify an owner session. Workflow journals, metadata sidecars, debug logs, file-history snapshots, and third-party memory stores are not treated as Claude Code sessions. On macOS, embedded Claude Desktop Cowork/local-agent transcripts are audited and counted in an exclusion warning, but are not added to Claude Code usage.

Configured and canonical homes plus recognized immediate native `.claude-*` siblings feed one logical provider. Identical or partial copies of the same physical session coalesce before session/rhythm counting; unique source IDs and concrete model/usage associations are preserved. Explicit source overrides bypass automatic native discovery.

The adapter also audits and reconciles `history.jsonl`, `stats-cache.json`, the global `.claude.json` plus recognized backups, and Claude Desktop Code-session indexes. These sources can prove that detailed transcripts are missing; they contribute only the fields they actually retain and never fabricate lost model, token, tool, response, or full transcript detail.

The adapter derives:

- physical session identity from the filename/path plus matching records, and start/end from valid record timestamps rather than filesystem mtime;
- project basename from `cwd`;
- human prompts from eligible user-message text or supported attachment blocks, excluding metadata, compact summaries, visible-transcript-only records, and tool-result echoes;
- provider-native model events and the source-recorded token ledger, including recorded thinking tokens, from assistant messages;
- distinct tool calls from assistant `tool_use` block IDs.

Repeated streaming updates for one assistant response are merged by response ID: distinct tool blocks are retained and the last complete cumulative usage snapshot wins. Copied fork history is then deduplicated globally by stable response, prompt, and tool identifiers. Every suppression/conflict class emits an aggregate warning. Child histories remain visible as child-session counts but do not contribute owner/root activity totals.

### Codex

Codex discovery walks active, archived and recovery roots for `rollout-*.jsonl` and `rollout-*.jsonl.zst`. Plain and compressed siblings are one logical rollout candidate; the plain file wins when both exist, while the skipped compressed source remains byte-hashed in the audit. Byte-identical same-ID copies are suppressed before pagination validation, with every physical file retained in the audit set. The adapter additionally audits `history.jsonl`, `session_index.jsonl`, the external-import index, the thread-state SQLite database, and the local app catalog. State-referenced paths outside configured rollout roots are never probed.

The adapter uses a valid first `session_meta` record as rollout identity and physical start, detects children from parent/thread metadata, derives project basename from `cwd`, and counts provider-native model events from turn context. The end is the latest valid record timestamp. Prompt extraction is history-mode-aware: legacy histories use human `user_message` events, while paginated histories use completed `UserMessage` items. Unknown modes skip prompt extraction and emit a warning rather than risk double-counting copied user representations. `response_item` records are used for tool calls, not as a second prompt source.

When a paginated continuation supplies an exact base thread, byte boundary, and ordinal boundary, both physical segments are parsed at that boundary and stitched. Same-ID files without a validated chain are not merged speculatively. Supplemental indexes add history-only sessions or aggregate missing-detail evidence; remote ChatGPT catalog rows and account/workspace host keys are context only and are excluded from local usage totals.

Within each physical segment, token usage is the largest/final cumulative `total_token_usage` snapshot. Validated pagination stitches segments and sums their segment ledgers; a logical session total still cannot be assigned exactly to individual model events.

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

- `Session`: provider, internal IDs for parent/deduplication, child/history-only/unanchored/time-unavailable flags, times, activity basis, project basename, prompt metrics, calls, model activity, token fields, and tool-call count;
- `PromptMetric`: internal event ID, time, word count, character count, and whether text existed;
- `CallMetric`: internal response/call ID, model label, token fields, and internal tool IDs;
- `TokenUsage`: availability, exactness, source label, and input/output/cache-read/cache-write/reasoning counts;
- `Warning`: stable code, count, and content-free message.
- `CoverageAssessment`: per-provider status/confidence, earliest local evidence, earliest detailed record, and aggregate counts of history-only or unmaterialized sessions.

Raw prompt or response text has no field in this model. Source paths remain in `provider.Discovery` and `ProviderResult.SourceFiles` only long enough to read, audit, and count them. Session, prompt, call, and tool IDs are used in memory and never copied into `analytics.Report`.

## Persisted report contract

The JSON artifact is `analytics.Report`, currently schema version `2`.

| Field | Meaning |
| --- | --- |
| `schema_version`, `product_name`, `generated_at` | Format identity and generation time |
| `coverage` | Earliest start, latest end, local timezone, honest display label, and `calendar_framing` presentation hint |
| `totals` | Root sessions, prompts, unique projects, tool calls from providers that expose that metric, active days, child sessions, declared provider-input count, changed-source count |
| `providers` | Per-harness status, verification scope, metrics, span, time basis, token ledger, limitations, warnings, declared input-file count, and explicit coverage assessment |
| `rhythm` | Activity by local date, 24 local hours, Monday-first weekdays, favorite hour, late-night percentage, streak, busiest day/month |
| `prompt_style` | Availability, textual-prompt sample count, median/average words, average characters, and deterministic label |
| `models` | Provider-qualified, provider-native model-event counts (the schema field remains `turns`) |
| `projects` | Project basenames ranked by root-session count |
| `longest_session` | Availability, provider, and rounded duration in minutes |
| `privacy.source_access` | Always `read-only`; the architectural guarantee that Skuggsja does not write to source paths |
| `privacy.source_observation` | Optional runtime activity observation: `observed`, `disabled`, or `unavailable` |
| `privacy.source_audit` | Before-snapshot file count, manifest digests, changed-file count, changed-directory count, and legacy `verified` snapshot-equality flag; independent of source-access guarantees |
| `methodology` | Human-readable interpretation rules |
| `warnings` | Provider-qualified aggregate warning codes, counts, and fixed messages |

`totals.source_files` counts parsed and audit-only files returned by adapters. `privacy.source_audit.files` counts existing files in the before snapshot and can be larger because of present SQLite sidecars; configured-but-absent paths affect the manifest and change detection but not that count. `totals.source_files_changed` counts changed file states; directory/root-membership changes are a separate audit field.

The terminal and UI always show read-only source access. An observed file change is reported neutrally as “2 files changed during the run by another process; skuggsja does not write to source paths.” They do not render `source_audit.verified` as an integrity verdict. Disabled or unavailable observation leaves the access guarantee intact. Snapshot comparison cannot identify the other process or detect a change that was reverted between captures.

### Metric rules

- Root sessions alone contribute prompts, projects, calls, models, tokens, rhythm, and longest-session statistics. Children are counted separately. Unanchored copied transcripts can contribute only globally deduplicated evidence, never session/time/activity counts. History-only sessions contribute only explicitly retained provider fields, including Claude prompt/time/project-basename evidence.
- Global span and rhythm use trustworthy detailed/history timestamps. Per-provider coverage independently reports earlier aggregate/index evidence and missing-detail counts; neither is an account-lifetime completeness claim.
- Activity is placed on each adapter's documented activity time and converted to the process's local timezone.
- Weekday bins are Monday through Sunday. Favorite-hour ties select the earliest hour. Busiest-day and busiest-month ties select the earliest chronological entry.
- Late night is 00:00–04:59 local time.
- A streak is a run of consecutive local calendar dates with activity.
- Prompt words are runs separated by Unicode whitespace; characters are Unicode code points. Prompt-style aggregates include textual prompts only.
- Projects persist only the final cleaned path component.
- Token ledgers remain provider-scoped and cache semantics are not normalized into a global total. A numeric zero can represent recorded zero or an absent/null category in source shapes that do not distinguish the two; the ledger-level availability and exactness flags do not provide per-category presence.
- `coverage.calendar_framing` and a concise year label are used only when a same-year span reaches the first seven and last seven days of that year. This never claims account-wide completeness; local histories may be missing.

## Output and serving

The default destination is `<user-cache>/skuggsja/rewind.json`; `SKUGGSJA_OUTPUT_DIRECTORY` can choose an absolute directory while retaining `rewind.json`. Before any reader runs, generation rejects a source root or protected directory that overlaps the artifact directory and any source file that equals the artifact or lies below its directory; a standalone metadata source file may safely be in an ancestor directory. `clean` performs the corresponding configured-source check before deleting. Checks cover cleaned and symlink-resolved forms, conservative case folding on macOS and Windows, existing ancestor identity, and hard-link identity. The writer and cleaner separately reject symlinks in output-directory components. Discovery-error paths do not bypass the guard. Temporary-copy destinations pass the same source-separation checks before workspace creation.

The writer creates and syncs a temporary file beside the destination, then renames it over the prior artifact. On Unix-like systems, the product directory is set to mode `0700` and the temporary/final artifact to mode `0600`. On Windows, the product directory is created or updated with a protected, inheritable DACL limited to the current user and LocalSystem and validated before the artifact is created. This code cross-compiles but remains runtime-unverified on Windows.

The server exposes:

- `/` plus `/styles.css` and `/app.js` from Go-embedded files;
- `/api/rewind`, a no-store serialization of the in-memory aggregate;
- `/healthz`, a plain local health response.

It binds IPv4 loopback only. Requests are accepted only when the normalized `Host` is exactly `127.0.0.1`, `localhost`, or `::1`; other hosts receive HTTP 421 to reduce DNS-rebinding exposure. Assets contain no external origins, and the JavaScript performs one same-origin fetch to `/api/rewind`. Responses include a Content Security Policy whose default, connection, script, style, and font sources are self (images additionally allow `data:`), `Referrer-Policy: no-referrer`, MIME-sniffing protection, frame denial, and cross-origin opener isolation.

## Failure behavior

| Condition | Behavior |
| --- | --- |
| All supported inputs for a harness are absent | Provider status `not found`; run continues |
| Detailed history absent but supplemental evidence survives | Provider remains usable with warnings and `known incomplete` coverage; only retained fields count |
| Discovery cannot inspect a location | Provider status `unavailable` plus `discovery_failed`; run continues |
| Malformed or oversized JSONL record | Record skipped, aggregate warning emitted |
| Unknown history mode | Ambiguous prompts skipped, warning emitted; other supported metrics continue |
| Artifact overlaps a source root, protected directory, or file | Generation fails before readers run; `clean` refuses deletion |
| Temporary workspace overlaps a source root, protected directory, or file | Generation fails before workspace creation |
| SQLite cannot be copied consistently | Provider unavailable; original database is never opened by SQLite |
| Required SQLite schema unknown | Provider status `unsupported schema`; no guessed queries |
| Optional SQLite metric unavailable | Supported session metrics remain, warning emitted |
| Source activity changes during a completed observation | Neutral file/directory activity counts; read-only guarantee unchanged |
| Source observation fails or is incomplete | `source_observation: unavailable`; no system provider, warning, or generation failure |
| Source observation disabled | `source_observation: disabled`; read-only guarantee unchanged |
| Discovery changes between the before snapshot and confirmation | Capture is retried up to three times; persistent churn records observation as unavailable |
| Artifact cannot be written | Generation fails; no server starts |
| Requested loopback port is occupied | An ephemeral loopback port is selected |

This degradation policy favors visible omission over fabricated comparability.

## Release-only source equality

`TestRealDataFullRunLeavesSourcesUnchanged` is opt-in release verification. It waits for a continuous quiet preflight, runs generation once, then requires matching outer snapshots for the declared live scope. That asks whether any process changed the observed sources during the window; it is separate from production's architectural source-write protection and normal runtime success.

When the verifier is itself a Codex agent, `SKUGGSJA_RELEASE_SNAPSHOT_CODEX=1` explicitly snapshots the configured Codex store's inventory and hashes and excludes that store only from the outer equality comparison. The exclusion covers rollouts and shared history/index/SQLite files because the verification session can update each. No other source is excluded. Generation and its internal runtime observation still use all original sources, including Codex. The release record must retain the exact private exclusion inventory and reasoning in the scope recorded by [VERIFICATION.md](VERIFICATION.md); it must not claim unchanged original Codex sources.

### Unparsed Codex paginated database

`thread_history_1.sqlite` and configured sidecars participate in output protection and source auditing. Its contents do not contribute usage because the paginated database format is not supported. A fixed warning makes that omission explicit. A malformed source remains untouched. All configured sources are declared before discovery can fail, so a provider error cannot remove them from output protection.
