#!/bin/bash
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license that can be found in
# the LICENSE file.
# Set up a Windows runner to build rmctl with rendering.
#
# Usage: setup-render-windows.sh
#
# rmrender-go draws with Cairo, Pango and PDFium on Windows, built as
# rmrender-lib builds it there: MSYS2 CLANG64's clang is the C23 compiler and
# its pkg-config finds the Cairo stack. The job installs those with
# msys2/setup-msys2 (msystem: CLANG64, path-type: inherit, so that the Go
# setup-go installed is found after CLANG64's tools) and runs this in its
# msys2 shell. This fetches the PDFium win-x64 SDK rmrender-lib pins (its
# fetch-pdfium.py, vendored in rmrender-go, checks the checksum) and writes
# CC, CGO_ENABLED, CGO_CFLAGS, CGO_LDFLAGS, PDFIUM_DIR and PDFIUM_BIN to
# $GITHUB_ENV for the steps after it.
#
# cgo, clang and Go are native Windows programs, so the paths they get are in
# mixed form (C:/...), as cygpath -m writes them. PDFIUM_BIN, which holds
# pdfium.dll, is in MSYS2 form, for a step to put on PATH before it runs a
# program built with the renderer; CLANG64's bin, with the Cairo stack's
# DLLs, is on PATH already.
set -euo pipefail

[[ "${MSYSTEM:-}" == CLANG64 ]] || { printf 'run this in an MSYS2 CLANG64 shell, not %s\n' "${MSYSTEM:-none}" >&2; exit 1; }
for tool in clang pkg-config python go; do
    command -v "$tool" >/dev/null || { printf '%s is not on PATH\n' "$tool" >&2; exit 1; }
done
clang --version | head -1
go version

go mod download github.com/altessa-s/rmrender-go
module=$(cygpath -m "$(go list -m -f '{{.Dir}}' github.com/altessa-s/rmrender-go)")
dir="$(cygpath -m "${RUNNER_TEMP:?}")/pdfium"
python "$module/internal/clib/rmrender/scripts/fetch-pdfium.py" --platform win-x64 --output "$dir"

{
    echo "CC=clang"
    echo "CGO_ENABLED=1"
    echo "CGO_CFLAGS=-O2 -g -I$dir/include"
    echo "CGO_LDFLAGS=-L$dir/lib"
    echo "PDFIUM_DIR=$dir"
    echo "PDFIUM_BIN=$(cygpath -u "$dir")/bin"
} >> "${GITHUB_ENV:?}"
