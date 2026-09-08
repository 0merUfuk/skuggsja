# Releasing

Release from a reviewed commit using a canonical stable tag, `vMAJOR.MINOR.PATCH`. The source repository builds and attests release artifacts; the dedicated `0merUfuk/homebrew-skuggsja` tap prepares a verified formula PR. Publishing the source release and merging its tap PR are separate owner decisions.

## Before a release

Review the final commit and [CHANGELOG.md](CHANGELOG.md). Keep the release scope consistent with [VERIFICATION.md](VERIFICATION.md): browser QA, source-access guarantees, runtime networking and release equality are separate records. Existing real-data equality evidence does not need to be regenerated for presentation or packaging changes that leave its scope intact.

The default branch requires up-to-date CI checks, one approving review, dismissal of stale reviews and resolved conversations; force pushes and branch deletion are disabled. The owner intentionally retains administrator bypass (`enforce_admins=false`). This is a documented exception, not universal enforcement or approval to bypass a failed check. The active stable-tag ruleset forbids updates and deletion for `v*` tags with no bypass actors. Inspect the actual repository rules before relying on them, and keep required check names aligned with the CI matrix; workflow files do not configure branch protection. [VERIFICATION.md](VERIFICATION.md) records the observed settings.

Run the applicable local checks from [CONTRIBUTING.md](CONTRIBUTING.md), including:

```sh
goreleaser check
goreleaser release --snapshot --clean --skip=publish
python3 scripts/verify-packages.py dist
python3 scripts/release_workflow_test.py
```

These commands create and verify local packages; they do not publish. `--clean` replaces generated `dist/` contents. GoReleaser and build dependencies can use the network. Package verification compares the archived README, license and embedded UI with the current checkout and exercises the host's native executable using isolated synthetic sources.

Before creating a version tag, the owner finalizes the changelog and reviews the public files and Git history for private artifacts. A release tag selects the exact source commit to build. Use only canonical stable tags, `vMAJOR.MINOR.PATCH`; the package verifier and formula generator reject prerelease labels and malformed versions.

Confirm that GitHub's repository setting for immutable releases is enabled before authorizing a new tag. An administrator can inspect it with `gh api repos/0merUfuk/skuggsja/immutable-releases`; the response must report `enabled: true`. That endpoint requires administration access, which the release workflow's token deliberately does not have. Enabling the setting affects future releases; it does not make older releases immutable retroactively. Protected tags and release-asset immutability are separate controls. [GitHub's immutability settings](https://docs.github.com/en/code-security/how-tos/secure-your-supply-chain/establish-provenance-and-integrity/prevent-release-changes)

## Release workflow

Pushing a `v*` tag triggers [Release](.github/workflows/release.yml). Publication proceeds only after the reusable [CI](.github/workflows/ci.yml) jobs succeed:

1. Run the native test/build/install matrix, quality checks and six-platform snapshot validation.
2. Build versioned archives with GoReleaser using `--skip=publish`.
3. Verify that package metadata matches the stable tag and that all six archives have correct hashes, regular-file members, embedded assets, platform/build metadata and the clean tagged source revision.
4. Generate `skuggsja.rb` from the release checksums.
5. Attest the six archives, `checksums.txt` and formula with GitHub build provenance.
6. Create a new draft release for the already-existing tag and generated notes. An existing release, including a draft, stops this creation step.
7. Upload all eight assets without replacement. Verify the draft's tag, state, exact asset names, completed uploads and sizes; download the assets again and compare every SHA-256 with the attested local bytes.
8. Publish the complete draft and confirm that GitHub reports it as published and immutable.

The release job runs these validation commands before attestation and publication:

```sh
python3 scripts/verify-packages.py dist "$RELEASE_TAG"
node scripts/generate-homebrew.cjs "$RELEASE_TAG" dist/checksums.txt dist/skuggsja.rb
```

`RELEASE_TAG` is supplied by the tag-triggered workflow. The formula generator refuses to overwrite an existing output file; rebuild into a clean generated directory when repeating it. `.goreleaser.yml` does not publish a Homebrew cask or update another repository.

The publish job uses this repository's automatic `GITHUB_TOKEN` with `contents: write`, plus `id-token: write` and `attestations: write` for provenance. No personal access token or cross-repository tap secret is required. It publishes only from `0merUfuk/skuggsja`.

The draft remains unpublished if creation, upload or byte verification fails. The workflow never uses `--clobber`, deletes a release, moves a tag or silently resumes an existing draft. Inspect any failed draft before retrying. The owner may explicitly discard an incomplete unpublished draft and rerun the same reviewed tag; published assets must never be replaced. If publication succeeded but its confirmation failed, inspect that release before attempting recovery. The post-publication immutability check detects a missing protection; it cannot substitute for the administrator's pre-release settings check. [GitHub's recommended draft/upload/publish sequence](https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases)

After publication, download all eight release assets and verify each asset's provenance against the expected repository, release workflow and exact tag, rejecting self-hosted signer runs. Run the package verifier against those downloaded archives from a clean checkout of the tagged source. The archived README, license, embedded assets and source revision must match that tag; a newer working tree is not the verification reference. Record the native extracted executable's isolated installation result separately from six-target cross-compilation and package checks. A correction to a published executable requires a new version and tag; do not replace an existing stable artifact or move its tag.

## Homebrew synchronization

The tap workflow is `.github/workflows/update-skuggsja.yml` in `0merUfuk/homebrew-skuggsja`. It checks the latest stable public release hourly at minute **23 UTC** and supports manual dispatch with a `tag` input (empty selects the latest stable release). Scheduling may be delayed; publication does not promise an immediate Homebrew update.

Before preparing `Formula/skuggsja.rb`, the importer verifies the downloaded formula's GitHub attestation against `0merUfuk/skuggsja`, the release workflow, the exact tag reference, and GitHub-hosted runners. It rejects drafts, prereleases, malformed tags, missing or duplicate formula assets, rollbacks and changed formula contents for an already imported version. It reuses one candidate PR per version on `update-skuggsja-vMAJOR.MINOR.PATCH`; it does not push the formula to `main`.

The importer uses the tap's own automatic `GITHUB_TOKEN` with job-scoped `contents: write` and `pull-requests: write`. Enable Actions-created PRs in that repository's settings. Candidate tests use read-only permissions and receive no publishing secrets. Keep candidate formula evaluation and installed-binary execution out of the importer job: a Homebrew formula is executable Ruby. Source CI needs no PAT, tap writer or cross-repository dispatch secret.

GitHub puts workflows triggered by `GITHUB_TOKEN`-created or updated PRs into an approval-required state. The owner selects **Approve workflows to run**, then reviews the checked PR head and authorizes its merge after required checks pass. New commits require fresh checks and review. Approval to run CI is not approval to publish the formula, and the bot does not approve or merge its own PR. [GitHub token event behavior](https://docs.github.com/en/actions/concepts/security/github_token)

Candidate CI must test the exact PR checkout on macOS arm64/amd64 and Linux arm64/amd64, after checking the formula's provenance and bytes. Installing the remote default branch would test the previous public formula. Keep the candidate tap isolated and assert its formula hash and commit before Homebrew loads it. Required checks include formula style/audit, updater rejection cases, installed architecture/version, completions, `brew test`, isolated synthetic CLI use, a real previous-version upgrade, reinstall and uninstall with user-state preservation. An already-current upgrade is only a no-op check. Missing-release skips are not passing installation evidence.

The owner merges only the verified candidate head through the tap's branch rules. A failed candidate leaves the public formula unchanged. After merge, verify the public qualified installation path separately; premerge custom-remote tests do not establish that GitHub's public default branch serves the intended formula.

After publishing a stable release, an authorized maintainer can request synchronization immediately:

```sh
gh workflow run update-skuggsja.yml --repo 0merUfuk/homebrew-skuggsja --ref main -f tag="$RELEASE_TAG"
```

Then inspect the tap workflow result and exercise the published installation:

```sh
brew install 0merUfuk/skuggsja/skuggsja
skuggsja version
brew test 0merUfuk/skuggsja/skuggsja
```

Record install/update/removal results before claiming the public Homebrew lifecycle is verified. Confirm the release links, six archive downloads, checksums and attestations, and keep README installation instructions aligned with the available version.

## Tap migration evidence

A tap move does not require an application release or a Homebrew revision. Bootstrap the dedicated tap with the current attested formula unchanged and prove the new path first. Then remove the old tap's Skuggsja formula and updater, and add or retain `skuggsja → 0merUfuk/skuggsja` in `tap_migrations.json`. Keep unrelated Matrix formulas and migration metadata available. Historical archive contents are preserved even when their README names the former tap.

Test same-version migration before and after the destination is trusted, pinned installations, retained older kegs, and a real previous-version upgrade in disposable Homebrew CI. Inspect every installed keg's `INSTALL_RECEIPT.json` `source.tap`, the linked executable, completions, pin state and preserved user-data canaries. `brew info --json=v2` describes the resolved formula; its top-level `tap` is not proof that existing receipts migrated.

The real untrusted-destination test found that qualified reinstall can write the former tap back from Homebrew's cached `Tab`, even after building from the new formula. A second update without the original Git diff does not replay the migration. Preserve those negative results; do not substitute uninstall, a version bump or fabricated receipts for the required compatibility result.

The dedicated tap's `brew skuggsja-migrate` external command provides explicit metadata recovery. It checks the local mapping and committed destination formula, verifies eligible legacy binaries against the original attested release hashes, locks Skuggsja's Homebrew installation, backs up all affected receipts, and changes only their tap association. The default/`--check` leaves receipts and installed files unchanged; `--apply` performs individually atomic writes and reports any partial result honestly. It must preserve binaries, completions, pins and reports, handle idempotent retries, and reject concurrent changes or unknown legacy binaries. Local identity checks are not a claim that an offline command independently verified GitHub publication.

Candidate and public CI exercise this recovery on macOS/Linux arm64/amd64 in addition to the existing installation lifecycle. They include retained older kegs and a pin, network-denied helper execution, no-write checks, lock refusal, explicit application, idempotence and a qualified reinstall after receipt recovery. Formula trust and command trust remain separately scoped. The [README migration steps](README.md#existing-the-matrix-tap-installations) show the user flow. Homebrew can skip a pinned reinstall while returning zero, so inspect state instead of treating an exit code as proof of reinstallation. Do not silently unpin, uninstall first, use `brew migrate` for this unchanged-name move or force-untap The Matrix.
