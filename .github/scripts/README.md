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

## Fetching PDFium

Both setup scripts fetch PDFium through `fetch-pdfium.sh PYTHON FETCH_SCRIPT PLATFORM FALLBACK_DIR`, which prints the SDK's directory.
GitHub's release downloads fail now and then with a 5xx, so it tries a download five times, waiting 15, 30, 45 and 60 seconds between
attempts. With `PDFIUM_CACHE` set it keeps the SDK there under its checksum from `fetch-pdfium.py`, so a cache restored for another pin is
never used, and a hit downloads nothing. CI, CodeQL, bench and fuzz restore `$RUNNER_TEMP/pdfium-cache` with `actions/cache/restore`
before the setup step and save it with `actions/cache/save` right after it, so a run whose tests fail still keeps the SDK. The release
fetches afresh, without the cache.

## Rendering on Windows

`setup-render-windows.sh [PLATFORM]` prepares a Windows runner to build rmctl with rendering, as rmrender-go's Windows CI builds it, on x64
(`windows-2022`, MSYS2 CLANG64) and on ARM64 (`windows-11-arm`, CLANGARM64, whose packages are `mingw-w64-clang-aarch64-*`; read CLANG64 as
CLANGARM64 below there). The job first installs MSYS2 CLANG64 with `msys2/setup-msys2` (`msystem: CLANG64`, `path-type: inherit`, so the Go of `setup-go` is found after CLANG64's
tools) and the packages rmrender-go's README lists: clang, pkgconf, python, cairo, pango, fontconfig, harfbuzz, gdk-pixbuf2, zlib and
ntldd. The script runs in that `msys2` shell: it fetches the PDFium SDK rmrender-lib pins for `PLATFORM`, by default the environment's (`win-x64` for CLANG64, `win-arm64` for CLANGARM64), with the `fetch-pdfium.py` vendored in
rmrender-go (checksum-verified) and writes `CC=clang`, `CGO_ENABLED`, `CGO_CFLAGS`, `CGO_LDFLAGS` (in `cygpath -m` form, as the native clang
and Go want them), `PDFIUM_DIR` and `PDFIUM_BIN` to `$GITHUB_ENV`. A step that runs a program built with the renderer puts `$PDFIUM_BIN`, with
`pdfium.dll`, on `PATH` first; CLANG64's `bin` is there already. CI's Windows job, on both architectures, vets every package with cgo and runs the renderer's tests and
the end-to-end render tests, with the race detector on x64 only (Go has none on windows/arm64); the release builds
`rmctl_<version>_windows_amd64.zip` and `rmctl_<version>_windows_arm64.zip` the same way. `bundle-windows-dlls.sh` names the licence
directories after the package without `$MINGW_PACKAGE_PREFIX`, which the MSYS2 shell sets for its environment.

`bundle-windows-dlls.sh EXE` makes the Windows archive run without MSYS2. A Windows rmctl loads PDFium and the Cairo stack as DLLs, so it
cannot be linked statically; the script copies every DLL `ntldd -R` resolves under CLANG64's `bin` or PDFium's `bin` beside `EXE` and fails on
one found anywhere else or not at all (Windows' own DLLs and API sets stay out). Each CLANG64 DLL's package (`pacman -Qo`) gets
`licenses/msys2/<package>/` with the files it installs under `$MSYSTEM_PREFIX/share/licenses` and its `pacman -Qi` record as `PACKAGE.txt`;
a package with no licence file is warned about. PDFium's licences and the fonts' are copied by the release as on Linux.
`bundle-windows-dlls.sh --verify EXE` checks an unpacked archive: with `PATH` reduced to the Windows directory, every DLL must resolve beside
`EXE` or in Windows. The release's smoke test runs it on the zip unpacked into an empty directory, then runs `version --full` and renders the
sample to PDF and PNG with that `PATH`.

## rmctl-lite

rmctl-lite is rmctl built with `-tags norender`: the renderer adapter is its stub, so rmrender-go, its fonts, Cairo, Pango and PDFium are not linked, the binary is still named `rmctl`, `render` fails with exit code 1 naming the full build, and `version --full` says `render no`. The release builds it next to the full build and names its archives `rmctl-lite_<version>_<os>_<arch>`; `checksums.txt` covers both.

- **Linux** (`build-lite`): clang 19 alone, for rmlines-lib and rmdoc-lib, and a plain `go build -tags norender`; no PDFium, no `libpdfium.so`, no `$ORIGIN` run path, no font licences. The smoke test checks that `readelf -d` shows no run path and no library of the render stack.
- **Windows** (`build-windows-lite`, amd64 on `windows-2022` with MSYS2 CLANG64, arm64 on `windows-11-arm` with CLANGARM64): one static `rmctl.exe` linked with `-linkmode=external -extldflags=-static`, zipped with `LICENSE` and `README.md`. `check-windows-static.sh EXE` lists the DLLs `EXE` imports with `llvm-objdump -p` and fails on any that is neither an API set nor in the Windows `System32` directory; the smoke test runs it on the unpacked zip, then runs the binary with `PATH` reduced to the Windows directory.
- **macOS**: `release-macos.sh` builds, signs and notarizes it with the full build (below).

`smoke-lite.sh BIN SAMPLE` is every lite smoke test: `version --full` says `render no` (and `"render": false` in JSON), `text` reads the sample `.rmdoc`, and `render` exits 1 with nothing on standard output and the rmctl-lite message on standard error. With `SMOKE_PATH` set, `BIN` runs with that `PATH` alone. CI's `lite` job builds, vets, lints and tests every package with the tag on a runner with only clang 19 installed, checks that `go list -deps -tags norender ./cmd/rmctl` has no rmrender-go, and type-checks `windows/arm64` without cgo; `make rmctl-lite`, `build-lite`, `golangci-lite` and `test-lite` run the same locally.

## macOS release

The release workflow builds and publishes the Linux and Windows archives. The macOS archives are built on a Mac, so they carry the Developer ID signature of Altessa Solutions Inc without the certificate leaving its keychain: once the release is published, `release-macos.sh TAG` checks the tag out into a temporary worktree, builds `darwin/arm64` and `darwin/amd64` with cgo, each as rmctl and as rmctl-lite, signs each of the four binaries with the hardened runtime and a secure timestamp, notarizes them in one submission, renders a sample with the rmctl of the Mac's own architecture and runs `smoke-lite.sh` on its rmctl-lite, packs `rmctl_<version>_darwin_<arch>.zip` and `rmctl-lite_<version>_darwin_<arch>.zip` with `ditto`, uploads them to the release, puts their lines into `checksums.txt` and starts [`homebrew.yml`](../workflows/homebrew.yml), which writes both formulas from the release's macOS and Linux archives: `homebrew-formula.sh VERSION CHECKSUMS` writes `rmctl.rb` (class `Rmctl`) and `homebrew-formula.sh --lite VERSION CHECKSUMS` writes `rmctl-lite.rb` (class `RmctlLite`, no Linux dependencies); each `conflicts_with` the other, as both install `rmctl`, and both install the bash, zsh and fish completion scripts the binary writes (`generate_completions_from_executable`). A bare binary cannot be stapled, so Gatekeeper looks its ticket up online. `UPLOAD=0` stops after packing and leaves the archives in `dist/`.

It needs the certificate in the login keychain, Go, Apple's clang, `gh` signed in with write access to the repository, `jq`, and a notarytool profile, `rmctl-notary` unless `NOTARY_PROFILE` names another, stored once:

```sh
xcrun notarytool store-credentials rmctl-notary --apple-id <apple-id> --team-id SZA4GU6AL2
```

`make release-macos TAG=v1.2.0` runs it.

## Windows package managers

After a release (not a pre-release) is published, the release workflow updates two package managers from its Windows archives, rmctl's and rmctl-lite's. A job whose token is not set warns and does nothing.

- **Scoop.** `scoop-manifest.sh VERSION CHECKSUMS` writes `rmctl.json` from the release's `checksums.txt` (Scoop unpacks the whole zip into the app's folder and its `rmctl` shim starts `rmctl.exe` there, beside its DLLs), and `scoop-manifest.sh --lite VERSION CHECKSUMS` writes `rmctl-lite.json`; the `scoop` job commits both under `bucket/` to [altessa-s/scoop-bucket](https://github.com/altessa-s/scoop-bucket). A manifest has the `64bit` architecture, and `arm64` when `checksums.txt` lists the `windows_arm64` zip: rmctl-lite's always does, so its manifest has both; rmctl's has had that zip since the full build was built for Windows ARM64, so its manifest has both too (a `checksums.txt` without the line, as an older release's, still gets `64bit` alone). Both install `bin: rmctl.exe`, and Scoop has no conflicts field: with both installed, the later one's shim answers `rmctl`. It needs `SCOOP_BUCKET_TOKEN`, a token with contents: write on that repository. Users install with `scoop bucket add altessa-s https://github.com/altessa-s/scoop-bucket` and `scoop install rmctl` or `scoop install rmctl-lite`.
- **winget.** The `winget` job runs [winget-releaser](https://github.com/vedantmgoyal9/winget-releaser), which opens a pull request to `microsoft/winget-pkgs` for `Altessa.rmctl` from the fork of that repository under altessa-s. It needs `WINGET_TOKEN`, a classic token with the `public_repo` and `workflow` scopes, and it only updates a package already there: the first version is submitted once by hand, for example with [Komac](https://github.com/russellbanks/Komac) (`komac new Altessa.rmctl --version <version> --urls <url of rmctl_<version>_windows_amd64.zip> <url of rmctl_<version>_windows_arm64.zip> --submit`, each a zip with `rmctl.exe` as its nested portable installer, command `rmctl`). The zip holds the DLLs `rmctl.exe` loads beside it, so the installer manifest needs `ArchiveBinariesDependOnPath: true`: winget then adds the package's folder to `PATH` instead of linking `rmctl.exe` from its `Links` folder, through which Windows would look for the DLLs in the wrong directory (see the `ArchiveBinariesDependOnPath` field of the [installer manifest schema](https://github.com/microsoft/winget-pkgs/blob/master/doc/manifest/schema/1.12.0/installer.md)). winget-releaser bases each new version on the one already there, so the field must be set by hand, on every installer (x64 and ARM64), in the first manifest whose zips carry DLLs: the first submission, or the pull request of the first version with DLLs if an older version is already there; later versions inherit it. Microsoft reviews every pull request before `winget install Altessa.rmctl` sees the version. The step's `installers-regex` takes both `rmctl_<version>_windows_amd64.zip` and `rmctl_<version>_windows_arm64.zip`, and not rmctl-lite's.
- **winget, rmctl-lite.** A second winget-releaser step opens the pull request for `Altessa.rmctl.Lite` with both `rmctl-lite_<version>_windows_amd64.zip` and `rmctl-lite_<version>_windows_arm64.zip`. Like `Altessa.rmctl`, the package's first version is submitted once by hand, for example `komac new Altessa.rmctl.Lite --version <version> --urls <url of the amd64 zip> <url of the arm64 zip> --submit`, each a zip with `rmctl.exe` as its nested portable installer, command `rmctl`. Its `rmctl.exe` is static and loads no DLL beside it, so it does not need `ArchiveBinariesDependOnPath`. Both packages install the `rmctl` command, so install one of them.

The Windows binary is not signed yet, so SmartScreen may warn when it is started from Explorer; installed by either manager and run from a terminal, it is not.
