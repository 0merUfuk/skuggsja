# Contributing

Thank you for helping improve Skuggsja. The project treats privacy claims as part of the executable contract: a parser is not complete until its data minimization, source safety, and verification scope are tested and documented.

## Prerequisites

- Go 1.25.6 or newer compatible toolchain
- Git
- A POSIX-like shell for the commands below; equivalent Go commands work on Windows

Dependency and toolchain installation may use the network. The product's zero-outbound statement applies to the built program at runtime, not to development tooling.

## Set up and test

```sh
git clone <your-fork-url>
cd skuggsja
go mod download
go test ./...
go vet ./...
go build -trimpath -o ./skuggsja ./cmd/skuggsja
```

Before submitting a change, format touched Go files and rerun the checks:

```sh
gofmt -w path/to/changed.go path/to/changed_test.go
go test ./...
go vet ./...
```

For concurrency-sensitive changes, also run:

```sh
go test -race ./...
```

Do not run integration experiments against histories you do not own or have permission to inspect.

## Repository conventions

- Keep packages focused and internal unless there is a demonstrated external API requirement.
- Prefer the standard library. A new dependency needs a concrete justification, license review, and discussion of build/network and supply-chain cost.
- Wrap errors with a concise operation using `%w`; callers decide whether an error is fatal or becomes a provider warning.
- Accept `context.Context` on discovery, reading, hashing, and database work that can block.
- Use deterministic ordering before serialization or tests.
- Bound input sizes and concurrency explicitly.
- Write table-driven or focused behavioral tests with `t.TempDir` and synthetic data.
- Keep CLI output concise and actionable. Keep persisted warning messages path-free and content-free.

Commit subjects follow Conventional Commit form with an imperative summary:

```text
feat(cursor): detect a supported composer schema
fix(audit): include a newly created WAL sidecar
docs(privacy): narrow the runtime network claim
test(codex): cover paginated prompt deduplication
```

Use `feat`, `fix`, `docs`, `test`, `refactor`, `perf`, `build`, `ci`, or `chore` as appropriate. Keep unrelated changes in separate commits.

## Privacy invariants

Every contribution must preserve these invariants unless an explicit, reviewed design change updates the product promise:

1. Raw prompt and response content must not enter `internal/model` or `analytics.Report`.
2. Absolute source paths and session/prompt/call/tool identifiers must not be serialized.
3. Project paths must pass through `provider.ProjectName`; other source labels must be bounded with `provider.SafeLabel` or an equally strict transformation.
4. Source files must be opened read-only. Do not use `os.OpenFile` on a history source.
5. SQLite must never open a source database. Use `internal/sqlitecopy` and query only the private copy.
6. A reader must list every intended source during `Discover` before `Read` accesses it, so the default audit can bracket the operation.
7. Parser problems must produce aggregate codes, counts, and fixed messages—never source excerpts or record-specific paths.
8. An unavailable provider ledger or unrecognized core schema must stay unavailable. Do not estimate tokens, infer unknown schemas, or silently merge histories with incompatible semantics. A provider may expose an available ledger whose individual numeric categories use zero for either recorded zero or source-level absence; document that ambiguity because the aggregate token model does not represent per-category presence independently.
9. Runtime code and embedded browser assets must not add outbound requests, remote assets, telemetry, update checks, or analytics.
10. The UI must render source-provided values with `textContent`, `createElement`, or equivalent text-safe DOM APIs, never HTML interpolation.

The serialization regression test should be expanded whenever a normalized or report field changes. Use unmistakably sensitive synthetic values and assert that none reach JSON.

## Adding or changing a provider

A provider implements the two-step interface in `internal/provider`:

```go
type Reader interface {
    Harness() model.Harness
    DisplayName() string
    Discover(context.Context) (Discovery, error)
    Read(context.Context, Discovery) model.ProviderResult
}
```

### Discovery

- Resolve verified native source locations and explicit path overrides; document excluded metadata stores separately.
- Avoid merging primary and derived indexes unless their identities and duplication semantics are proven.
- Return stable, sorted file lists.
- Treat a missing optional install as an empty discovery, not a fatal error.
- Declare every configured root and standalone input before fallible discovery. Keep parsed files, supplemental/audit-only inputs, and configured-but-absent paths separate; all three participate in source/output safety. Never serialize these paths.
- For SQLite, discover the primary database and declare its parent in `ProtectedDirectories` before any fallible inspection. Protected parents prevent artifact/copy writes but never expand recursive audit roots; the audit and copy layer handle present sidecars.

### Reading

- Start with status `not found` and use `supported with warnings`, `unsupported schema`, or `unavailable` precisely. SQLite adapters must recognize their core schema before reporting support. Existing JSONL adapters select candidates by filename and should gain explicit format evidence before any claim is broadened.
- Set `VerificationLevel` to the evidence actually obtained: for example, `synthetic fixture`, `schema-verified synthetic; not real-data verified`, or `real data on macOS`.
- State provider-specific limitations in the result rather than hiding missing metrics.
- Deduplicate only with a stable key whose semantics are understood.
- Keep children/subagents labeled; analytics excludes them from root-session metrics.
- Count only human prompts. Filter injected context, summaries, tool results, and copied alternate representations.
- Set token availability and exactness only for explicit source fields. Keep cache-read, cache-write, and reasoning categories distinct.
- Refuse unknown core SQLite schemas and ambiguous history modes instead of probing with speculative queries.

### Verification levels

Use narrow language:

- **Unit-tested**: helper behavior only.
- **Fixture-tested** or **schema-verified synthetic**: a synthetic file/database representing a documented or inspected schema passes.
- **Local smoke-tested**: a read-only run against real local data completed, with source audit results recorded separately.
- **Real-data verified on _OS_**: representative real histories were inspected on the named platform and key metrics were independently checked.

Do not turn compilation on Linux or Windows into a real-data support claim. Do not describe a synthetic Cursor fixture as real-data validation.

### Provider test checklist

- missing source;
- minimal supported source;
- malformed and partial records;
- child/root classification;
- prompt filtering and deduplication;
- model and tool-call semantics;
- token availability/exactness semantics, including zero-versus-missing ambiguity;
- project basename with no path persistence;
- deterministic results;
- unknown schema or history mode;
- live SQLite WAL behavior when applicable;
- unchanged synthetic sources and neutral handling of an external writer during generation;
- serialization contains no fixture secrets or internal IDs.

## Fixtures

Only synthetic fixtures belong in Git.

- Use names such as `synthetic-session`, `example-project`, and `model-test`.
- Do not copy and redact a real history; structural remnants can still reveal content or identifiers.
- Build SQLite fixtures inside tests with `t.TempDir` when practical.
- Keep fixture timestamps deterministic and explain any schema facts the fixture represents.
- Keep records minimal while still exercising the behavior.
- Never add home-directory paths, API keys, access tokens, user IDs, real repository names, real prompts/responses, or database pages copied from a real source.

If a real-data smoke test is necessary, run it locally, do not capture content in terminal output, record source activity separately from the read-only access guarantee, record only aggregate conclusions, and delete generated test exports afterward. Concurrent harness activity is normal runtime information, not a failure.

The release-only opt-in verifier waits for 60 continuous seconds of unchanged source metadata, then performs exactly one generation bracketed by independent hash/inventory snapshots. It requires complete, equal snapshots in the declared release scope:

```sh
SKUGGSJA_VERIFY_REAL_DATA=1 go test ./internal/app -run TestRealDataFullRunLeavesSourcesUnchanged -count=1 -v
```

Run it only on a machine whose histories you are authorized to inspect. Its terminal output is aggregate-only. If no quiet window occurs within ten minutes, generation remains unstarted; do not relabel this as a runtime source-write failure.

For a self-hosted Codex verification session on macOS, explicitly snapshot and exclude only its Codex store from release equality:

```sh
scripts/verify-live-source-protection.sh --snapshot-codex-store --evidence-dir NEW_PRIVATE_DIRECTORY
```

This stores exact private hash/inventory manifests, including configured absence states. Generation still reads every original source, including Codex. The excluded manifest is not a byte-for-byte archive or an atomic snapshot of an active store. Record the precise exclusion and shared-store reasoning in `VERIFICATION.md`; exclude no other harness. Without the explicit flag, the release equality scope includes Codex too. The driver calibrates source-write denial and the external-connect observer independently of equality.

On macOS, `make verify-offline` builds synthetic histories for every Tier-1 adapter, calibrates the external-connect observer with a deliberate Go probe, and exercises generation plus every localhost UI/API route with remote networking denied. The probe, fixture builder, and DYLD guard under `scripts/` are verification-only and are excluded from release builds.

## Web UI changes

The three embedded assets are `web/index.html`, `web/styles.css`, and `web/app.js`.

- Keep all fonts, styles, scripts, and images local; no external origin or `@import` is allowed.
- Preserve the single same-origin `fetch("/api/rewind")` network call.
- Keep the page functional for loading, error, empty, sparse, and dense reports.
- Preserve semantic landmarks, keyboard operation, visible focus, touch targets, responsive layouts, forced-colors support, and reduced-motion behavior.
- Never interpolate report labels into `innerHTML`, SVG markup strings, URLs, selectors, or class names.
- Update `web/embed_test.go` if the embedded asset contract intentionally changes.

Run `go test ./...` after any asset edit because the UI and CSP invariants are tested from the embedded filesystem.

## Documentation changes

Claims must match the current implementation and evidence:

- Say “runtime” when discussing zero outbound; builds and installs can use the network.
- Distinguish read-only source access from the raw private SQLite copy.
- Keep the always-on read-only guarantee separate from optional, neutral runtime source activity and release-only unchanged-source equality.
- Distinguish discovered transcript and supplemental-input counts from the audit manifest, which also includes sidecars and configured absences.
- Keep local data coverage separate from lifetime usage; index-only evidence adds no invented usage.
- Distinguish cross-platform code paths from real-data verification.
- State Cursor's synthetic/schema verification explicitly until real-data evidence exists.
- Keep provider limitations and metric definitions aligned with code.

Update [README.md](README.md), [ARCHITECTURE.md](ARCHITECTURE.md), [PRIVACY.md](PRIVACY.md), [SECURITY.md](SECURITY.md), and [CHANGELOG.md](CHANGELOG.md) as appropriate.

## Pull requests

A focused pull request should include:

- the problem and why the chosen behavior is honest;
- tests using synthetic inputs;
- privacy/security impact;
- provider and OS verification scope;
- user-visible limitations or migration notes;
- documentation and changelog entries when behavior changes.

By contributing, you agree that your contribution is licensed under the repository's [MIT License](LICENSE).
