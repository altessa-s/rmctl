# GitHub Actions scripts

## Commit messages

`validate-commit-message.sh` reads a complete message from stdin. The local `commit-msg` hook and CI use this same validator. It checks
Conventional Commits (`type[(scope)][!]: description`), types, lowercase descriptions and scopes from [`commit_scopes.txt`](../../commit_scopes.txt).
Only the scope lines of that file count; comment and blank lines name no scope. The `root` scope is forbidden; omit the scope for repository-wide
changes. Breaking changes may use `!` before the colon.

Messages must describe the concrete change without filler, test results, coverage figures or validation summaries. Agent attribution, generation
signatures and agent co-author trailers are forbidden. The validator rejects common report headings and attribution patterns, matching agent names
as whole words only; reviewers check meaning and concision. Human co-author trailers and `BREAKING CHANGE:` explanations remain allowed.

`validate-commits.sh` validates the complete message of every commit in a range. All failures fail CI. The only exemption is the subject of a
merge git generates itself (`Merge branch|tag|remote-tracking branch '…'`, `Merge pull request #N from …`), as git flow merges releases and
hotfixes; its body is checked like any other. Automated and revert commits use a conventional subject: Dependabot writes `build(deps): bump …`
(see [`dependabot.yml`](../dependabot.yml)), which the `deps` scope covers, so it needs no exemption. PR titles use the same validator.

## Usage

```sh
# Enable the local hook.
git config core.hooksPath .githooks

# Exercise validation rules and the local hook without creating commits.
bash .github/scripts/test-commit-validation.sh

# Validate one message.
bash .github/scripts/validate-commit-message.sh < /tmp/commit-message

# Validate a specific range.
bash .github/scripts/validate-commits.sh origin/develop..HEAD
```

Without a range, local runs compare against the merge base with `origin/develop`. CI supplies `BASE_SHA` and `HEAD_SHA` for the event. A new branch
push without a base checks all commits reachable from the head. Missing references and invalid messages fail the check; history is never fetched
implicitly or replaced with a shorter range.

## Examples

```text
fix(browse): resolve an id prefix that names a folder
feat(write): move a document into the trash
build(deps): bump golang.org/x/sys from 0.48.0 to 0.49.0
build: pin the options generator
```

Exit status is zero only when every message is valid (or the selected range is empty).

## Benchmark regressions

`bench-regression.sh BASE_REF` checks the base out into a temporary worktree, runs every benchmark of both trees `BENCH_COUNT` times with
`-benchtime` `BENCH_TIME` and compares them with the `benchstat` pinned in [`devtools`](../../devtools). It fails when a benchmark shows a
statistically significant increase in `sec/op` or `allocs/op` above `BENCH_THRESHOLD` percent; changes benchstat marks as insignificant (`~`) and
benchmarks present on one side only never fail. `BENCH_REPORT` names a file that also receives the report. The defaults of these variables are set
and documented in the script's header. The [`bench.yml`](../workflows/bench.yml) workflow runs it on pull requests against the base branch; locally
use `make bench-compare BASE=<ref>`.

## Coverage gate

`coverage-gate.sh [PROFILE]` reads the threshold and the package list from [`.coverage.yml`](../../.coverage.yml) and fails when any listed
package's statement-weighted coverage in `PROFILE` (default `coverage.out`) is below it. Subpackages are gated on their own entries; a package
with no statements yet is reported as skipped. CI runs it on the profile `make coverage` writes; locally use `make coverage-gate`.

## Rendering on Linux

`setup-render-linux.sh [RPATH]` prepares a Linux runner to build rmctl with rendering: it installs clang 19, which rmrender-go's C23 needs,
and the Cairo stack's development packages, fetches the PDFium build rmrender-lib pins with the `fetch-pdfium.py` vendored in rmrender-go
(checksum-verified), and writes `CC`, `CGO_CFLAGS`, `CGO_LDFLAGS` and `PDFIUM_DIR` to `$GITHUB_ENV`. `RPATH` is the run path
the binary finds `libpdfium.so` on: PDFium's own directory by default, for tests run in place; the release passes `'$ORIGIN'` and puts the
library beside the binary in its archive. Every Linux job that compiles the module with cgo runs it after setting up Go.

## macOS release

The release workflow builds and publishes the Linux and Windows archives. The macOS archives are built on a Mac, so they carry the Developer ID signature of Altessa Solutions Inc without the certificate leaving its keychain: once the release is published, `release-macos.sh TAG` checks the tag out into a temporary worktree, builds `darwin/arm64` and `darwin/amd64` with cgo, signs each binary with the hardened runtime and a secure timestamp, notarizes both in one submission, renders a sample with the binary of the Mac's own architecture, packs `rmctl_<version>_darwin_<arch>.zip` with `ditto`, uploads them to the release, puts their lines into `checksums.txt` and starts [`homebrew.yml`](../workflows/homebrew.yml), which writes the formula from the release's macOS and Linux archives. A bare binary cannot be stapled, so Gatekeeper looks its ticket up online. `UPLOAD=0` stops after packing and leaves the archives in `dist/`.

It needs the certificate in the login keychain, Go, Apple's clang, `gh` signed in with write access to the repository, `jq`, and a notarytool profile, `rmctl-notary` unless `NOTARY_PROFILE` names another, stored once:

```sh
xcrun notarytool store-credentials rmctl-notary --apple-id <apple-id> --team-id SZA4GU6AL2
```

`make release-macos TAG=v1.2.0` runs it.

## Windows package managers

After a release (not a pre-release) is published, the release workflow updates two package managers from its Windows archive. A job whose token is not set warns and does nothing.

- **Scoop.** `scoop-manifest.sh VERSION CHECKSUMS` writes `rmctl.json` from the release's `checksums.txt`; the `scoop` job commits it as `bucket/rmctl.json` to [altessa-s/scoop-bucket](https://github.com/altessa-s/scoop-bucket). It needs `SCOOP_BUCKET_TOKEN`, a token with contents: write on that repository. Users install with `scoop bucket add altessa-s https://github.com/altessa-s/scoop-bucket` and `scoop install rmctl`.
- **winget.** The `winget` job runs [winget-releaser](https://github.com/vedantmgoyal9/winget-releaser), which opens a pull request to `microsoft/winget-pkgs` for `Altessa.rmctl` from the fork of that repository under altessa-s. It needs `WINGET_TOKEN`, a classic token with the `public_repo` and `workflow` scopes, and it only updates a package already there: the first version is submitted once by hand, for example with [Komac](https://github.com/russellbanks/Komac) (`komac new Altessa.rmctl --version <version> --urls <url of rmctl_<version>_windows_amd64.zip> --submit`, a zip with `rmctl.exe` as its nested portable installer, command `rmctl`). Microsoft reviews every pull request before `winget install Altessa.rmctl` sees the version.

The Windows binary is not signed yet, so SmartScreen may warn when it is started from Explorer; installed by either manager and run from a terminal, it is not.
