#!/bin/bash
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license that can be found in
# the LICENSE file.
# Set up a Linux runner to build rmctl with rendering.
#
# Usage: setup-render-linux.sh [RPATH]
#
# rmrender-go draws with Cairo, Pango and PDFium on Linux and is C23 that
# needs clang 19 or newer. This installs clang 19 and the Cairo stack's
# development packages, fetches the PDFium build rmrender-lib pins (its
# fetch-pdfium.py, vendored in rmrender-go, checks the checksum; with
# PDFIUM_CACHE set, fetch-pdfium.sh keeps it there), and writes
# CC, CGO_CFLAGS, CGO_LDFLAGS and PDFIUM_DIR to $GITHUB_ENV for the steps
# after it. RPATH is the run path the binary finds libpdfium.so on: the
# default is PDFium's own directory, for tests run in place; a release
# passes '$ORIGIN', the binary's own directory, where its archive puts the
# library.
set -euo pipefail

rpath=${1:-}
case "$(uname -m)" in
    x86_64) platform=linux-x64 ;;
    aarch64 | arm64) platform=linux-arm64 ;;
    *) printf 'no PDFium build for %s\n' "$(uname -m)" >&2; exit 1 ;;
esac

sudo=""
[[ "$(id -u)" == 0 ]] || sudo=sudo
$sudo apt-get update -q
$sudo apt-get install -y -q --no-install-recommends clang-19 pkg-config python3 \
    libcairo2-dev libpango1.0-dev libharfbuzz-dev libgdk-pixbuf-2.0-dev libfontconfig1-dev zlib1g-dev

go mod download github.com/altessa-s/rmrender-go
module=$(go list -m -f '{{.Dir}}' github.com/altessa-s/rmrender-go)
dir=$(bash "$(dirname "$0")/fetch-pdfium.sh" python3 "$module/internal/clib/rmrender/scripts/fetch-pdfium.py" \
    "$platform" "${RUNNER_TEMP:?}/pdfium")
[[ -n "$rpath" ]] || rpath="$dir/lib"

{
    echo "CC=clang-19"
    echo "CGO_CFLAGS=-O2 -g -I$dir/include"
    echo "CGO_LDFLAGS=-L$dir/lib -Wl,-rpath,$rpath"
    echo "PDFIUM_DIR=$dir"
} >> "${GITHUB_ENV:?}"
