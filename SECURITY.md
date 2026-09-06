# Security

Skuggsja processes local files that can contain private conversations, credentials pasted into prompts, source paths, and proprietary project names. Its primary security goal is to keep source content on the machine, avoid modifying the source histories, and expose only a reduced aggregate.

Read [PRIVACY.md](PRIVACY.md) for the complete data lifecycle.

## Reporting a vulnerability

Please do not open a public issue containing an exploit, local path, Rewind artifact, agent history, prompt, response, database, screenshot of personal metrics, or other sensitive evidence.

Use GitHub's private vulnerability-reporting or Security Advisory interface for this repository if it is enabled. If that interface is unavailable, contact the maintainer privately through their GitHub profile before publishing details. Include only the minimum synthetic information needed to reproduce the issue:

- affected commit or version;
- operating system and Go version;
- provider adapter involved;
- impact and expected behavior;
- reproduction using a synthetic fixture;
- proposed mitigation, if known.

Never send real Claude, Codex, Hermes, or Cursor history files. There is no published response-time SLA yet.

## Verification scope

The current code has operating-system branches for macOS, Linux, and Windows. Real-provider-data verification has occurred only on macOS for the adapters that claim it. Cursor is schema-verified with synthetic data and is not real-data verified. Cross-platform compilation or path resolution is not the same as real-data security verification.

Until versioned releases exist, security fixes target the latest `main` branch. The [changelog](CHANGELOG.md) records release status.

## Security properties

### Runtime network isolation

The installed/built Skuggsja process has no outbound network client, telemetry, update check, CDN, or remote browser dependency. It binds the UI to IPv4 loopback (`127.0.0.1`) and serves an in-memory aggregate plus embedded static assets.

This claim begins after installation. Building or running through the Go tool may download the requested Go toolchain and modules. Launching the default browser delegates to software outside Skuggsja; `--no-open` avoids that launch.

The macOS verification harness creates disposable synthetic histories for all four adapters, runs generation and the localhost server under both a Seatbelt profile that denies remote networking and a DYLD guard that records and rejects non-loopback `connect`, `connectx`, `sendto`, and `sendmsg` destinations, then retrieves every UI/API route locally. A calibrated Go network probe must be observed and denied before the product run, while the product run must record no external attempt. The guard, probe, and fixture builder live under `scripts/` and are never linked into release binaries. A separate source-policy test limits production network-capable imports to the loopback server.

### Source isolation

- JSONL and compressed history files are opened read-only.
- SQLite sources and persistent sidecars must be regular, non-symlink files and are copied through read-only handles into a private workspace. SQLite opens only the copy. The prospective workspace is checked against every source root/file and protected SQLite parent before any directory is created; direct copy calls also reject a temporary parent within their source database directory.
- A stable-copy check verifies source identity, size, and SHA-256 before, during, and after copying and retries up to five times with bounded backoff.
- Copied databases must pass `PRAGMA quick_check`; provider queries reopen the copy read-only with `query_only` and defensive mode enabled, double-quoted-string parsing disabled, `trusted_schema` disabled, and temporary storage kept in memory.
- Read-only source access is an architectural guarantee in every run, including when activity observation is disabled or unavailable. Source handles use read-only access; reviewed artifact/private-copy writers have separate destinations. The production binary does not drop the user's OS permissions. An architectural regression test guards the write API surface, while the release verifier separately enforces OS-level source-write denial.
- The optional before/after observation compares source bytes, configured-path presence, root presence, and directory membership. Discovery is repeated after the first capture, with up to three capture attempts if the set changes. Concurrent activity is neutral information attributed to another process, never a source-integrity warning or runtime failure. Persistent discovery churn leaves best-effort analytics and an unavailable observation. Unchanged-source equality is a release-only check with an explicitly recorded scope.
- Unknown schemas and ambiguous history modes are skipped or degraded with warnings rather than queried or counted speculatively.
- Cancellation observed before report persistence preserves the prior artifact and cleans the private workspace. Once atomic persistence starts, a later cancellation may complete that already-started installation.

### Data minimization

- Raw prompt/response content has no field in the normalized or persisted model.
- Source paths and source IDs used for discovery, relationships, and deduplication are not serialized.
- Project paths are reduced to their final directory names.
- Source-provided labels are control-character filtered and length limited.
- Parser warnings are aggregate codes/counts with fixed content-free messages.
- The UI inserts report values with DOM text APIs rather than HTML interpolation.

### Artifact and web-server hardening

- The aggregate is encoded to a same-directory temporary file, set to mode `0600` where supported, synced, and renamed over the previous artifact.
- The default product directory is set to mode `0700` on Unix-like systems. Windows applies and validates a protected, inheritable DACL limited to the current user and LocalSystem before artifact creation; the Windows path is compile-tested but not runtime-verified.
- Generation fails before reads when a source root overlaps the artifact directory, or a source file equals the artifact, lies below its directory, or aliases it; `clean` performs the corresponding configured-source check before deletion. A standalone source file may live in an ancestor directory. Cleaned paths, resolved symlinks, conservative macOS/Windows case folding, existing ancestor identity, and hard-link identity are checked. Write and clean operations also reject output-directory symlink components. Declared paths remain protected after discovery errors.
- `clean` also refuses to remove an unexpected artifact filename.
- The server never binds a wildcard or LAN address; an occupied requested port falls back to another loopback port.
- HTTP requests whose normalized `Host` is not exactly `127.0.0.1`, `localhost`, or `::1` are rejected with status 421 to reduce DNS-rebinding exposure.
- Responses set a Content Security Policy restricting connections, scripts, styles, and fonts to self (with `data:` additionally allowed for images), `Referrer-Policy: no-referrer`, `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, and `Cross-Origin-Opener-Policy: same-origin`.
- The API response is marked `Cache-Control: no-store`.
- Embedded assets are tested for external origins and additional browser networking APIs.

### Resource bounds

- JSONL parsing has a 64 MiB maximum record size; oversized records are skipped.
- File parsing and hashing use bounded worker pools.
- Codex zstd decoding has an explicit decoder-memory ceiling.
- SQLite stable-copy attempts are limited to five, each copied database uses one connection, and SQLite integrity is checked before queries.

These bounds reduce accidental resource exhaustion; Skuggsja is not a hardened sandbox for actively malicious multi-gigabyte inputs.

## Threat model

Skuggsja is intended for one user inspecting their own histories on a machine they control.

| Threat | Mitigation | Residual risk |
| --- | --- | --- |
| Accidental source mutation by SQLite | SQLite opens a stable private copy, never the original | Raw OS reads can update filesystem access metadata; the audit does not compare all metadata |
| Source/output collision | Generation, private-workspace creation and `clean` fail closed on source/protected-parent overlap and source-file path/symlink/hard-link aliases; output symlink components are rejected | Misconfigured unrelated output handling outside Skuggsja remains the user's responsibility |
| Discovery set changes around capture | Capture then rediscover, retrying up to three times; retain the independent read-only guarantee | Persistent churn permits best-effort analytics with a neutral unavailable observation; coverage may change during the run |
| Upstream database changes during copy | Hash source and copied sets, retry five times with bounded backoff | A continuously active source can be skipped; a sophisticated same-hash race is outside the model |
| Raw content leaking into artifact | Content-free types, label sanitization, serialization tests | Project basenames and aggregates may still be identifying; uncovered parser bugs remain possible |
| Browser asset exfiltration | Embedded assets, one same-origin fetch, strict CSP/no-referrer | Browser extensions and browser-level behavior are outside the process |
| Remote access to report | Bind `127.0.0.1` only and reject non-loopback hostnames | Any sufficiently privileged local process can connect with an allowed Host header; there is no app authentication |
| Partial/malformed history causing false precision | Warnings, record bounds, SQLite-schema and Codex-mode refusal, per-provider coverage status, and provider-scoped semantics | Upstream private formats can change; Claude/Codex filename-matched valid JSON with no recognized records may currently look supported but empty |
| Artifact disclosure | Private Unix modes or a protected Windows DACL, plus a privacy-reduced schema | No encryption at rest; Windows runtime behavior is not yet validated; custom copies inherit downstream handling |
| Temporary SQLite disclosure | Private Unix modes or a validated protected Windows DACL, plus normal-path cleanup | Crash or `SIGKILL` may leave a raw copy in the OS temp directory; Windows runtime behavior has not been validated on a Windows machine |
| Dependency or build-chain compromise | Small dependency surface, reproducible module versions, reviewable Go build | Dependency acquisition is networked and remains a supply-chain trust decision |

## Out of scope and non-goals

Skuggsja does not currently provide:

- authentication, authorization, TLS, or multi-user serving;
- encryption of the aggregate or temporary SQLite copies;
- a sandbox for untrusted histories;
- protection from a compromised kernel, administrator, same-user process, browser, or Go toolchain;
- proof that upstream tools retained all usage or that a local history is account-complete;
- proof that source metadata such as access time was unchanged;
- automatic deletion of custom exports, shell redirections, or stale temp directories after an ungraceful crash;
- normalized cross-provider billing or token-cost calculations.

Do not expose the loopback port through a proxy, tunnel, container port publication, SSH forwarding, or firewall rule. The server is deliberately not designed for remote use.

## Maintainer checklist for sensitive changes

Changes to a reader, report field, dependency, output path, server, or browser asset should answer all of the following before merge:

1. Can raw content, an absolute path, or a stable source ID reach `analytics.Report`?
2. Does any code open a source with write flags or let SQLite see the original path?
3. Does the source audit know every file and sidecar the reader intends to access?
4. Can a configured source root or file overlap the fixed artifact without the guard rejecting it?
5. Does the change add an outbound socket, external browser origin, telemetry, or update behavior?
6. Are source-provided strings bounded and rendered only as text?
7. Does a synthetic regression test cover the privacy or security boundary?
8. Does this document or [PRIVACY.md](PRIVACY.md) need a narrower claim?

When evidence is incomplete, describe support as synthetic, schema-verified, or unverified rather than broadening the claim.
