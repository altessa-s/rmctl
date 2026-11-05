#!/bin/bash
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license that can be found in
# the LICENSE file.
# Set up a Windows runner to build rmctl with rendering.
#
# Usage: setup-render-windows.sh [PLATFORM]
#
# rmrender-go draws with Cairo, Pango and PDFium on Windows, built as
# rmrender-lib builds it there: MSYS2's clang is the C23 compiler and its
# pkg-config finds the Cairo stack, CLANG64 on x64 and CLANGARM64 on ARM64.
# The job installs those with msys2/setup-msys2 (msystem: CLANG64 or
# CLANGARM64, path-type: inherit, so that the Go setup-go installed is found
# after the toolchain's tools) and runs this in its msys2 shell. This fetches
# the PDFium SDK rmrender-lib pins for PLATFORM (win-x64 or win-arm64; by
# default the one of the MSYS2 environment: win-x64 for CLANG64, win-arm64
# for CLANGARM64) with its fetch-pdfium.py, vendored in rmrender-go, which
# checks the checksum, and writes
# CC, CGO_ENABLED, CGO_CFLAGS, CGO_LDFLAGS, PDFIUM_DIR and PDFIUM_BIN to
# $GITHUB_ENV for the steps after it.
#
# cgo, clang and Go are native Windows programs, so the paths they get are in
# mixed form (C:/...), as cygpath -m writes them. PDFIUM_BIN, which holds
# pdfium.dll, is in MSYS2 form, for a step to put on PATH before it runs a
# program built with the renderer; the toolchain's bin, with the Cairo
# stack's DLLs, is on PATH already.
set -euo pipefail

case "${MSYSTEM:-}" in
    CLANG64) platform=win-x64 ;;
    CLANGARM64) platform=win-arm64 ;;
    *) printf 'run this in an MSYS2 CLANG64 or CLANGARM64 shell, not %s\n' "${MSYSTEM:-none}" >&2; exit 1 ;;
esac
platform=${1:-$platform}
case "$platform" in
    win-x64 | win-arm64) ;;
    *) printf 'no PDFium SDK for %s: win-x64 or win-arm64\n' "$platform" >&2; exit 1 ;;
esac
for tool in clang pkg-config python go; do
    command -v "$tool" >/dev/null || { printf '%s is not on PATH\n' "$tool" >&2; exit 1; }
done
clang --version | head -1
go version

go mod download github.com/altessa-s/rmrender-go
module=$(cygpath -m "$(go list -m -f '{{.Dir}}' github.com/altessa-s/rmrender-go)")
dir="$(cygpath -m "${RUNNER_TEMP:?}")/pdfium"
python "$module/internal/clib/rmrender/scripts/fetch-pdfium.py" --platform "$platform" --output "$dir"

{
    echo "CC=clang"
    echo "CGO_ENABLED=1"
    echo "CGO_CFLAGS=-O2 -g -I$dir/include"
    echo "CGO_LDFLAGS=-L$dir/lib"
    echo "PDFIUM_DIR=$dir"
    echo "PDFIUM_BIN=$(cygpath -u "$dir")/bin"
} >> "${GITHUB_ENV:?}"
