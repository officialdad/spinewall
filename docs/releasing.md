# Releasing

Releases are automatic. The `release` workflow runs `release-please` on every push to `main`.

## Flow

1. Merge pull requests with Conventional Commit titles into `main`.
2. The workflow opens or updates a release PR. It holds the new version, `version.txt`, and `CHANGELOG.md`.
3. Merge the release PR. The workflow creates the tag `vX.Y.Z` and a GitHub Release.
4. The workflow builds `spinewall-X.Y.Z.tar.gz` with `git archive`, plus `SHA256SUMS`, and attaches both. It stops if the archive holds a model or runtime file.

## Version rules

Versions follow [semantic versioning](https://semver.org/). Before 1.0.0:

| Commit | Bump |
|---|---|
| `fix:`, `perf:` | Patch |
| `feat:` | Minor |
| `feat!:` or `BREAKING CHANGE:` | Minor, because of `bump-minor-pre-major` |

Set `bump-patch-for-minor-pre-major` to `true` in `release-please-config.json` if you want `feat:` to bump the patch version before 1.0.0.

## First release: 0.1.0

The early commits do not use Conventional Commits, so the tool finds nothing to release. `release-as: "0.1.0"` in `release-please-config.json` forces the first release to be exactly 0.1.0. The manifest starts at `0.0.0`.

One-time steps after the 0.1.0 release PR merges:

1. Remove the `release-as` line from `release-please-config.json`. A left-over line pins every later release to 0.1.0.
2. Merge that change with a `chore:` title.

## One-time GitHub settings

1. Open Settings, Actions, General. Allow GitHub Actions to create and approve pull requests. Set workflow permissions to read and write.
2. A PR made with `GITHUB_TOKEN` does not start other workflows. Close and reopen the release PR to run CI on it, or give the workflow a personal access token.
3. Protect `main`: require a pull request, require the `ci` and `pr-title` checks, block force pushes.
4. Use squash merge, with the PR title as the commit message. The title then drives the version.
5. Turn on private vulnerability reporting under Settings, Security.

## Install from a release

1. Download `spinewall-X.Y.Z.tar.gz` and `SHA256SUMS` from the release page.
2. Run `sha256sum -c SHA256SUMS`.
3. Extract the archive and run `scripts/install.sh` from it. The script needs no git checkout.
