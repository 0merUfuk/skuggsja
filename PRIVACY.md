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
| Source-audit manifests | SHA-256 over paths, content digests, sizes, and directory listings | Yes | Digest and aggregate comparison only |

Prompt text is reduced with Unicode-aware character counting and Unicode-whitespace word splitting. Once a `PromptMetric` is created, the normalized model retains only counts, an internal event ID, a timestamp, and a text-availability flag. The internal ID is later dropped.

Project names deserve special attention: Skuggsja strips the parent path and persists the final directory name. That avoids an absolute-path disclosure but can still reveal a client, employer, codename, or repository name.

## Runtime network behavior

After the binary and its dependencies are installed, Skuggsja's runtime has no outbound network client, telemetry, update check, remote API, CDN, remote font, or remote script. It starts an inbound HTTP listener on IPv4 loopback (`127.0.0.1`) only and serves embedded assets plus the aggregate report.

Installation and development are outside that guarantee:

- `go run`, `go install`, `go build`, and `go mod download` may contact Go module, source, checksum, or toolchain servers when dependencies are not already cached.
- Skuggsja normally asks the operating system to open the local URL in your default browser. Browser networking, extensions, profile sync, and other browser behavior are outside Skuggsja's process. Use `--no-open` to avoid that launch.

The embedded page has no external origins and its only fetch targets the same-origin `/api/rewind` endpoint. The server applies a strict Content Security Policy—connections, scripts, styles, and fonts are restricted to self—and a no-referrer policy.

## Source-file handling

### JSONL and compressed rollouts

JSONL sources are opened with read-only file handles. Records are read incrementally with a 64 MiB per-record ceiling; an oversized record is discarded with a warning. Codex zstd rollouts are streamed through a bounded decoder and then through the same record boundary.

Parsers may decode raw content to distinguish human prompts from injected context, summaries, tool results, or assistant records. Content is not copied into normalized sessions or the report.

### SQLite

SQLite itself never receives a source database path. For Hermes and Cursor, Skuggsja:

1. requires the database and present WAL or journal to be regular, non-symlink files;
2. hashes that source set;
3. copies it with raw read handles into an OS temporary directory named with the `skuggsja-sqlite-` prefix;
4. verifies file identity, size, and SHA-256 before, during, and after the copy, retrying up to five times with bounded backoff if the source set changed;
5. opens only the copy with a one-connection pool so SQLite can recover a copied hot journal/WAL, runs `PRAGMA quick_check(1)`, closes that connection, and reopens the copy read-only with query-only protections for provider queries;
6. closes SQLite and removes the temporary directory on normal completion and handled error paths.

On Unix-like systems the temporary directory is set to and verified as mode `0700`, and copied files as mode `0600`. On Windows it is atomically created with a protected, inheritable two-entry DACL granting access only to the current user and LocalSystem; the code validates protection, the two expected principals, inheritance, and required access, and fails closed on a validation error. The code has a Windows-specific validation test and cross-compiles for Windows, but this behavior has not been exercised in a Windows runtime during current verification. The SHM file is not copied because SQLite reconstructs coordination state beside the private copy; WAL and rollback journal files are copied when present.

An abrupt process termination, machine crash, or `SIGKILL` can prevent deferred cleanup. In that case a raw database copy may remain in the OS temporary directory. Skuggsja does not currently sweep stale directories on startup. Inspect and remove stale `skuggsja-sqlite-*` directories using operating-system tools appropriate to your account.

## Before/after source audit

The source audit is enabled by default. It captures:

- SHA-256 and byte size for every discovered primary file;
- present `-wal`, `-shm`, and `-journal` files for SQLite sources;
- sorted entry names and file/directory/symlink kinds in existing discovery-root and source-containing directories.

The capture runs before and after all readers. The report persists the before-snapshot file count, before/after manifest digests, changed-file count, changed-directory count, and a `verified` flag. Absolute paths and directory entries participate in the digest but are not serialized.

Verification means the captured bytes and directory membership matched at those two points. It does not compare permissions, ownership, access times, or every filesystem metadata field. It cannot identify which process caused a concurrent change, and it cannot prove no change occurred and was reverted between snapshots.

Manifest digests can act as stable fingerprints of an unchanged source set across reports. Treat them as sensitive aggregate metadata, not as anonymity.

If a source is actively changing, verification may be inconclusive even though Skuggsja only read it. If auditing itself fails, the report remains available with an aggregate warning. `--no-source-audit` skips both snapshots and makes the report explicitly unverified.

## Persisted artifact

The default artifact is the OS user-cache path ending in `skuggsja/rewind.json`. Its contents include:

- provider names, status, verification wording, limitations, and aggregate warnings;
- coverage timestamps and timezone;
- counts by provider, day, hour, weekday, model, and project basename;
- prompt-length aggregates;
- provider-scoped source-recorded token ledgers where available, including their ledger-level exactness flag;
- source-audit digests and aggregate change counts;
- fixed methodology text.

Before reading or writing, Skuggsja refuses a fixed artifact path inside a configured source root or aliased to a source file through a cleaned path, resolved symlink, or existing hard link. The writer creates or changes the containing directory to mode `0700`, writes a mode-`0600` temporary file, syncs it, and renames it into place. Those artifact modes provide their intended protection on Unix-like systems; unlike the SQLite temporary-copy path, the artifact writer does not establish an equivalent protected Windows DACL.

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

This removes only the default `rewind.json` and removes the product cache directory only if it is then empty. It deliberately refuses to act on an unexpected filename or when configured source locations overlap or alias the artifact. It does not delete:

- source histories;
- stdout redirections or copies;
- a stale SQLite temporary directory left by an ungraceful termination.

All aggregates are regenerable from the histories still present at the next run.

## Testing the boundary

Repository tests use synthetic fixtures and temporary databases. They verify that serialized reports exclude absolute source paths and internal session/prompt/call/tool identifiers, that live WAL-backed SQLite data can be read through a private copy, that source audit detects directory changes, and that browser assets contain no external origins.

Tests demonstrate the coded invariants for covered cases; they are not a formal proof. Please report a privacy or security issue through the process in [SECURITY.md](SECURITY.md) without attaching real history files.
