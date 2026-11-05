#!/bin/bash
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license that can be found in
# the LICENSE file.
# Build, sign and notarize the macOS archives of a release on a Mac, add them
# to the GitHub release and start the Homebrew workflow.
#
# Usage: release-macos.sh TAG
#
# TAG is a pushed tag such as v1.0.0; its release workflow must have
# published the release. The tag is checked out into a temporary worktree and
# built for arm64 and amd64 with cgo, as CI builds the other platforms. Each
# binary is signed with the Developer ID Application certificate in the login
# keychain, with the hardened runtime and a secure timestamp, and both are
# notarized in one submission with the notarytool keychain profile
# NOTARY_PROFILE (default rmctl-notary; create it once with `xcrun notarytool
# store-credentials`). A bare binary cannot be stapled: Gatekeeper finds its
# ticket online. The binary of this Mac's architecture renders a sample
# before anything is uploaded. The archives replace any of the same name in
# the release, and their lines replace the macOS lines of checksums.txt.
# UPLOAD=0 stops after packing and leaves the archives in ./dist.
set -euo pipefail

tag=${1:?tag, such as v1.0.0}
[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || { printf 'not a release tag: %s\n' "$tag" >&2; exit 2; }
version=${tag#v}
repo=altessa-s/rmctl
identity="Developer ID Application: Altessa Solutions Inc (SZA4GU6AL2)"
profile=${NOTARY_PROFILE:-rmctl-notary}
upload=${UPLOAD:-1}
root=$(git rev-parse --show-toplevel)
dist="$root/dist"
work=$(mktemp -d)
tree="$work/src"

cleanup() {
    git -C "$root" worktree remove --force "$tree" 2>/dev/null || true
    rm -rf "$work"
}
trap cleanup EXIT

[[ "$(uname -s)" == Darwin ]] || { echo "run this on a Mac" >&2; exit 1; }
security find-identity -v -p codesigning | grep -qF "$identity" || { printf 'no signing identity %s\n' "$identity" >&2; exit 1; }
if [[ "$upload" == 1 ]]; then
    gh release view "$tag" --repo "$repo" >/dev/null || { printf 'no release %s yet: wait for its release workflow\n' "$tag" >&2; exit 1; }
fi

git -C "$root" fetch --quiet origin "refs/tags/$tag:refs/tags/$tag"
git -C "$root" worktree add --quiet --detach "$tree" "$tag"
commit=$(git -C "$tree" rev-parse --short=7 HEAD)
built=$(date -u +%Y-%m-%dT%H:%M:%SZ)
pkg="github.com/altessa-s/go-atlas-core/runtime/appinfo"
fonts=""
if renderer=$(go -C "$tree" list -m -f '{{.Dir}}' github.com/altessa-s/rmrender-go 2>/dev/null); then
    fonts="$renderer/fonts"
fi

mkdir -p "$work/notarize"
for arch in arm64 amd64; do
    out="$work/$arch"
    mkdir -p "$out"
    cc=clang
    [[ "$arch" == amd64 ]] && cc="clang -arch x86_64"
    printf 'building darwin/%s\n' "$arch"
    CGO_ENABLED=1 GOOS=darwin GOARCH="$arch" CC="$cc" go -C "$tree" build -trimpath -o "$out/rmctl" \
        -ldflags "-s -w -X ${pkg}.Name=rmctl -X ${pkg}.Project=rmctl -X ${pkg}.Version=${version} \
        -X ${pkg}.Commit=${commit} -X ${pkg}.BuildTime=${built}" ./cmd/rmctl
    cp "$tree/LICENSE" "$tree/README.md" "$out/"
    if [[ -n "$fonts" ]]; then
        # The renderer's fonts are embedded in the binary, under the SIL Open Font License.
        mkdir -p "$out/licenses/fonts"
        cp "$fonts"/*-OFL.txt "$out/licenses/fonts/"
    fi
    codesign --force --options runtime --timestamp --identifier io.altessa.rmctl --sign "$identity" "$out/rmctl"
    codesign --verify --strict --verbose=2 "$out/rmctl"
    cp "$out/rmctl" "$work/notarize/rmctl-$arch"
done

native=amd64
[[ "$(uname -m)" == arm64 ]] && native=arm64
bin="$work/$native/rmctl"
[[ "$("$bin" version)" == "$version" ]] || { echo "the binary reports another version" >&2; exit 1; }
if [[ -n "$fonts" ]]; then
    sample="$tree/internal/adapter/renderer/testdata/Notes.rmdoc"
    "$bin" render "$sample" -o "$work/smoke.pdf" >/dev/null
    "$bin" render "$sample" --page 1 -o "$work/smoke.png" >/dev/null
fi

echo "notarizing"
ditto -c -k "$work/notarize" "$work/notarize.zip"
result=$(xcrun notarytool submit "$work/notarize.zip" --keychain-profile "$profile" --wait --timeout 30m --output-format json)
status=$(jq -r .status <<<"$result")
if [[ "$status" != Accepted ]]; then
    xcrun notarytool log "$(jq -r .id <<<"$result")" --keychain-profile "$profile" || true
    printf 'notarization ended %s\n' "$status" >&2
    exit 1
fi

mkdir -p "$dist"
archives=()
for arch in arm64 amd64; do
    archive="$dist/rmctl_${version}_darwin_${arch}.zip"
    rm -f "$archive"
    ditto -c -k "$work/$arch" "$archive"
    archives+=("$archive")
done
(cd "$dist" && shasum -a 256 rmctl_"${version}"_darwin_*.zip)

if [[ "$upload" != 1 ]]; then
    printf 'packed in %s; nothing uploaded\n' "$dist"
    exit 0
fi

gh release download "$tag" --repo "$repo" --pattern checksums.txt --dir "$work" --clobber
grep -v "_darwin_" "$work/checksums.txt" >"$work/checksums.new" || true
(cd "$dist" && shasum -a 256 rmctl_"${version}"_darwin_*.zip) >>"$work/checksums.new"
mv "$work/checksums.new" "$work/checksums.txt"
gh release upload "$tag" --repo "$repo" --clobber "${archives[@]}" "$work/checksums.txt"
# A pre-release leaves the formula alone, and GitHub only dispatches a
# workflow the default branch has, which it has once a release reached main.
if [[ "$version" == *-* ]]; then
    printf 'uploaded the macOS archives of %s; a pre-release leaves the Homebrew formula alone\n' "$tag"
    exit 0
fi
gh workflow run homebrew.yml --repo "$repo" --ref "$tag" -f tag="$tag"
printf 'uploaded the macOS archives of %s and started the Homebrew workflow\n' "$tag"
