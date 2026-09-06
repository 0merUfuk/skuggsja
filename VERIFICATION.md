# Verification

This is the public verification summary. Complete original audit records, source manifests, reports, screenshots and scanner logs are retained privately and were preserved byte-for-byte before this summary replaced them. They are not distributable product assets.

**The first table preserves the completed UI revision baseline. The subsequent Show more investigation and final UI results are recorded below under “Disclosure interaction corrections.” Backend and release-readiness changes require their own verification; the baseline alone does not certify them.**

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

CI now reuses macOS/Linux/Windows test, build and installed-binary checks for releases, with a six-platform package gate, vulnerability checks and workflow linting. Third-party actions are pinned to verified full commit hashes, and the release tool and Node versions are explicit. Publication permissions are confined to the final job. The prepared ordinary Homebrew formula uses prebuilt macOS/Linux binaries with no Go, Python or Node runtime dependency; it is not an unsigned cask and does not remove quarantine metadata. A separately prepared tap workflow verifies the formula's attestation, workflow identity and exact stable tag before updating its own repository with its own automatic token. No personal access token is copied into Actions secrets. The tap updater passed 17 offline guard tests.

The final macOS synthetic runtime check completed generation and all localhost routes with **zero observed external connection attempts** under calibrated OS/network denial. An isolated CodeRabbit review covered seven release/configuration files; its Windows ARM64 host-name correction was implemented and covered by the package suite. Independent review also closed the wrong-target and ZIP special-file archive gaps.

The public GitHub repository has now been created with a description, topics, private vulnerability reporting, Dependabot security updates, secret scanning and push protection. Actions default to read permissions and require full-SHA action references. **Source synchronization, remote CI, release publication, attestation verification and the public Homebrew lifecycle remain pending at this checkpoint.** The local checks above do not establish those outcomes. Later release records must identify the actual commits, workflow runs and published artifacts.

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

A provider may degrade with a scoped warning after safe source resolution. Failure to establish source/output separation can stop generation before writes. Partial-history reconciliation can omit unique older prompts; missing detail is never reconstructed from recollection, filesystem times or aggregate estimates. A hard crash can leave a private temporary database copy. Linux/Windows real-store and Windows DACL runtime verification remain outstanding.

[FORENSIC-AUDIT.md](FORENSIC-AUDIT.md) documents generalizable discovery and deduplication findings. Raw histories, personal usage totals, absolute source paths, source/report fingerprints and private operational context remain outside the public record. Secret scanning is one check, not proof of anonymization.
