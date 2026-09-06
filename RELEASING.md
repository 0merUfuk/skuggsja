# Releasing

The first release is intended to be **v0.1.0**. Publication and the public Homebrew lifecycle are still pending. This document describes the prepared automation; it is not evidence that a remote workflow or installation has succeeded.

## Before a release

Review the final commit and [CHANGELOG.md](CHANGELOG.md). Keep the release scope consistent with [VERIFICATION.md](VERIFICATION.md): browser QA, source-access guarantees, runtime networking and release equality are separate records. Existing real-data equality evidence does not need to be regenerated for presentation or packaging changes that leave its scope intact.

The repository's default-branch rules should require reviewed changes and the current CI checks, and prevent branch deletion and force pushes. Inspect the actual repository rules before relying on them; workflow files do not configure branch protection. This document does not claim that those remote rules are enabled.

Run the applicable local checks from [CONTRIBUTING.md](CONTRIBUTING.md), including:

```sh
goreleaser check
goreleaser release --snapshot --clean --skip=publish
python3 scripts/verify-packages.py dist
```

These commands create and verify local packages; they do not publish. `--clean` replaces generated `dist/` contents. GoReleaser and build dependencies can use the network. Package verification compares the archived README, license and embedded UI with the current checkout and exercises the host's native executable using isolated synthetic sources.

Before creating a version tag, the owner finalizes the changelog and reviews the public files and Git history for private artifacts. A release tag selects the exact source commit to build. Use only canonical stable tags, `vMAJOR.MINOR.PATCH`; the package verifier and formula generator reject prerelease labels and malformed versions.

## Release workflow

Pushing a `v*` tag triggers [Release](.github/workflows/release.yml). Publication proceeds only after the reusable [CI](.github/workflows/ci.yml) jobs succeed:

1. Run the three-OS test/build/install matrix, quality checks and six-platform snapshot validation.
2. Build versioned archives with GoReleaser using `--skip=publish`.
3. Verify that package metadata matches the stable tag and that all six archives have correct hashes, regular-file members, embedded assets, platform/build metadata and the clean tagged source revision.
4. Generate `skuggsja.rb` from the release checksums.
5. Attest the six archives, `checksums.txt` and formula with GitHub build provenance.
6. Create the GitHub release with the already-existing tag, generated notes and those verified artifacts.

The release job runs these validation commands before attestation and publication:

```sh
python3 scripts/verify-packages.py dist "$RELEASE_TAG"
node scripts/generate-homebrew.cjs "$RELEASE_TAG" dist/checksums.txt dist/skuggsja.rb
```

`RELEASE_TAG` is supplied by the tag-triggered workflow. The formula generator refuses to overwrite an existing output file; rebuild into a clean generated directory when repeating it. `.goreleaser.yml` does not publish a Homebrew cask or update another repository.

The publish job uses this repository's automatic `GITHUB_TOKEN` with `contents: write`, plus `id-token: write` and `attestations: write` for provenance. No personal access token or cross-repository tap secret is required. After a failure, inspect the failed stage and any existing release before retrying; the workflow does not silently replace an existing release.

## Homebrew synchronization

The prepared tap workflow is `.github/workflows/update-skuggsja.yml` in `0merUfuk/homebrew-thematrix`. It runs daily at **07:23 UTC** and supports manual dispatch without inputs. It reads Skuggsja's latest stable public release; an unavailable first release is a no-op.

Before copying `Formula/skuggsja.rb`, it verifies the downloaded formula's GitHub attestation against `0merUfuk/skuggsja`, the release workflow, the exact tag reference, and GitHub-hosted runners. It rejects drafts, prereleases, malformed tags, missing or duplicate formula assets, rollbacks and changed formula contents for an already-installed version. Only a verified changed formula is committed and pushed to the tap's `main` branch.

The tap job uses its own automatic `GITHUB_TOKEN` with `contents: write`; it does not use a PAT, a token from Skuggsja, or a cross-repository dispatch secret. The tap's branch rules must permit this narrowly scoped bot update. If the workflow is blocked by permissions or branch rules, resolve that configuration rather than bypassing attestation.

Once the workflow and upstream release are published, an authorized maintainer can request synchronization immediately:

```sh
gh workflow run update-skuggsja.yml --repo 0merUfuk/homebrew-thematrix --ref main
```

Then inspect the tap workflow result and exercise the published installation:

```sh
brew install 0merUfuk/thematrix/skuggsja
skuggsja version
brew test skuggsja
```

Record install/update/removal results before claiming the public Homebrew lifecycle is verified. Confirm the release links, six archive downloads, checksums and attestations before changing README's publication status.
