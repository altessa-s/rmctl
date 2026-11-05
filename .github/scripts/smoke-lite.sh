#!/bin/bash
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license that can be found in
# the LICENSE file.
# Check an rmctl-lite binary: the build without the renderer.
#
# Usage: smoke-lite.sh BIN SAMPLE
#
# BIN is the binary, named rmctl as in the full build; SAMPLE is an .rmdoc
# archive, in the form BIN reads paths (cygpath -w for a Windows binary).
# version --full must say render no, in text and in JSON; text must read
# SAMPLE; render must fail with exit code 1, as in any build that draws
# nothing, write nothing to standard output and name the full build on
# standard error. When SMOKE_PATH is set, BIN runs with PATH set to it alone:
# a Windows job sets it to the Windows directory, so the binary finds no
# library a development machine has.
set -euo pipefail

bin=${1:?binary}
sample=${2:?sample .rmdoc}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

run() {
    if [[ -n "${SMOKE_PATH:-}" ]]; then
        PATH="$SMOKE_PATH" "$bin" "$@"
    else
        "$bin" "$@"
    fi
}

fail() {
    printf '::error::%s\n' "$*" >&2
    exit 1
}

run version --full | tee "$work/version.txt"
grep -qx $'render\tno' "$work/version.txt" || fail "version --full does not say render no"
run version --full --json >"$work/version.json"
grep -q '"render": false' "$work/version.json" || fail "version --full --json does not say \"render\": false"

run text "$sample" >"$work/text.txt"
[[ -s "$work/text.txt" ]] || fail "text read nothing of $sample"

code=0
run render "$sample" -o lite-smoke.pdf >"$work/render.out" 2>"$work/render.err" || code=$?
cat "$work/render.err"
((code == 1)) || fail "render exited $code, not 1"
[[ ! -s "$work/render.out" ]] || fail "render wrote an answer"
grep -qF "rendering is not in rmctl-lite" "$work/render.err" || fail "render does not name rmctl-lite"
[[ ! -e lite-smoke.pdf ]] || fail "render wrote lite-smoke.pdf"
echo "rmctl-lite smoke test passed"
