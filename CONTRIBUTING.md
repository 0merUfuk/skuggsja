# Contributing

Thank you for helping improve Skuggsja. The project treats privacy claims as part of the executable contract: a parser is not complete until its data minimization, source safety, and verification scope are tested and documented.

## Prerequisites

- Go 1.27.1 or newer compatible toolchain
- Git
- A POSIX-like shell for the commands below; equivalent Go commands work on Windows
- Node.js 22.23.1, matching CI, for frontend regression tests, installation checks and browser verification
- Python 3 for release archive validation
- GoReleaser 2.18.1 for package builds, matching CI
- Chrome for Testing Headless Shell for browser verification

Node, Python, GoReleaser and Chrome are development tools, not product dependencies. The Go suite skips its frontend regression check if Node is unavailable; install Node before claiming the full frontend suite passed. Python and GoReleaser are needed only for packaging work; Chrome is needed for browser QA.

Dependency and toolchain installation may use the network. The product's zero-outbound statement applies to the built program at runtime, not to development tooling.

## Set up and test

From the repository root:

```sh
go mod download
go test ./...
go vet ./...
CGO_ENABLED=0 go build -trimpath -o ./bin/skuggsja ./cmd/skuggsja
```

Build artifacts belong in the ignored `bin/` directory. `./bin/skuggsja version` and `./bin/skuggsja --help` inspect the CLI without discovering or reading histories; running it without a subcommand generates a report from your local sources. On Windows, use `go build -trimpath -o ./bin/skuggsja.exe ./cmd/skuggsja`.

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

### Installation and package checks

Exercise the built executable through an isolated install prefix, with every source redirected to disposable synthetic data:

```sh
node scripts/verify-install.cjs bin/skuggsja dev
node --test scripts/generate_homebrew_test.cjs
python3 scripts/verify_packages_test.py
```

On Windows, pass `bin/skuggsja.exe`. The optional second argument is the expected version; replace `dev` with the exact expected version for a release or CI build. This checks the CLI, first use, JSON persistence, synthetic counts, localhost routes and CSP, cleanup, and reinstall/removal. It changes `PATH` only in its child processes and preserves the user's home and harness settings. Its synthetic source-hash checks are separate from the real-data release equality record. Node's Windows termination behavior does not establish graceful Go signal handling there.

Build and inspect all six platform archives without publishing:

```sh
goreleaser check
goreleaser release --snapshot --clean --skip=publish
python3 scripts/verify_packages_test.py
python3 scripts/verify-packages.py dist
```

`--clean` replaces the generated `dist/` directory. Package validation requires exactly six checksummed archives, verifies regular-file members, current embedded assets, Go target/build metadata and checkout revision, then runs the native archive's installation smoke test. It does not read original histories. GoReleaser and dependency acquisition may use the network; this is a non-publishing build, not an offline-build claim.

### CI gates

[CI](.github/workflows/ci.yml) runs Go tests, vet, frontend/formula tests, builds, and installed-binary checks on macOS, Linux and Windows. Race checks run on macOS and Linux; the macOS job also runs the calibrated runtime network-isolation test. Separate jobs check Go formatting, known vulnerabilities and workflow syntax, then build and validate six snapshot packages. The release workflow reuses these jobs before producing publishable artifacts.

Browser QA uses the retained-report procedure below; CI installation HTTP checks do not replace rendered-browser verification. See [RELEASING.md](RELEASING.md) for publication and tap synchronization.

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
11. Tool-call and model-event units remain provider-scoped. Do not add a global sum, ranking, or leading-model claim across harnesses; preserve unavailable counts as unavailable.

The serialization regression test should be expanded whenever a normalized or report field changes. Use unmistakably sensitive synthetic values and assert that none reach JSON.

The current report schema is 3. The removal of `totals.tool_calls` is intentional; consumers use provider counts and `tool_calls_available`. A fixture spanning distinct provider-native count mechanisms must preserve both counts without serializing a combined tool-call or model-event total.

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
- model and tool-call semantics, including provider-native counts and unavailable metrics without a cross-provider total;
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

The release-only opt-in verifier performs up to eight direct generation windows, with no idle preflight or quiet wait. Every attempt gets fresh independent hash/inventory snapshots and a distinct aggregate; the first complete equality result wins. Configure a private evidence directory to retain every attempted comparison and aggregate:

```sh
mkdir -m 700 /path/to/new-release-evidence
SKUGGSJA_VERIFY_REAL_DATA=1 \
  SKUGGSJA_RELEASE_EVIDENCE_DIR=/path/to/new-release-evidence \
  go test ./internal/app -run TestRealDataFullRunLeavesSourcesUnchanged -count=1 -v
```

Run it only on a machine whose histories you are authorized to inspect. Terminal output contains aggregate counts and digests; exact source paths remain in private manifests. Attempts that detect activity stay in the evidence record. The verifier stops on generation errors or cancellation, or after eight comparisons without equality; none of these release outcomes changes the runtime source-access guarantee. A successful selected report is retained as `rewind.json`; when eight comparisons end without equality, the last report is labeled `rewind-observed-changing.json`.

For a self-hosted Codex verification session on macOS, explicitly snapshot and exclude only its Codex store from release equality:

```sh
scripts/verify-live-source-protection.sh --snapshot-codex-store --evidence-dir NEW_PRIVATE_DIRECTORY
```

This stores exact private hash/inventory manifests, including configured absence states. Generation still reads every original source, including Codex. The excluded manifest is not a byte-for-byte archive or an atomic snapshot of an active store. Record the precise exclusion and shared-store reasoning in `VERIFICATION.md`; exclude no other harness. Without the explicit flag, the release equality scope includes Codex too. The driver calibrates source-write denial and the external-connect observer independently of equality.

When the release owner explicitly accepts a Claude-plus-Cursor fallback after a failed complete comparison, use the separately labeled scope:

```sh
scripts/verify-live-source-protection.sh --snapshot-codex-store --claude-cursor-fallback --evidence-dir NEW_PRIVATE_DIRECTORY
```

This measures exactly Claude and Cursor, retains the own-Codex snapshot, and labels Hermes live equality `unmeasured`. All four original providers remain enabled for ingestion and protected by the same source-write policy. `release-scope.json`, scoped manifest names and `rewind-claude-cursor-verified.json` prevent this result from implying complete-scope equality. Failed attempts and the earlier complete-scope result remain evidence. There is no generic provider filter or further narrowing option. Do not pause, signal, suspend or kill an owning harness to obtain an equality record; an unmet comparison is an honest result.

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

### Browser verification

Use Node.js 22.23.1 and an installed Chrome for Testing Headless Shell executable. Keep the executable, retained aggregate, screenshots, and browser evidence outside the repository. The procedure exercises the real embedded UI through Chrome DevTools Protocol (CDP); the DOM stubs in the JavaScript unit tests do not replace browser rendering.

Build the verification-only server and serve the current-schema aggregate retained by the release run:

```sh
go build -trimpath -o /path/to/private/serve-report ./scripts/serve-report
/path/to/private/serve-report -report /path/to/new-release-evidence/rewind.json
```

The server prints the report SHA-256 and its loopback URL. Keep it running and pass that URL, the same aggregate, and a new evidence directory to the browser verifier:

```sh
node scripts/verify-browser.cjs \
  --chrome /path/to/chrome-headless-shell \
  --url http://127.0.0.1:PORT \
  --report /path/to/new-release-evidence/rewind.json \
  --evidence-dir /path/to/new-browser-evidence \
  --revision true
```

The verifier creates its evidence directory and isolated browser profile, checks that the served aggregate matches the retained file, exercises provider disclosures, records desktop/mobile screenshots, and observes requests. Full-page DPR-2 screenshots are captured as vertical tiles (`rewind-<width>-dpr2-full-NN.png`) because one 2x capture of the whole report exceeds Chromium's single-capture raster budget and drops the DevTools socket; the tiles hold the same device pixels and skip no region. The run also launches with `--disable-component-extensions-with-background-pages` and records browser-internal child targets separately from product scope, so a Chrome component extension cannot fail an otherwise clean observation. It calibrates interception with a deliberately denied external control; on macOS it also applies the verification-only remote-network denial policy. Inspect `browser-result.json` and the screenshots before claiming a pass. This establishes browser evidence without rereading histories or changing the release input set. Stop the verification server afterward. The profile is removed on normal exit; screenshots and the aggregate remain private evidence.

Verify that tool-call values appear only in provider folios, unavailable counts remain unavailable, and model rankings and meter scales restart for each harness. The mere presence of the browser tools is not a completed rendering or zero-outbound check; record actual outcomes in `VERIFICATION.md`.

The UI revision checks capture 1280, 1440, 1728 and 1920 CSS-pixel viewports at DPR 2, plus 320 and 390 CSS-pixel mobile layouts. Computed type/spacing values, parent content-box bounds and real-value visibility are retained with the screenshots. The verifier checks circle area against recorded sessions/prompts, non-overlap, keyboard/touch detail and native model scales. Synthetic edge-state checks are labeled separately from the retained real report. None of these checks generates an aggregate or reads original histories.

Run the frontend suites directly with `node --test web/source_activity_test.cjs web/usage_chart_test.cjs`; the Go embedded-UI test runs both as well. The exact asset allowlist, single same-origin fetch assertion and CSP tests remain in force. The UI renders with the bundled Open Font License webfonts in `web/fonts` — embedded with the three UI files, served from loopback only, and covered by `TestEmbeddedFontsAreBundledAndReferenced`, so a stylesheet font that is not embedded fails `go test ./web/`. Every other typeface resolves through the system stacks already defined in `styles.css`; the UI never requests a font from a network origin.

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
