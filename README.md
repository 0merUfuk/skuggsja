# skuggsja

**A local-first Rewind for your AI coding agents.**

`skuggsja` (from Old Norse *skuggsjá*, “mirror”) reads the histories already stored by Claude Code, Codex, Hermes Agent, and Cursor, reduces them to privacy-safe aggregates, and opens an editorial report on loopback. It is a record of what survives on one machine, not an account-wide usage statement.

> Verification scope: the code has path handling for macOS, Linux, and Windows, but real-provider-data verification is currently macOS-only. Cursor support is schema-verified with synthetic data and is not real-data verified. Treat other operating systems and new upstream schemas as unverified until tested.

## Quick start

From a checkout with Go 1.25.6 or newer available:

```sh
go run ./cmd/skuggsja
```

That one command generates the aggregate report, prints a terminal summary, starts an HTTP server bound to `127.0.0.1`, and asks the operating system to open the local Rewind in the default browser. If browser launch is unavailable, open the printed loopback URL yourself. Stop the server with <kbd>Ctrl</kbd>+<kbd>C</kbd>.

`go run`, `go install`, and the first build may contact Go module or toolchain servers. The zero-outbound guarantee applies to the installed/built program at runtime: Skuggsja has no telemetry, update check, remote API call, CDN, remote font, or remote browser asset. Use `--no-open` if you also do not want Skuggsja to launch your browser.

To install from a checkout and then use the single command `skuggsja` (with `GOBIN`, or the Go bin directory, on `PATH`):

```sh
go install ./cmd/skuggsja
skuggsja
```

## What it reads

| Harness | Default source | Reported capabilities | Verification |
| --- | --- | --- | --- |
| Claude Code | `projects`, `history.jsonl`, and `stats-cache.json` below `CLAUDE_CONFIG_DIR` (otherwise `~/.claude`), plus the home-level `.claude.json`; macOS Desktop local-agent transcripts and Code-session indexes are audited as exclusion/coverage evidence only | Root and nested child sessions, human prompts, models, tools, source-recorded message token ledger, and explicit local-coverage evidence | Real data on macOS |
| Codex | rollout roots plus `history.jsonl`, `session_index.jsonl`, import/state/catalog indexes below `CODEX_HOME` (otherwise `~/.codex`) | Root/child sessions, human prompts, models, tools, final cumulative token fields, validated pagination, and explicit local-coverage evidence | Real data on macOS; compressed-file handling is fixture-tested |
| Hermes Agent | `state.db` below `HERMES_HOME`; otherwise `~/.hermes` on Unix-like systems or the local app-data Hermes directory on Windows | Sessions, parent relationships, prompts, models, tool calls, source-recorded database token ledger | Real data on macOS |
| Cursor | OS-specific `Cursor/User/globalStorage/state.vscdb` | Composer sessions/subagents, human prompts, models, project basename; no token estimates | Schema-verified synthetic; not real-data verified |

When every supported input for a harness is absent, it is reported as `not found`; a missing detailed root accompanied by supplemental index evidence can instead be supported with warnings and marked incomplete. Parser and schema problems appear as provider-scoped warnings. See [Architecture](ARCHITECTURE.md) for the adapter contract and [Privacy](PRIVACY.md) for the data lifecycle.

## Commands and flags

```text
skuggsja [flags]
skuggsja clean
skuggsja version
skuggsja completion [bash|fish|powershell|zsh]
```

| Flag | Behavior |
| --- | --- |
| `--no-open` | Serve the report without launching the default browser. |
| `--once` | Generate and persist the aggregate, print the summary, then exit without serving. |
| `--json` | Generate and persist the aggregate, write it to standard output, then exit. |
| `--no-source-audit` | Skip source snapshots and capture/rediscovery stabilization. This does not change the read-only parser design, but the first discovery is parsed best-effort and the run cannot claim a stable or integrity-verified source set. |
| `--port N` | Prefer loopback port `N`; default `4321`. Use `0` for an ephemeral port. If a requested port is busy, Skuggsja falls back to an ephemeral loopback port. |

Examples:

```sh
# Generate the artifact without starting a server.
skuggsja --once

# Inspect the same aggregate JSON written to stdout.
skuggsja --json | jq '.totals'

# Serve on an automatically selected loopback port without opening a browser.
skuggsja --no-open --port 0

# Remove the default regenerable artifact.
skuggsja clean
```

`skuggsja clean` removes only the default `rewind.json` artifact, after confirming configured source locations do not overlap or alias it, and removes its product directory only when empty. It does not remove a copy created by redirecting `--json`.

## Source-path overrides

These environment variables replace individual discovery locations:

| Variable | Meaning |
| --- | --- |
| `SKUGGSJA_CLAUDE_PROJECTS` | Claude Code projects directory |
| `SKUGGSJA_CLAUDE_HISTORY` | Claude Code prompt-history JSONL |
| `SKUGGSJA_CLAUDE_STATS` | Claude Code aggregate statistics cache |
| `SKUGGSJA_CLAUDE_GLOBAL_STATE` | Claude global-state JSON; recognized backups are discovered beside it |
| `SKUGGSJA_CLAUDE_DESKTOP_SESSIONS` | Claude Desktop local-agent root, audited as exclusion evidence on macOS |
| `SKUGGSJA_CLAUDE_CODE_SESSIONS` | Claude Desktop Code-session index root on macOS |
| `SKUGGSJA_CODEX_SESSIONS` | Codex active sessions directory |
| `SKUGGSJA_CODEX_ARCHIVED` | Codex archived sessions directory |
| `SKUGGSJA_CODEX_HISTORY` | Codex prompt-history JSONL |
| `SKUGGSJA_CODEX_SESSION_INDEX` | Codex session index JSONL |
| `SKUGGSJA_CODEX_EXTERNAL_IMPORTS` | Codex external-session import index |
| `SKUGGSJA_CODEX_STATE_DATABASE` | Codex thread-state SQLite database |
| `SKUGGSJA_CODEX_CATALOG_DATABASE` | Codex app catalog SQLite database |
| `SKUGGSJA_CODEX_THREAD_HISTORY_DATABASE` | Codex paginated-history database; audited as unparsed coverage evidence |
| `SKUGGSJA_HERMES_DATABASE` | Hermes `state.db` path |
| `SKUGGSJA_CURSOR_DATABASE` | Cursor `state.vscdb` path |
| `CLAUDE_CONFIG_DIR` | Base Claude configuration directory for `projects`, prompt history, and statistics; home-level global state and macOS Desktop roots remain separate unless specifically overridden |
| `CODEX_HOME` | Base Codex directory for rollout roots and all supplemental Codex indexes unless their specific overrides are set |
| `HERMES_HOME` | Base Hermes directory; `state.db` is appended unless the specific Skuggsja override is set |

`CLAUDE_CONFIG_DIR`, `CODEX_HOME`, and `HERMES_HOME` change their respective default roots before the more specific `SKUGGSJA_*` overrides are applied. On Linux, `XDG_CONFIG_HOME` affects Cursor's default path; on Windows, `APPDATA` and `LOCALAPPDATA` are used where available.

Overrides are useful for tests and nonstandard installs. Point them only at histories you intend the current process to read. A source root and the artifact directory may not contain one another; a source file may not be the artifact or lie within its directory. Generation and `clean` fail closed on cleaned-path, symlink-component, resolved-symlink, case-insensitive macOS/Windows, and existing hard-link aliases. Declared paths are guarded even when provider discovery fails.

## What the numbers mean

| Metric | Definition |
| --- | --- |
| Sessions | Top-level physical or history-index sessions with trustworthy identity. A timestamp-unavailable physical session can be counted but is excluded from span/rhythm; unanchored copied files are not sessions. Child/subagent histories are excluded. |
| Child sessions | Histories identified as children or subagents. They are counted separately and excluded from session-derived totals and rhythm metrics. |
| Prompts | Unique human prompt events recognized by each adapter. Text is reduced immediately to word and character counts; supported attachment-only events may count as prompts but not as prompt-style samples. |
| Projects | Distinct final directory names associated with top-level sessions. Absolute project paths are not persisted. |
| Tool calls | Deduplicated call identifiers where the source exposes them; otherwise a mapped source count. Totals exclude harnesses such as Cursor where this metric is unavailable. |
| Active days | Local calendar dates on which at least one countable top-level session has a trustworthy provider-defined activity time. |
| Model events | Provider-native model/API activity: for example, assistant responses, turn contexts, API-call counts, or eligible Cursor prompt bubbles carrying a model label. These events are not a comparable cross-provider unit. |
| Tokens | A provider-scoped ledger copied from recognized source usage records when available. Skuggsja does not estimate an unavailable provider ledger or combine unlike cache semantics. A zero category can mean recorded zero or an absent/null field in formats that do not distinguish those cases. |
| Longest session | Largest non-negative `end - start` duration among countable top-level sessions with trustworthy times. |
| Late night | Percentage of time-binned top-level sessions whose trustworthy activity hour is 00:00 through 04:59 in the machine's local timezone. |

Prompt words are whitespace-delimited Unicode fields; characters are Unicode code points. The prompt-style label uses median words: `terse operator` at 15 or fewer, `specification writer` above 60, and `precision prompter` between them.

The global displayed span runs from the earliest trustworthy top-level session start to the latest top-level session end found locally. Every provider also reports a separate coverage assessment: earliest local evidence, earliest surviving detailed record, history-only sessions, known references without detail, status, confidence, and an explanatory note. A provider can therefore be **known incomplete** even when its recovered counts are large or small. `calendar_framing` is only a presentation hint; deleted, cloud-only, moved, alternate-home, or unsupported histories cannot be inferred from usage totals.

## Privacy in one minute

- JSONL histories are opened read-only. SQLite histories and present WAL or journal files must be regular, non-symlink files and are copied into a private temporary directory. SQLite may recover and check only that copy, then reopens it read-only with query-only protections for provider queries. The copy directory is mode `0700` with mode-`0600` files on Unix-like systems; Windows uses a validated protected DACL for the current user and LocalSystem.
- Raw prompt text is used transiently to calculate counts and is absent from the normalized model and persisted report.
- Source paths, session IDs, prompt IDs, call IDs, and tool IDs may exist temporarily for discovery or deduplication but are excluded from the JSON artifact.
- The persisted artifact contains aggregate timestamps, counts, model labels, project basenames, provider warnings, and source-audit digests. It is privacy-reduced, not anonymous.
- The default artifact is written through a synced same-directory temporary file and rename-over-existing. Unix-like systems receive a mode-`0700` artifact directory and mode-`0600` artifact. Windows applies and validates a protected directory DACL limited to the current user and LocalSystem; this path cross-compiles but has not been runtime-tested on Windows.
- A before/after SHA-256 audit checks parsed and audit-only contents, configured/root presence, and directory membership unless disabled. Capture/rediscovery stabilization binds the snapshot to the reader input set. The audit does not compare metadata such as access times or prove which process caused a concurrent change.

Read [PRIVACY.md](PRIVACY.md) before using real histories and [SECURITY.md](SECURITY.md) for the threat model.

## Limitations

- Upstream history formats are private implementation details and can change without notice. Unknown SQLite schemas are skipped instead of guessed.
- Claude and Codex select candidate histories by filename and parse recognized record shapes rather than validating a version marker. A matching, syntactically valid but unrelated JSONL file can currently appear supported with zero recognized events.
- Real-data verification is currently limited to macOS; Cursor currently has synthetic schema verification only.
- Codex’s paginated `thread_history_1.sqlite` is included in source auditing but is not parsed into usage; its presence produces an explicit coverage warning. Skuggsja never migrates or repairs it.
- Claude project `sessions-index.json` entries contribute only known session references and dates; indexed summaries and message counts never become usage. Runtime debug, session-env, and telemetry residues are not usage inputs.
- One configured `CLAUDE_CONFIG_DIR` and `CODEX_HOME` is traversed per run. Arbitrary alternate historical homes are not guessed automatically; use the documented overrides or separate runs for known alternate roots.
- Claude nested `subagents/agent-*.jsonl` transcripts are discovered and counted as child sessions; their events are excluded wholesale from owner/root usage totals.
- Claude Desktop Cowork/local-agent transcripts are audited and explicitly reported as excluded because they are a different product surface despite embedding Claude Code-shaped history.
- Supplemental history, state, and catalog indexes establish evidence of missing detail. They contribute only fields they actually retain—some Claude prompt-history rows retain a project basename—and do not reconstruct lost model, tool, token, response, or full-detail records.
- Codex model-event counts cannot be joined exactly to its session-level token totals.
- Hermes combines sessions created through all interfaces that share `state.db`.
- Cursor reads only the canonical global composer store, does not merge derived search or legacy workspace stores, and reports no token totals for the supported schema.
- A hard crash or `SIGKILL` can prevent cleanup of a private SQLite copy in the OS temporary directory.
- The loopback report has no application authentication or encryption. Other local processes may be able to read it while the server runs.

## Development

```sh
go test ./...
go vet ./...
go build -trimpath -o ./skuggsja ./cmd/skuggsja
```

Tests use synthetic fixtures and temporary databases. Never add real agent histories to the repository. See [CONTRIBUTING.md](CONTRIBUTING.md) for provider and privacy requirements.

## Documentation

- [Architecture and data model](ARCHITECTURE.md)
- [Privacy model](PRIVACY.md)
- [Security policy and threat model](SECURITY.md)
- [Contributing](CONTRIBUTING.md)
- [Changelog](CHANGELOG.md)

## License

Skuggsja is available under the [MIT License](LICENSE).
