#!/bin/bash
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license that can be found in
# the LICENSE file.
# Write the Scoop manifest for a release to stdout.
#
# Usage: scoop-manifest.sh VERSION CHECKSUMS
#
# VERSION is the release version without the leading "v"; CHECKSUMS is the
# release's checksums.txt. The manifest installs the release's Windows
# archive, which holds rmctl.exe at its top. checkver and autoupdate let
# Scoop's own tooling follow later releases, should the bucket's workflow
# ever fall behind.
set -euo pipefail

version=${1:?version}
checksums=${2:?checksums file}
file="rmctl_${version}_windows_amd64.zip"
base="https://github.com/altessa-s/rmctl/releases/download"

hash=$(awk -v f="$file" '$2 == f || $2 == "*" f { print $1 }' "$checksums")
[[ "$hash" =~ ^[0-9a-f]{64}$ ]] || { printf 'no checksum for %s\n' "$file" >&2; exit 1; }

cat <<EOF
{
    "version": "${version}",
    "description": "Command-line client for the reMarkable cloud",
    "homepage": "https://github.com/altessa-s/rmctl",
    "license": "MIT",
    "notes": "Rendering is not available in the Windows build.",
    "architecture": {
        "64bit": {
            "url": "${base}/v${version}/${file}",
            "hash": "${hash}"
        }
    },
    "bin": "rmctl.exe",
    "checkver": "github",
    "autoupdate": {
        "architecture": {
            "64bit": {
                "url": "${base}/v\$version/rmctl_\$version_windows_amd64.zip"
            }
        },
        "hash": {
            "url": "\$baseurl/checksums.txt"
        }
    }
}
EOF
