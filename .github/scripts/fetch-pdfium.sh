#!/bin/bash
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license that can be found in
# the LICENSE file.
# Fetch the PDFium SDK rmrender-lib pins, through a cache when there is one.
#
# Usage: fetch-pdfium.sh PYTHON FETCH_SCRIPT PLATFORM FALLBACK_DIR
#
# Prints the directory that holds the SDK. FETCH_SCRIPT is the
# fetch-pdfium.py vendored in rmrender-go, which downloads the SDK and checks
# its checksum. With PDFIUM_CACHE set (a directory actions/cache keeps), the
# SDK is kept there under its checksum, so a cache restored for another pin
# is never used, and a hit downloads nothing; without it, the SDK goes to
# FALLBACK_DIR. GitHub's release downloads fail now and then with a 5xx, so a
# download is tried five times, a minute apart at most.
set -euo pipefail

python=$1 script=$2 platform=$3 fallback=$4

sum=$(sed -n "s/^ *\"$platform\": \"\([0-9a-f]\{64\}\)\",*$/\1/p" "$script")
[[ -n "$sum" ]] || { printf 'no checksum for %s in %s\n' "$platform" "$script" >&2; exit 1; }

dir=$fallback
if [[ -n "${PDFIUM_CACHE:-}" ]]; then
    dir="$PDFIUM_CACHE/$platform-${sum:0:16}"
    if [[ -f "$dir/.complete" ]]; then
        printf 'PDFium %s from the cache\n' "$platform" >&2
        printf '%s\n' "$dir"
        exit 0
    fi
fi

for attempt in 1 2 3 4 5; do
    rm -rf "$dir"
    if "$python" "$script" --platform "$platform" --output "$dir" >&2; then
        touch "$dir/.complete"
        printf '%s\n' "$dir"
        exit 0
    fi
    [[ $attempt == 5 ]] && break
    delay=$((15 * attempt))
    printf 'PDFium download failed (attempt %d of 5), retrying in %ds\n' "$attempt" "$delay" >&2
    sleep "$delay"
done
printf 'PDFium %s could not be downloaded\n' "$platform" >&2
exit 1
