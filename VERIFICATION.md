# Verification

This is the public verification summary. Complete original audit records, source manifests, reports, screenshots and scanner logs are retained privately and were preserved byte-for-byte before this summary replaced them. They are not distributable product assets.

**Current release: [v0.1.1](https://github.com/0merUfuk/skuggsja/releases/tag/v0.1.1). See [final release verification](#final-release-verification) and [the final UI correction](#singular-usage-labels--v011-candidate) for its evidence. Earlier tables preserve dated checkpoints; their narrower scopes do not replace the latest results.**

## Recorded baseline test and build results

Counts include Go tests and subtests. The real-data test is opt-in and intentionally skipped by ordinary suites.

| Check | Recorded result |
| --- | --- |
| `env -u SKUGGSJA_VERIFY_REAL_DATA go test -count=1 -json ./...` | 198 PASS, 0 FAIL, 1 opt-in SKIP; 12 tested packages pass |
| `env -u SKUGGSJA_VERIFY_REAL_DATA go test -race -count=1 -json ./...` | 198 PASS, 0 FAIL, the same intentional SKIP; no race report |
| Both frontend Node test files | 28/28 PASS |
| `go vet ./...` | Exit 0 |
| `go mod verify` | All modules verified |
| `govulncheck ./...` with Go 1.27.1 | No vulnerabilities found |
| JavaScript syntax and whitespace checks | Exit 0 |
| Embedded-asset external-origin assertion and CSP tests | PASS, without weakening either test |
| Chrome Headless 152.0.7977.82 over direct CDP | 645/645 PASS; 19 retained-report screenshots |
| Product browser network observation | Zero non-loopback requests |
| Browser fault observation | Zero JavaScript exceptions, uninstrumented child targets or observer failures |
| Native build and cross-platform archive verification | PASS; 6/6 archives and checksums |
| Isolated macOS arm64 archive installation lifecycle | 35/35 PASS |

The native binary and cross-builds used Go 1.27.1, `CGO_ENABLED=0` and `-trimpath`. Targets are macOS, Linux and Windows on amd64 and arm64. Archives contain only the executable, README and LICENSE. Cross-compilation does not establish Linux/Windows real-store behavior or Windows DACL behavior.

The archive lifecycle check covered command resolution from an unrelated directory, replacement, reinstall and removal in an isolated prefix. It was not a Homebrew download/install/upgrade/uninstall or quarantine/notarization test. Local packaging used GoReleaser 2.15.4; this does not claim execution of the separately pinned CI release version. No publication is established by these local checks.

Staticcheck 2026.2.1 retained 20 inspected maintenance advisories: proper-name error capitalization, an explicit metric-field mapping, an unused helper and deprecated AST metadata in an architectural test. They were not suppressed and the scan is not represented as clean. A prior malformed-index resilience suggestion also remains deliberate fail-closed behavior: an invalid supplemental identifier makes that index unavailable with a warning while detailed rollout parsing continues.

## Setup and distribution verification — 2026-09-06

The setup audit found no Skuggsja remote repository, release or published Homebrew package. Configuration files alone were not accepted as distribution evidence. Public-history preparation preserves the complete original Git history and original private audit documents outside the public repository; the public summaries retain findings and their limitations without personal source inventories.

The final disclosure implementation passed fresh ordinary Go and race runs: **198 PASS, 0 FAIL, 1 intentional opt-in SKIP** in each, across 12 tested packages. The frontend and formula-generator suites passed **34/34** checks. Package-verifier regressions passed **8/8** checks using real cross-built synthetic Go executables and tampered archives with recomputed checksums. `go vet`, module verification, workflow linting and whitespace checks passed. Version-pinned `govulncheck` v1.7.0 reported **no vulnerabilities found**. The previously installed vulnerability tool could not analyze the newer Go toolchain; its failed invocation is retained and is not counted as a pass.

GoReleaser **2.18.1** passed configuration validation and built all six local snapshot archives. Package verification checked archive hashes, regular-only contents, packaged documentation, the three embedded assets and executable GOOS/GOARCH/module/source-revision/static-build metadata. Release validation additionally rejects a dirty build. The native extracted binary passed **29/29** isolated installation checks, including command resolution outside the checkout, exact synthetic first-use counts, empty input, localhost HTML/assets/API/CSP, completion scripts, cleanup, reinstall and removal. The verifier explicitly overrides every supported history input; it does not read personal harness stores. Synthetic verification is not a new live-source equality record.

CI now reuses macOS/Linux/Windows test, build and installed-binary checks for releases, with a six-platform package gate, vulnerability checks and workflow linting. Third-party actions are pinned to verified full commit hashes, and the release tool and Node versions are explicit. Publication permissions are confined to the final job. The prepared ordinary Homebrew formula uses prebuilt macOS/Linux binaries with no Go, Python or Node runtime dependency; it is not an unsigned cask and does not remove quarantine metadata. A separately prepared tap workflow verifies the formula's attestation, workflow identity and exact stable tag before updating its own repository with its own automatic token. No personal access token is copied into Actions secrets. The tap updater passed 20 offline guard tests, including version-comment and four-platform URL consistency.

The final macOS synthetic runtime check completed generation and all localhost routes with **zero observed external connection attempts** under calibrated OS/network denial. An isolated CodeRabbit review covered seven release/configuration files; its Windows ARM64 host-name correction was implemented and covered by the package suite. Independent review also closed the wrong-target and ZIP special-file archive gaps.

The public GitHub repository has now been created with a description, topics, private vulnerability reporting, Dependabot security updates, secret scanning and push protection. Actions default to read permissions and require full-SHA action references. Source is synchronized to the public repository. The remote CI results below establish native test and package outcomes. **Release publication, release-asset attestation verification and the public Homebrew lifecycle remain pending at this pre-tag checkpoint.**

### Native CI and portability corrections

The first public CI run exposed a workflow PATH assumption and Windows portability gaps. The snapshot build now invokes the pinned GoReleaser action directly. The only production correction normalizes the private SQLite-copy file URI for Windows drives; query protections, source handles, persistent sidecars, copy stability, parsers and analytics remain unchanged. Test-only evidence-directory checks now use the existing Windows DACL implementation; Unix permission checks retain their requirements.

Windows mandatory byte locks prevent a complete read of an active normal-WAL SHM file. A dedicated native test requires both full captures to be unavailable and rejects an equality result, while still verifying committed-row ingestion through the private copy. The separate WAL visibility/equality fixture uses SQLite's documented exclusive mode on Windows and retains its full source audit, including SHM presence/absence. No live store or existing release-equality record was rerun or narrowed.

[CI run 34051324382](https://github.com/0merUfuk/skuggsja/actions/runs/34051324382), at source commit `0c19536128e5a685c501720e5800705cef9c7219`, passed **5/5 jobs**:

- Full native Go suites, vet, 34 frontend/formula tests, builds and **29/29 installed-CLI checks** on macOS, Linux and Windows/x64.
- Race suites on macOS and Linux; calibrated macOS zero-outbound runtime verification.
- **8/8 package-verifier regressions**, six snapshot archives with exact target/provenance metadata, and **29/29** checks on the extracted Linux package.
- Workflow linting, Go formatting and a vulnerability scan with no findings.

Fresh local ordinary and race suites passed **199 tests/subtests each**, with zero failures and the same intentional opt-in skip. The final isolated CodeRabbit review covered all 11 portability/workflow/formula changes with **zero findings**. Homebrew style reported **one formula, zero offenses** and the full strict formula audit passed. Its redundant explicit version was replaced with an attestation-checked release-version comment; Homebrew infers the same version from the four verified download URLs. The prepared tap workflow's first genuine hosted run passed its 20 tests and correctly made no formula change while no stable release existed.

The public-history audit compared **20 mapped commits and 1,487 tree entries** against privately retained originals, with production bytes and file modes preserved. Only the explicitly reviewed privacy/documentation/attribution/fixture-label transformations were allowed. The restored public source tree exactly matched the committed final source tree. Full-history and public-tree secret scans reported zero findings; independent content inspection also checked identifying paths and private context.

### Published v0.1.0 checkpoint

[v0.1.0](https://github.com/0merUfuk/skuggsja/releases/tag/v0.1.0) was published on 2026-09-06 from commit `4bd47168e7e5faacbada3266991a58ac95db9dd0`. [Release run 34051730255](https://github.com/0merUfuk/skuggsja/actions/runs/34051730255) passed **6/6 jobs**, including its five reused CI gates and artifact publication. The separately recorded pre-tag CI run above passed **5/5 jobs**. This section records that release's completed distribution evidence; it does not certify later changes.

| Published-artifact check | Result |
| --- | --- |
| Downloaded release inventory | Six platform archives, `checksums.txt` and `skuggsja.rb`; no missing or extra release assets |
| GitHub provenance verification | **8/8 assets PASS**, tied to the release workflow, exact `refs/tags/v0.1.0`, source commit and GitHub-hosted runner identity |
| Downloaded archive verification against the tagged source | **6/6 PASS** for checksums, regular-file contents, packaged documentation, embedded assets and Go target/build/source metadata |
| Extracted macOS arm64 release executable | **29/29 isolated installed-CLI checks PASS**, using synthetic sources |

[Homebrew run 34052028078](https://github.com/0merUfuk/homebrew-thematrix/actions/runs/34052028078) passed its attestation gate and **20/20 updater tests**, then committed only `Formula/skuggsja.rb`. All three hosted lifecycle jobs passed:

| Native Homebrew platform | Recorded v0.1.0 lifecycle |
| --- | --- |
| Linux amd64, `ubuntu-24.04` | Install, formula test, **29/29** isolated installed-CLI checks, already-current upgrade, reinstall, a second formula test, uninstall and removal assertions |
| macOS amd64, `macos-15-intel` | The same lifecycle and **29/29** installed-CLI checks |
| macOS arm64, `macos-15` | The same lifecycle and **29/29** installed-CLI checks |

The owner's macOS arm64 installation also passed **11/11 command stages**, including its **29/29** synthetic installed-CLI checks. The installed executable matched the downloaded release binary, and the installed formula matched the attested formula asset. Bash, Zsh and Fish completions were present. Uninstall was exercised, then v0.1.0 was installed again and left available at this checkpoint; the existing default report was unchanged. Upgrade verification was an already-current no-op. No older-to-newer release upgrade was fabricated.

A public documentation audit checked all 11 Markdown files: **38/38** local links/anchors, **5/5** external targets, **25/25** configuration variables, **5/5** explicit CLI flags and **9/9** referenced verification scripts passed. All **16/16** committed documentation/metadata blobs matched public `main`. One stale Windows DACL sentence in `ARCHITECTURE.md` was identified for correction without broadening real-harness coverage.

Read-only GitHub API checks confirmed five required CI contexts on `main`, strict up-to-date checks, one approving review with stale reviews dismissed, required conversation resolution, and disabled force pushes and branch deletion. **Administrator enforcement is disabled (`enforce_admins=false`): the owner retains administrator bypass, so these branch rules are not universal enforcement.** The active [stable-tag ruleset](https://github.com/0merUfuk/skuggsja/rules/22397946) blocks updates and deletion for `refs/tags/v*`, with no bypass actors; creation of new version tags remains allowed.

At this checkpoint, Linux/arm64 and Windows/arm64 archives were built and verified structurally, but their native CLI validation and Linux/arm64 Homebrew lifecycle had no completed result. Subsequent ARM and UI work must record its own results. None of the distribution checks reread personal histories, change the original equality scope, or establish Linux/Windows real-harness coverage.

### Native ARM64 verification

[CI run 34053233092](https://github.com/0merUfuk/skuggsja/actions/runs/34053233092), at `55dd2b8973909f39824fe044a39ebcb1974eb7f6`, passed **7/7 jobs**. Native tests now cover macOS arm64, Linux amd64/arm64 and Windows amd64/arm64. Each of the five native jobs verified the runner architecture, Node architecture, Go host and Go target before passing all 12 Go packages, module verification, vet, **34/34** frontend/formula checks, build and **29/29** isolated installed-CLI checks. Race suites passed on macOS and both Linux architectures; Windows race tests are intentionally excluded. The six-platform package gate passed its **8/8** regression checks, **6/6** archives and **29/29** extracted Linux executable checks. Calibrated runtime network denial remains a macOS check.

All seven CI contexts are now required on `main`, with strict checks bound to the GitHub Actions application. An independent API read confirmed this configuration; the documented administrator bypass remains. This native checkpoint predates the final singular-label correction. It establishes synthetic Windows DACL and CLI behavior on both architectures, not real Windows or Linux harness-store coverage.

[Homebrew run 34053315403](https://github.com/0merUfuk/homebrew-thematrix/actions/runs/34053315403) passed **5/5 jobs** for the unchanged, attested v0.1.0 formula. Linux arm64, Linux amd64, macOS arm64 and macOS amd64 each passed **29/29** installed-CLI checks, two formula tests, install, current-version upgrade, reinstall, uninstall and removal assertions. The updater passed **20/20** checks and made no commit for the unchanged formula. This closes native Homebrew coverage for all four formula targets; its current-version upgrade remains a no-op.

### Final release verification

[v0.1.1](https://github.com/0merUfuk/skuggsja/releases/tag/v0.1.1) was published from `b50703df997d3ffb297715a9af0c8a226bea3e46` after [main CI 34054057011](https://github.com/0merUfuk/skuggsja/actions/runs/34054057011) passed **7/7 jobs** and [release run 34054068746](https://github.com/0merUfuk/skuggsja/actions/runs/34054068746) passed **8/8 jobs**. All five native targets passed 12 Go packages, module verification, vet, **36/36** frontend/formula tests, build and **29/29** installed-CLI checks. macOS and both Linux architectures passed race checks; Windows race tests remain explicitly excluded. Snapshot package gates each passed **8/8** regressions, **6/6** archives and **29/29** extracted-native checks. The publish job separately verified the six versioned archives before attestation and publication.

All eight published assets were downloaded again. **8/8** hashes matched the release API, and **8/8** GitHub attestation verifications passed with the release workflow, exact tag, exact source SHA and hosted-runner policy enforced. An anonymous tag checkout used isolated Git configuration and disabled credentials/prompts. Against that tagged source, the downloaded packages passed **6/6** archive checks and the extracted macOS arm64 binary passed **29/29** isolated CLI checks. The previous v0.1.0 tag and artifacts were not moved or replaced.

The tap updater verified the new formula and committed only `Formula/skuggsja.rb` in `48db55cd6879450023adde7eb746eb749719bc41`. On the owner's macOS arm64 machine, the actual **0.1.0 → 0.1.1 Homebrew upgrade passed**, followed by the formula test and **29/29** isolated installed-CLI checks. The executable and formula match the attested release assets byte-for-byte, all three completion files are present, and the existing default report is unchanged. The installed command was left at v0.1.1. This is a real version-to-version upgrade; the earlier already-current no-op records remain historical evidence.

[Homebrew run 34054422118](https://github.com/0merUfuk/homebrew-thematrix/actions/runs/34054422118) passed **5/5 jobs** for v0.1.1. The updater passed **20/20** guard tests. Each of Linux amd64/arm64 and macOS amd64/arm64 passed native architecture/version assertions, actual install, **29/29** isolated CLI checks, two formula tests, current-version upgrade, reinstall, uninstall and removal assertions. These four jobs provide **116/116** installed-CLI checks and eight formula-test executions for the published patch.

The final singular-label UI passed **32/32** frontend tests and **1,582/1,582** actual Chrome/CDP assertions, with 53 screenshots and **63 product requests, all loopback**. The three served assets match the committed bytes. Detailed viewport, disclosure, focus, state and negative-control evidence appears below. No backend or CLI implementation changed between v0.1.0 and v0.1.1; original source/equality records remain intact.

Setup changes were pushed using the owner's retained administrator bypass; no GitHub PR approval is claimed. The actual final commits subsequently passed their CI gates, and publication waited for its separate verification jobs. Current branch protection requires all seven checks, one review and resolved conversations for actors without that bypass, and stable tags cannot be updated or deleted. Source and shared-tap security defaults include private vulnerability reporting, Dependabot alerts/security updates, secret scanning/push protection, read-default workflow tokens and full-SHA action enforcement. The latest API recheck found no configured Actions/Dependabot secrets and no open secret/Dependabot alerts. Neither repository has a separate code-scanning analysis; vulnerability, static and secret checks are recorded by their actual tool, not represented as CodeQL results. Shared-tap licensing and branch governance for unrelated formulas were left outside this change; Skuggsja's MIT license is included in every release archive.

## Read-only access and release equality are separate claims

Skuggsja opens original sources read-only, gives SQLite private copies, and guards output and cleanup destinations against source roots, source files and protected SQLite parents. The production binary is not an OS sandbox and does not remove the caller's filesystem permissions. Its source-access guarantee comes from its reader/writer architecture and regression checks.

Concurrent source activity is neutral runtime information. A changed-file observation does not imply a Skuggsja write and does not fail generation. Runtime observation, complete-scope release equality and scoped equality are distinct records.

Release equality compares complete before/after content hashes, sizes, existence, root states and directory inventories across generation. It cannot prove an atomic snapshot, unchanged access metadata, writer identity or absence of a change that reverted between captures.

### Complete scope: 0/8 in each direct invocation

The idle preflight was removed and generation was attempted with harnesses live. Both direct eight-attempt invocations completed with **0/8 equal windows**. Complete manifests were retained for failed comparisons. A normally active machine did not remain unchanged across any measured generation window; this is an operating-environment finding, not a runtime safety failure.

The verifying agent's own default Codex store was separately snapshotted and excluded from equality. That agent itself writes active rollouts and shared store metadata, so excluding only one rollout would leave its own store activity in the comparison. The snapshot was a hash/inventory record, not an atomic database archive. No alternate Codex store or unrelated source subset was silently excluded. All original providers remained enabled for ingestion.

### Explicit Claude/Cursor fallback: 1/3

After complete-scope failure, the explicit Claude-plus-Cursor fallback passed on its third attempt: **1/3 equal windows**. Both failed attempts remain part of the record. Hermes is a live database whose equality is **unmeasured in this fallback**. The complete-scope 0/8 findings remain failed and are not superseded by this narrower result.

The fallback changed only the labeled equality scope. Ingestion still covered all original providers, and source-write protection still covered all original sources. No additional narrowing was used. No live harness process was paused, signaled, suspended or killed during verification.

Both live verification invocations passed calibrated source-write controls with and without OS-level write denial, and generation recorded zero observed external connection attempts. The write-denial policy demonstrates that source writes could not succeed under that policy; it is not a syscall count of attempted writes. A separate denied external negative control calibrated the network observer before product execution. Dependency installation and development-tool networking are outside the runtime guarantee.

## Browser and presentation verification

The UI baseline was rendered at 1280, 1440, 1728 and 1920 CSS px, plus 320 and 390 CSS px, all at DPR 2. Before/after screenshots and computed layout measurements remain private because the rendered report contains actual usage data. Baseline screenshots, final viewport/full-page screenshots and chart/provider detail captures are indexed in the retained private evidence manifest.

The oversized layout came from viewport-dependent display `clamp()` values, a section rule overriding overview padding, near-viewport minimum height, generous provider spacing and a 1600px container cap. The root/body scale was already 16px/24px; it was not the cause. At the tested desktop widths, the earlier H1 grew from about 108px to 157px and the main count from about 230px to 304px.

The revised scale uses 12, 14, 16, 18, 24, 42 and 80px type tokens and 4, 8, 12, 16, 24, 32 and 48px spacing tokens. Body text remains 16px/24px; desktop H1 is 42px/47.04px, H2 is 24px/30px, and the primary display count is 80px/96px. Prose is bounded to 70ch and the container to 1280px. Provider padding/gaps are 24px. Mobile H1 is 32px, display counts are 64px, and provider padding/gutters are 16px.

Baseline inspection found painted elements extending outside their immediate parent despite no page-wide horizontal scroll. Structural layout changes addressed the offset display count, clock caption, rotated panel and full-bleed decorative elements. The completed six-width matrix recorded zero page overflow, visible block/content-box overflow or key-value clipping. Intentional calendar scrolling and accessibility-only content were classified explicitly.

### Honest chart geometry

The report schema contains model names and provider-native event counts, but no model-level sessions or prompts. The UI therefore uses a harness chart sized by recorded sessions or prompts, with separate model bars ranked and scaled within each provider. It explains that model-level sessions and prompts are unavailable. Tokens never determine geometry.

The deterministic packed layout uses vanilla JavaScript in the existing embedded `app.js`; radius is proportional to the square root of the exact count, so area is proportional to usage. Harness colors remain consistent with the legend and model groups. Hover, tap, focus, Enter and Space expose detail. There is no drag physics or remote dependency. Typography uses system sans-serif and monospace stacks. The embedded asset set remains `index.html`, `styles.css` and `app.js`.

Zero, one or two positive entities receive empty/exact bar states. More than 24 entities, or a distribution with unusably small circles, receives exact bars retaining the entire tail. Missing values remain unavailable and measured zeros remain zero. No minimum-area inflation or estimated Other bucket is used. Label sanitization, text-safe rendering, per-harness coverage and provider-native metric semantics remain in force.

The completed browser run checked API/report identity, real card values, unavailable labels, all model rows, chart proportions, non-overlap, bounds and pointer/keyboard detail. Explicitly synthetic edge cases covered small entity sets, a long tail, zero prompts, loading/error/empty/retry states and reduced motion. Synthetic data was not used for the real-report screenshots. The later Show more investigation added interaction and layout regressions beyond these earlier row-value assertions; its separate final results follow below.

CDP interception began before navigation. HTTP, WebSocket and child-target observations remained in scope under an isolated OS remote-network denial policy. Final ledgers were checked after browser exit and observer drainage. The browser negative control was separately labeled, rather than counted as product traffic.

The completed UI review reported zero production findings and two valid verifier findings: a helper assumed circles where honest bar fallback applied, and helper cleanup reset viewport emulation between intended mobile fixtures. Both verifier defects were corrected and independently browser-checked. This review record predates the later Show more investigation.

### Disclosure interaction corrections

The reported Show more defect was reproduced before editing. Eight far-edge pointer attempts across the four ranked-list controls at desktop and mobile widths opened none of them: the summary occupied only 82–93px of a 358–880px row. Clicking the text itself opened all eight, so native expansion worked. Every expanded control nevertheless retained its “Show N more” label and lacked an expansion marker. Long project tails also had no collapse action after the final row. The earlier browser checks proved that rows existed and a disclosure could open; they did not establish complete hit areas, correct state feedback or a usable return path.

Summaries now occupy the full row with a minimum 44px target, a plus/minus marker, visible keyboard focus and an accurate “Show fewer” expanded label. More than ten hidden rows add a bottom collapse button that returns focus and scroll to the summary; short tails avoid duplicate controls. Singular ranked values and their accessible meter labels now use “1 session” and “1 native event.” Counts, rankings, meter geometry and source handling are unchanged.

The final UI was tested at **1280, 1440, 1728 and 1920 CSS px**, plus **320 and 390 CSS px**, all at **DPR 2**. Real pointer/touch and keyboard input verified all 24 ranked-list disclosure cases: far-edge activation, exact labels, painted and contained rows, content growth, a stationary summary on opening, restored document/disclosure height on closure, visible returned focus, and Enter/Space operation. All provider/model/method/warning disclosures and section links were exercised. Existing real-value, chart geometry, loading/error/empty/retry, sparse-data and reduced-motion checks remained in scope.

| Final UI check | Result |
| --- | --- |
| `node --test web/source_activity_test.cjs web/usage_chart_test.cjs` | 30/30 PASS; 0 failures or skips |
| Main Chrome/CDP regression run | 1,245/1,245 PASS; 49 screenshots |
| Expanded provider/warning viewport supplement | 60/60 PASS; 18 screenshots at 1440, 320 and 390 CSS px, DPR 2 |
| Main/supplement product network observations | 55 and 16 requests respectively; zero non-loopback requests in either run |
| Browser faults and interception | Zero application exceptions, uninstrumented targets or observer failures; separately calibrated external negative controls |
| Served embedded assets | All three hashes match the verified worktree; CSP unchanged |
| Verification process cleanup | All owned servers and isolated browser profiles closed; no live harness process signalled |

Expanded model rows, the start and end of long project lists, provider coverage, token ledgers, limitations and warning lists were visually inspected at desktop and mobile widths. Actual viewport captures confirm readable wrapping and preserved provider-specific values. Large full-element Chrome captures can paint an otherwise offscreen fixed skip link inside their artificial capture area; separate viewport screenshots and 18 geometry checks confirmed that the unfocused link remained above the real viewport. Those full-element captures are supplemental diagnostics, not the authoritative viewport evidence.

The following paths are relative to `setup-audit-20260906/ui/` in the **private evidence archive**, not the repository. They contain actual usage and must not be published.

| Evidence | Private relative path |
| --- | --- |
| Before text-centre interaction ledger and 16 closed/expanded screenshots | `disclosure-before/browser-result.json`; `disclosure-before/before-{width}-more-{index}-{closed,expanded}.png` |
| Before far-edge failure ledger and 16 screenshots | `disclosure-before-edge/browser-result.json`; `disclosure-before-edge/before-{width}-more-{index}-{closed,trailing-edge}.png` |
| Final regression ledger and all six-width overview/expanded/end screenshots | `after-final-labels/browser-result.json`; `after-final-labels/rewind-{width}-disclosure-{index}-{expanded,end}.png` |
| Final provider/warning viewport ledger and screenshots | `after-final-detail-viewports/browser-result.json`; `after-final-detail-viewports/rewind-{width}-detail-{index}-viewport-{fraction}.png` |
| Exact file/screenshot digests, request counts and cleanup | `ui-final-manifest.json`; `UI-UX-AUDIT.md` |

The local `interface-guidelines` skill informed native semantics, touch targets, focus visibility and responsive checks. Independent local review found no actionable issue in the disclosure implementation and verifier. This correction used the previously retained aggregate only: it did not reread original histories, rerun live parser verification or change the release equality record.

### Singular usage labels — v0.1.1 candidate

The chart reused plural metric identifiers in accessible names and selected details, producing “1 sessions” and “1 prompts”; weekday meters also hardcoded “sessions.” Two new regression tests reproduced the defects against unchanged production code: **30 PASS, 2 FAIL**. A shared chart count-label formatter now supplies the correct noun to circle/key accessible names and selected details. Weekday meters use the existing plural helper. Zero, unavailable and plural values keep their meaning; counts, geometry, parsers and source handling are unchanged.

| Candidate check | Result |
| --- | --- |
| Frontend regression suites | **32/32 PASS**, including singular/zero/plural/unavailable labels, circle and bar layouts, metric changes and pointer/focus selection |
| Frontend plus Homebrew-generator suites | **36/36 PASS** |
| Fresh ordinary Go and race suites | **199 PASS each**, no failures; the existing opt-in real-data skip remains |
| Vet, native build and isolated installed CLI | PASS; **29/29** installed-command checks for the candidate version |
| Chrome Headless 152.0.7977.82 / CDP | **1,582/1,582 PASS**; 49 retained-report screenshots and four explicitly synthetic one-count chart screenshots |
| Product browser requests | **63**, all loopback; the separately labelled external negative control was blocked |
| Browser faults | Zero application exceptions, uninstrumented targets or observer failures |
| Embedded assets and CSP | All three served assets match the worktree byte-for-byte; existing asset-origin and CSP checks remain unchanged and pass |
| Independent isolated code review | Zero findings across the five changed presentation/test/verifier/CI files |

The full **1280, 1440, 1728, 1920, 320 and 390 CSS px** matrix at **DPR 2** retained the disclosure, real-value, overflow, chart geometry, keyboard/touch, loading/error/empty/retry and reduced-motion checks. Exact accessible-unit assertions cover every chart key, circle, selected detail and weekday meter. Separate synthetic cases exercise a one-session bar and three one-session circles in both Sessions and Prompts modes; their four mobile captures were visually inspected alongside actual retained-report views. All owned verification processes closed and the isolated Chrome profile was removed.

Evidence paths are relative to `setup-audit-20260906/ui-plural-patch/` in the **private evidence archive**, not the repository: `frontend-before.log`, `frontend-after.log`, `browser/browser-result.json`, `browser/rewind-{width}-dpr2.png`, `browser/rewind-{width}-disclosure-{index}-{expanded,end}.png`, `browser/synthetic-{one-recorded-session,three-single-session-harnesses}-{sessions,prompts}-390-dpr2.png`, `served-assets.json`, `summary.json` and `manifest.json`. Personal usage screenshots and report/source fingerprints remain private. This presentation correction used the retained aggregate only; no original history, live parser audit or release equality window was rerun.

### Taste principles applied

All 13 taste skills were read in full and adapted for a dense local retrospective. Marketing-page structures were not imported.

| Taste skill | Applied principle or deliberate exclusion |
| --- | --- |
| `brandkit` | Consistent functional color and typography establish identity; logo boards and generated imagery were unnecessary. |
| `brutalist-skill` | Structural rules and technical numerals provide hierarchy; giant type and simulated telemetry were excluded. |
| `gpt-tasteskill` | Varied component weight creates reading order; conversion formulas and oversized spacing were excluded. |
| `image-to-code-skill` | Actual screenshots guide inspection; a generated reference image would not validate data rendering. |
| `imagegen-frontend-mobile` | Mobile type, touch targets and collapsing groups were applied; native-app imagery was inapplicable. |
| `imagegen-frontend-web` | Section weight and a coherent palette were applied; image heroes and conversion funnels were excluded. |
| `minimalist-skill` | Flat surfaces, semantic color and type hierarchy replace ornamental backgrounds and uniform boxes. |
| `output-skill` | Viewport, edge-state and artifact checks support complete delivery. |
| `redesign-skill` | Computed-layout diagnosis preceded changes while the existing stack and verified behavior were preserved. |
| `soft-skill` | Responsive spacing discipline was applied; glass, pill styling and physics were excluded. |
| `stitch-skill` | Functional tokens have exact values; external services and perpetual motion were unnecessary. |
| `taste-skill-v1` | Dense grouping, monospace numerals and complete states suit the report; airy bento patterns did not. |
| `taste-skill` | Audit-first design and purposeful interaction were applied without promotional page recipes. |

## Metric and support boundaries

Sessions and prompts are derived only from recognized source events. Child sessions are reported separately from owner-session totals. Model-event and tool-call units remain provider-native: there is no global tool-call sum, universal model-turn ranking or cross-provider token total. Cursor tokens/tools are unavailable, and Codex cumulative tokens cannot be allocated exactly to models.

| Harness | Recorded validation | Remaining limits |
| --- | --- | --- |
| Claude Code | Real local ingestion on macOS and synthetic fixtures | Missing detailed history is surfaced as coverage; supplemental indexes never invent usage. |
| Codex | Real local ingestion on macOS and synthetic fixtures | Missing/corrupt detail remains unavailable; compressed rollouts are fixture-tested; model token allocation is unavailable. |
| Hermes | Real local ingestion on macOS and SQLite fixtures | Continually changing databases may be unavailable; live equality is unmeasured in the explicit fallback. |
| Cursor | Schema-verified synthetic fixtures; local parsing observed | No trusted token/tool ledger; missing/malformed bodies are omitted; local parsing is not promoted to semantic real-data verification. |

A provider may degrade with a scoped warning after safe source resolution. Failure to establish source/output separation can stop generation before writes. Partial-history reconciliation can omit unique older prompts; missing detail is never reconstructed from recollection, filesystem times or aggregate estimates. A hard crash can leave a private temporary database copy. Linux/Windows real-store verification remains outstanding. Windows/x64 synthetic CI now verifies private DACL behavior; this is separate from real-harness validation.

[FORENSIC-AUDIT.md](FORENSIC-AUDIT.md) documents generalizable discovery and deduplication findings. Raw histories, personal usage totals, absolute source paths, source/report fingerprints and private operational context remain outside the public record. Secret scanning is one check, not proof of anonymization.

## Dedicated Homebrew distribution — 2026-09-08

This record covers distribution and Homebrew migration only. It does not change
parser, analytics, real-history coverage, runtime-network or release-equality
claims elsewhere in this document.

Skuggsja now has its own dedicated tap:
[`0merUfuk/skuggsja`](https://github.com/0merUfuk/homebrew-skuggsja).
It continues to distribute the existing attested v0.1.1 macOS/Linux binaries;
no application version, tag, formula revision, bottle, cask, copied binary, or
replacement of an existing release asset was introduced for the tap move.

The recovery candidate
[`c5f9f6b`](https://github.com/0merUfuk/homebrew-skuggsja/commit/c5f9f6bd5955b0cc7127b18f2fd74aa0319a5aa5)
passed [5/5 CI jobs](https://github.com/0merUfuk/homebrew-skuggsja/actions/runs/34175126837):
120 Python checks and 27 Ruby checks in candidate verification, followed by
native macOS arm64/amd64 and Linux arm64/amd64 lifecycles. Each native platform
passed 30 package-lifecycle assertions and 36 receipt-recovery assertions.
Nested synthetic CLI results are separate checks and are not added to those
outer counts.

An earlier recovery candidate remains recorded as a failure: its two Linux jobs
stopped before helper execution because the network-isolation wrapper changed
the private trust configuration after dropping privileges. The
[diagnostic run](https://github.com/0merUfuk/homebrew-skuggsja/actions/runs/34174778830)
identified that environment boundary. The minimal correction preserved the
original isolated trust and network-denial settings after the privilege drop; it
did not weaken formula or command trust.

[Tap PR #2](https://github.com/0merUfuk/homebrew-skuggsja/pull/2) was reviewed
and merged through the dedicated tap's protected branch at
[`4ce8732`](https://github.com/0merUfuk/homebrew-skuggsja/commit/4ce8732a02565c45ac1d26ee716b4d6096911ce5).
Its [public-main CI](https://github.com/0merUfuk/homebrew-skuggsja/actions/runs/34175627876)
also passed 5/5 jobs with the same four-native 30-package/36-recovery scope,
using the public GitHub tap and qualified formula path. The hourly importer
subsequently completed as a [no-op](https://github.com/0merUfuk/homebrew-skuggsja/actions/runs/34175801737)
with `changed: false`.

The former Matrix-tap cutover is complete:
[former-tap PR #1](https://github.com/0merUfuk/homebrew-thematrix/pull/1) removed
only Skuggsja's formula, updater workflow, updater implementation and updater
test, while retaining:

```json
{"skuggsja":"0merUfuk/skuggsja"}
```

A post-cutover audit passed 35/35 assertions. It confirmed that the retired
Skuggsja workflow is deleted with no active runs, and that eight unrelated
Matrix/Rifja paths retained their Git blobs and modes.

A real pre-existing Homebrew installation also completed `brew update` with a
19/19 scoped acceptance result. Its one unpinned v0.1.1 receipt changed only
`source.tap` from the Matrix tap to the dedicated tap; retained files, links,
shell completions, pin state and existing generated report were preserved.
That installation did not require uninstallation, reinstall, helper application,
or removal of the Matrix tap. This host result covers one unpinned retained keg;
pinned and multi-keg recovery is covered by the separate four-platform CI.

The original v0.1.0 and v0.1.1 releases remain intact. A later read-only
integrity recheck passed 49/49 assertions over their release metadata, assets,
digests and tag identities. GitHub immutable releases are enabled for future
releases; the two existing releases remain mutable and are not represented as
retroactively immutable. The draft/upload/byte-verification/immutable-publish
workflow has regression coverage, but no new live immutable publication is
claimed here.

An isolated empty-prefix first-command installation passed only with
`HOMEBREW_NO_GITHUB_API=1`; an unqualified default-environment first-command
result is not claimed. The migration helper is individually atomic per receipt,
not a transaction across every retained keg. Existing provider, coverage and
metric limitations remain in force.

## Data-correctness and provenance fixes — 2026-09-12

Three confirmed findings from the retained data-comparison review were fixed and
re-verified from source and behavior; the finding text itself was re-derived
against the current Hermes revision rather than inherited.

Hermes compaction archives the carried-tail original and inserts a byte-exact
clone with a fresh row id (`_clone_message_rows` copies every column except
`id`, `active` and `compacted`). The adapter had keyed prompts by physical row
id, so one owner action could count twice. Prompt identity is now the stored
event: eligible user rows are grouped by session, exact timestamp and content
and count once per group, while a group holding several active rows is counted
once per active row so simultaneously live messages are never merged. A schema
without the `active` column falls back to one prompt per row and emits
`prompt_identity_unavailable`.

Measured on this machine's live Hermes history with the previous v0.1.1 binary
and the fixed build against the same sources (isolated output directories,
`--no-source-audit`, no source writes): Hermes root prompts **1,671 → 1,383**,
removing 288 duplicate representations, which matches the 288 extra rows
measured directly by grouping the same eligible rows in SQL. Aggregate prompts
fell **9,108 → 8,820**; Hermes tool calls and sessions were unchanged. The
synthetic control set covers the clone case (2 → 1), a summary-only archived
original (1), repeated text at distinct timestamps (2, preserved), and the
stored active tool counter (9, unchanged).

Project ordering for equal-count names that differ only by case is now
deterministic through an exact-name tie-break; the regression test builds 200
reports and requires one stable order. The persisted artifact now records
`generator_version`, the executable that produced it, and the report footer
shows it. `schema_version` remains 3 and the field is additive.

Local gates on the exact head: `go test ./...` 12/12 packages, `go test -race
./...` 12/12 packages, `go vet` and `gofmt` clean, frontend suites 36/36,
release-workflow regression 20/20, installed-CLI smoke 29/29, and the offline
runtime check passed with zero observed external connect attempts. Pull request
[#2](https://github.com/0merUfuk/skuggsja/pull/2) CI passed all required
contexts on head `04fd9a6` in
[run 34691635005](https://github.com/0merUfuk/skuggsja/actions/runs/34691635005).

The Hermes tool-call total is unchanged numerically; it is the stored
active-transcript counter, which in-place compaction, transcript replacement,
rewind or clear can lower. It is now disclosed as a provider limitation rather
than presented only as a native count. The historical +47 prompt / −539
tool-call attribution remains unattributed; the old database and WAL states
needed to reconstruct it do not exist.

## v0.1.2 release and Homebrew synchronization — 2026-09-12

The v0.1.2 tag points at the reviewed `main` commit `ef4dfe7` (annotated tag
object `badfb041`). Release run
[34699613650](https://github.com/0merUfuk/skuggsja/actions/runs/34699613650)
passed the full verification matrix, built the six platform archives, verified
them, attested all eight assets and created the draft release.

The workflow's draft byte-verification step then aborted because
`GET /releases/tags/{tag}` answers 404 for an unpublished draft even with
`contents: write`. The draft was completed with equivalent verification:

- 8/8 assets downloaded by release id; every SHA-256 matched GitHub's uploaded
  asset digest and the published `checksums.txt` covered 6/6 archives.
- The formula pinned every macOS/Linux archive checksum and was byte-identical
  to fresh generator output (`e8065419…`).
- `gh attestation verify` passed for all eight assets with the release
  workflow signer, `refs/tags/v0.1.2` and self-hosted runners denied.
- `verify-packages.py dist v0.1.2` from a clean worktree at the tag passed 6/6
  archive, embedded-asset and Go provenance checks plus 29/29 native
  installed-package checks.

The release was then published with `gh release edit v0.1.2 --draft=false
--verify-tag`; the API reports `draft: false`, `immutable: true`, eight assets
and the latest release. Pull request
[#4](https://github.com/0merUfuk/skuggsja/pull/4) (merged `263bf20`) fixes the
pipeline defect by resolving drafts from the release list and lets the package
verifier derive the version from the stable tag when the unpublished
`metadata.json` is absent; its regression suite passes 21/21.

Homebrew synchronization: the importer was dispatched and opened
[candidate PR #3](https://github.com/0merUfuk/homebrew-skuggsja/pull/3). Its
five required checks passed on the exact head, the candidate formula was
byte-identical to the attested release formula, and it was approved and merged
as `d8e2271`. The tap's post-merge CI passed, and the public formula now serves
0.1.2. The former Matrix tap still maps `{"skuggsja":"0merUfuk/skuggsja"}` and
contains no Skuggsja formula.

On the owner's machine `brew update` and `brew upgrade` moved the installation
from 0.1.1 to 0.1.2, `brew test` passed, and the installed binary's SHA-256
(`bc9f4c26…`) equals the binary inside the released darwin_arm64 archive. A
regenerated report records `generator_version: 0.1.2`, schema 3, and Hermes
prompts of 1,387 — exactly the distinct stored prompt events counted directly
in the live database (1,676 physical rows) — with 77 root sessions, 157
children, and the stored active-transcript tool counter unchanged in
semantics.

## Interface art direction and bundled fonts — 2026-09-13

The Rewind interface was rebuilt on `main` after `v0.1.2`. The earlier design
was technically sound but read as assembled from a generic component library:
repeated rounded cards, a gradient and glow vocabulary, symmetric sections and
the same container shape for every band. The replacement treats the report as a
printed field instrument: full-bleed bands with one inset content column, hard
2px ink rules, hairline data rows instead of boxes, chapter folios that number
and indent the sections, margin notes set as serif italic marginalia, exactly
one signal colour (vermilion) over bone paper and blue-black ink, and exactly
one sealed inverse band — the report boundary — where the page turns dark. The
fixed margin rail marks the current chapter from 84rem upward. No gradient,
blur, glow, shadow or rounded corner remains outside the rhythm dial, which is
the single deliberately circular figure.

Typography carries the hierarchy: Newsreader for display statements and
figures, IBM Plex Sans for prose, IBM Plex Mono for labels, ledger rows and
readout values. The three faces are the unmodified Google Fonts `latin` and
`latin-ext` subsets, bundled under the SIL Open Font License 1.1
(`web/fonts/OFL.txt`, `web/fonts/README.md`), embedded in the binary and served
from loopback only; `web/embed_test.go` now fails if a stylesheet font is not
embedded or if the licence is missing, and `scripts/verify-packages.py` proves
every webfont, the licence and the three UI files are byte-identical inside each
released executable.

### Findings, classified before any change

Review of the previous candidate produced four claims and two suspicions. Each
was re-checked against a rendered page before editing; only confirmed items were
changed.

| Claim | Verdict | Evidence |
| --- | --- | --- |
| Method disclosure summaries strand the expand marker in an empty column | Confirmed | `+`/`−` rendered at the far left with the label flush to the trailing edge at 1440 |
| Hero session figure clips its own content box | Confirmed | `#hero-session-count` `clientHeight` 89 against `scrollHeight` 96 at 1280 DPR 2 (`keyValues` check) |
| Weekday rows overflow with a heavy distribution | Confirmed | A forced synthetic distribution made `documentElement.scrollWidth` 399 against a 390 layout viewport; the value and label tracks were the cause |
| Long identifiers in warning prose need inline code styling | Not reproduced | No warning message in the retained report contains a dotted identifier; every identifier appears in the mono meta line, and `message.usage` appears only in the mono token-ledger note |
| Rhythm clock bars read as too thin | Not reproduced | Dial captured at 1440 DPR 2; hairlines are the intended instrument treatment and remain legible |
| Margin rail collides with the hero band | Not reproduced | Rail occupies the left margin only (x 46–142) while content starts at x 184; no overlap at 1440, 1728 or 1920 |

### Local gates on the exact head

| Gate | Result |
| --- | --- |
| `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l .` | All packages pass; no vet or format findings |
| `node --test web/source_activity_test.cjs web/usage_chart_test.cjs` | **32/32** pass |
| `python3 scripts/release_workflow_test.py` | **21/21** pass |
| `node scripts/verify-install.cjs` (installed binary, synthetic sources) | **30/30** checks, including the served bundled webfont |
| `./scripts/verify-runtime-offline.sh` | Generation and loopback viewing with zero observed external connect attempts |
| `node scripts/verify-browser.cjs --revision true` | **1,625/1,625** assertions, 61 screenshots, pass |

The browser run served the **production handler** (`scripts/serve-report`) from
the retained aggregate `c1a77d32…`, so the evidence covers the embedded assets
rather than a static preview. Chrome Headless 152.0.7977.84 ran inside the
verification Seatbelt policy with CDP interception from before navigation. Six
viewports (1280, 1440, 1728, 1920, 320, 390 CSS px) reported zero page overflow,
zero uncontained block overflow and zero clipped key values; session counts stay
in exact bars and prompt counts in circles whose 12px-plus labels fit their
marks. All 133 requests went to the loopback origin except the deliberately
denied external control, 68 of them for the bundled webfonts, and the ledger
recorded no application exception, no uninstrumented product child target and
no browser-internal target. The eight synthetic states — one entity, two
entities, one recorded session, three single-session harnesses, a three-entity
tiny minority, a 40-entity long tail, sessions without positive prompts, and the
loading/error/empty/retry reduced-motion sequence — each held the intended
390x844 DPR-2 touch viewport.

### Verification harness repairs

Two harness defects surfaced while re-running the browser record; both were
reproduced on the pre-redesign interface as well, so neither was a property of
the new page.

- A single `Page.captureScreenshot` that materialised the whole report at 2x
  closed the DevTools socket mid-capture, always on the widest full-page shot the
  run had reached. Full-page DPR-2 evidence is now captured in vertical tiles
  (`rewind-<width>-dpr2-full-NN.png`) bounded by a device-pixel and device-height
  budget. The tiles hold the same 1:1 device pixels; nothing is resampled and no
  region is skipped.
- Chrome's built-in `Google Network Speech` component extension starts a
  background service worker in any profile because `--disable-extensions` does
  not remove component extensions. The verifier now passes
  `--disable-component-extensions-with-background-pages` and classifies any
  remaining `chrome://`/`chrome-extension://`/`devtools://` child target into
  `browser_internal_targets`, leaving every http(s), blob:, data: and about:
  child target inside the strict product scope check.
- The same runs required a narrower Seatbelt allowance so headless Chrome can
  bind its own per-profile singleton unix socket; all remote socket classes
  remain denied by the existing `deny network*` rule.
