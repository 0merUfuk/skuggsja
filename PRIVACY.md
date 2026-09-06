# Privacy

Skuggsja is designed to turn sensitive local histories into a smaller, content-free report without sending those histories elsewhere. “Local-first” does not mean “contains no personal information”: project basenames, model labels, dates, working rhythms, and usage counts can still be sensitive.

## Privacy boundary

The persisted boundary is `analytics.Report`. Raw prompt and response text, absolute source paths, and source identifiers are not fields in that type.

| Data | Read or derived | Held temporarily | Persisted in `rewind.json` |
| --- | --- | --- | --- |
| Raw prompt text | Yes, where a provider exposes it | Yes, while one record is decoded and counted | No |
| Raw assistant text | May be decoded as part of a source record even when the record is then ignored; structured fields such as tool blocks can be inspected | May exist while its source record is decoded | No |
| Entire SQLite source | Copied before querying | Yes, in a private OS temporary directory | No |
| Absolute source/project paths | Yes, for discovery, reading, auditing, and basename derivation | Yes | No |
| Session, prompt, response, call, and tool IDs | Yes, for relationships and deduplication | Yes | No |
| Prompt word/character counts | Derived in memory | Yes | Yes, as aggregates only |
| Project basename | Derived from the final directory component | Yes | Yes |
| Model label | Read, control-character filtered, and length-limited | Yes | Yes |
| Dates, durations, rhythm, counts, token fields | Derived from source metadata | Yes | Yes |
| Provider status, limitations, warning codes/messages | Derived from parser outcomes | Yes | Yes |
| Source access and observation status | Fixed read-only access contract and optional activity-observation outcome | Yes | Yes |
| Source-audit manifests | SHA-256 over paths, configured/root presence states, content digests, sizes, and directory listings | Yes | Digest and aggregate comparison only |

Prompt text is reduced with Unicode-aware character counting and Unicode-whitespace word splitting. Once a `PromptMetric` is created, the normalized model retains only counts, an internal event ID, a timestamp, and a text-availability flag. The internal ID is later dropped.

Project names deserve special attention: Skuggsja strips the parent path and persists the final directory name. That avoids an absolute-path disclosure but can still reveal a client, employer, codename, or repository name.

## Runtime network behavior

After the binary and its dependencies are installed, Skuggsja's runtime has no outbound network client, telemetry, update check, remote API, CDN, remote font, or remote script. It starts an inbound HTTP listener on IPv4 loopback (`127.0.0.1`) only and serves embedded assets plus the aggregate report.

Installation and development are outside that guarantee:

- `go run`, `go install`, `go build`, and `go mod download` may contact Go module, source, checksum, or toolchain servers when dependencies are not already cached.
- Skuggsja normally asks the operating system to open the local URL in your default browser. Browser networking, extensions, profile sync, and other browser behavior are outside Skuggsja's process. Use `--no-open` to avoid that launch.

The embedded page has no external origins and its only fetch targets the same-origin `/api/rewind` endpoint. The server applies a strict Content Security Policy—connections, scripts, styles, and fonts are restricted to self—and a no-referrer policy.

## Source-file handling

Skuggsja does not write to source paths. Source readers and the source hasher use read-only (`O_RDONLY`) handles, and SQLite never receives an original database path. File mutations are confined to generated output and private copy code, with both destinations checked against every configured source before creation. SQLite parent directories are protected even when a configured database is absent.

This guarantee applies with agents running, with source observation disabled, and when observation is unavailable. It is an architectural source-access contract, not a claim that the production process drops operating-system permissions or runs inside an OS sandbox. The release verification harness can additionally deny writes at the OS boundary to test the contract. Reading may still cause ordinary filesystem metadata effects such as access-time updates; Skuggsja does not issue source writes or metadata mutations.

### JSONL and compressed rollouts

JSONL sources are opened with read-only file handles. Records are read incrementally with a 64 MiB per-record ceiling; an oversized record is discarded with a warning. Codex zstd rollouts are streamed through a bounded decoder and then through the same record boundary.

Parsers may decode raw content to distinguish human prompts from injected context, summaries, tool results, or assistant records. Content is not copied into normalized sessions or the report.

### SQLite

SQLite itself never receives a source database path. For Hermes, Cursor, and Codex's supplemental state/catalog indexes, Skuggsja:

1. requires the database and present WAL or journal to be regular, non-symlink files;
2. hashes that source set;
3. copies it with raw read-only handles into a `skuggsja-sqlite-*` directory inside the generation's guarded `skuggsja-run-*` workspace; the workspace is checked for source overlap before even its directory is created;
4. verifies file identity, size, and SHA-256 before, during, and after the copy, retrying up to five times with bounded backoff if the source set changed;
5. opens only the copy with a one-connection pool so SQLite can recover a copied hot journal/WAL, runs `PRAGMA quick_check(1)`, closes that connection, and reopens the copy read-only with query-only protections for provider queries;
6. closes SQLite and removes the temporary directory on normal completion and handled error paths.

On Unix-like systems the temporary directory is set to and verified as mode `0700`, and copied files as mode `0600`. On Windows it is atomically created with a protected, inheritable two-entry DACL granting access only to the current user and LocalSystem; the code validates protection, the two expected principals, inheritance, and required access, and fails closed on a validation error. Windows amd64/arm64 CI exercises the private-directory DACL and synthetic SQLite-copy behavior; real Windows harness stores remain unverified. The SHM file is not copied because SQLite reconstructs coordination state beside the private copy; WAL and rollback journal files are copied when present.

The workspace parent defaults to the OS temporary directory. A misplaced temporary-directory setting cannot authorize creation inside a source root or protected SQLite directory: generation checks the prospective workspace first, and `sqlitecopy.Open` independently rejects a copy parent inside its own source directory. These guards apply even when source observation is disabled.

An abrupt process termination, machine crash, or `SIGKILL` can prevent deferred cleanup. In that case a raw database copy may remain inside a `skuggsja-run-*` workspace in the OS temporary directory; direct uses of the SQLite-copy helper can leave a `skuggsja-sqlite-*` directory. Skuggsja does not currently sweep stale directories on startup. Inspect and remove stale directories using operating-system tools appropriate to your account.

## Optional source activity observation

Before/after observation is enabled by default. It captures:

- SHA-256 and byte size for every parsed file;
- SHA-256 and byte size for every present audit-only supplemental file;
- present `-wal`, `-shm`, and `-journal` files for SQLite sources;
- explicit missing-state entries for configured optional paths and absent discovery roots;
- sorted entry names and file/directory/symlink kinds recursively below existing discovery roots, plus the nearest existing directories for configured paths.

After the before capture, discovery runs again. If roots or file sets differ, Skuggsja restarts capture and retries up to three times; on success, the reader receives the exact discovery set paired with the retained snapshot. Persistent discovery churn permits best-effort parsing of the latest set and makes observation unavailable. A final capture runs after all readers only for a stable set. The report retains the before-snapshot existing-file count, before/after manifest digests, changed-file-state count, changed-directory/root count, and the legacy `verified` comparison flag. Configured missing paths participate in the digest but not the existing-file count; absolute paths and directory entries are not serialized. `ProtectedDirectories` is solely a write-guard input and never broadens the observed roots or file set.

`privacy.source_access` is always `read-only`. Independently, `privacy.source_observation` is `observed`, `disabled`, or `unavailable`. The terminal and UI display source access as the guarantee and observed activity as neutral information: “2 files changed during the run by another process; skuggsja does not write to source paths.” Directory-listing changes are counted separately, never added to the file count. An unavailable or disabled observation does not create a system provider, warning, or generation failure. Existing provider discovery and parsing warnings remain scoped to their actual cause.

`privacy.source_audit.verified` means the captured bytes, configured-file/root presence, and directory membership matched at those two points. It does not establish Skuggsja's read-only guarantee; the terminal and UI do not use it as an integrity verdict. Comparison does not cover permissions, ownership, access times, or every filesystem metadata field. It cannot identify which other process wrote a change, or detect a change reverted between snapshots.

Manifest digests can act as stable fingerprints of an unchanged source set across reports. Treat them as sensitive aggregate metadata, not as anonymity.

`--no-source-audit` skips snapshots and discovery stabilization and parses the first discovery best-effort. The report records observation as `disabled`; read-only access remains guaranteed.

### Release equality check

The opt-in real-data test separately tries up to eight direct generation windows, without waiting for an idle preflight, and stops at the first complete matching before/after comparison for its declared release scope. Each attempt captures fresh manifests and generates its own aggregate. A configured private evidence directory retains those files under distinct attempt names, including comparisons that detected activity. This check asks whether any process changed those sources during a completed window; it is not a runtime success condition.

When the verification agent is itself hosted in Codex, the test can explicitly snapshot the configured Codex store's inventory and hashes and exclude that store only from the outer release equality comparison. The scope includes shared prompt history, indexes, and SQLite files as well as rollouts because the verification agent can update all of them. No other source is excluded. Generation still reads every original Codex source and includes it in ordinary runtime observation. The release record must name this exclusion and must not claim that the original Codex store stayed unchanged. Exact evidence belongs in [VERIFICATION.md](VERIFICATION.md), separate from the privacy-safe runtime artifact.

## Persisted artifact

The default artifact is the OS user-cache path ending in `skuggsja/rewind.json`; an explicit `SKUGGSJA_OUTPUT_DIRECTORY` selects another absolute directory under the same source-separation protections. Its contents include:

- provider names, status, verification wording, limitations, and aggregate warnings;
- global coverage timestamps/timezone plus each provider's content-free coverage status, confidence, earliest evidence/detail timestamps, and missing-detail counts;
- counts by provider, day, hour, weekday, model, and project basename; tool-call and model-event counts remain provider-scoped because their native units differ;
- prompt-length aggregates;
- provider-scoped source-recorded token ledgers where available, including their ledger-level exactness flag;
- the read-only source-access contract, optional observation status, source-audit digests, and aggregate activity counts;
- fixed methodology text.

Schema 3 removes the global `totals.tool_calls` field. Provider counts remain paired with `tool_calls_available`; unavailable counts are never displayed as measured zero. Model ranking positions and meter scales are local to each harness, with no cross-provider winner.

Before reading or writing, Skuggsja rejects source-root/artifact-directory overlap and any source file equal to the artifact or below its directory, including cleaned-path, resolved-symlink, case, and existing hard-link aliases. SQLite parent directories are separately protected from artifact and temporary-copy creation; protecting them does not make their other files usage or audit inputs. A standalone metadata source file may safely live in an ancestor directory. Declared paths remain guarded after discovery errors, and writing/cleaning rejects symlinks in output-directory components. The writer creates or changes the containing directory to mode `0700`, writes a mode-`0600` temporary file, syncs it, and renames it into place on Unix-like systems. On Windows it applies and validates a protected, inheritable directory DACL limited to the current user and LocalSystem before creating the artifact; synthetic Windows amd64/arm64 CI verifies this implementation and the installed CLI.

`--json` still writes the normal artifact before emitting the same aggregate to standard output. Shell redirection, terminal scrollback, pipelines, logs, and any duplicate file created from stdout are controlled by your shell and downstream tools, not by Skuggsja.

## Local UI exposure

The Rewind server binds only to `127.0.0.1`, but it has no application authentication or TLS. While it is running, other processes able to reach your loopback interface can request the aggregate. The API never serves source files or raw histories, but the aggregate itself may be personal.

Recommended practice:

- stop the server with <kbd>Ctrl</kbd>+<kbd>C</kbd> when finished;
- use `--once` if you need only the artifact;
- use `--no-open` when testing or when browser launch is undesirable;
- avoid running on a hostile shared account;
- protect or remove exported/copied JSON yourself.

## Deleting generated data

```sh
skuggsja clean
```

This removes only `rewind.json` from the selected default or explicit output directory, then removes that directory only if empty. It deliberately refuses to act on an unexpected filename or when configured source locations overlap or alias the artifact. It does not delete:

- source histories;
- stdout redirections or copies;
- a stale SQLite temporary directory left by an ungraceful termination.

All aggregates are regenerable from the histories still present at the next run.

## Testing the boundary

Repository tests use synthetic fixtures and temporary databases. They verify that serialized reports exclude absolute source paths and internal session/prompt/call/tool identifiers, that live WAL-backed SQLite data can be read through a private copy, that configured-path changes and discovery-set changes are detected, that nested Claude children and Codex pagination/index coverage behave deterministically, that provider coverage serializes without paths/content, and that browser assets contain no external origins. Source-access checks cover the production write surface, temporary-workspace/source overlap including absent SQLite paths, and separation between runtime observation and read-only guarantees. Terminal and JavaScript tests check neutral activity in full, empty, unavailable, disabled, and retry states.

Browser verification serves a retained aggregate through `scripts/serve-report` and uses `scripts/verify-browser.cjs` with an isolated Chrome Headless profile. It records screenshots, rendered-value assertions, and intercepted requests in a private evidence directory; it does not reread source histories. Screenshots and retained aggregates can contain sensitive project basenames and usage patterns, and belong outside the repository. The temporary browser profile is removed when the verifier exits normally. Verification results are reported separately from the availability of these tools.

Tests demonstrate the coded invariants for covered cases; they are not a formal proof. Please report a privacy or security issue through the process in [SECURITY.md](SECURITY.md) without attaching real history files.
