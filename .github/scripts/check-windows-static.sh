#!/bin/bash
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license that can be found in
# the LICENSE file.
# Check that a Windows rmctl.exe imports only Windows' own DLLs.
#
# Usage: check-windows-static.sh EXE
#
# rmctl-lite on Windows is one static executable: the MinGW-w64 runtime and
# the C libraries are linked in, so it runs without MSYS2 and without a DLL
# beside it. llvm-objdump -p lists the DLLs EXE imports; each must be an API
# set (api-ms-*, ext-ms-*, which Windows maps itself) or a DLL of the Windows
# System32 directory. A static executable imports nothing else, so the DLLs
# those load are Windows' too. Runs in an MSYS2 shell whose clang toolchain
# has llvm-objdump; the imported DLLs are printed.
set -euo pipefail

exe=${1:?exe}
[[ -f "$exe" ]] || { printf '%s: no such file\n' "$exe" >&2; exit 1; }
system32="$(cygpath -u "${SYSTEMROOT:-C:/Windows}")/System32"

mapfile -t dlls < <(llvm-objdump -p "$exe" | sed -n 's/^[[:space:]]*DLL Name:[[:space:]]*//p' | tr -d '\r')
((${#dlls[@]} > 0)) || { printf '::error::llvm-objdump lists no DLL of %s\n' "$exe" >&2; exit 1; }
failed=0
for dll in "${dlls[@]}"; do
    lower=${dll,,}
    if [[ "$lower" =~ ^(api|ext)-ms- ]]; then
        continue
    fi
    # The Windows file system ignores case.
    if [[ ! -f "$system32/$dll" ]]; then
        printf '::error::%s imports %s, which is not a Windows DLL\n' "$exe" "$dll" >&2
        failed=1
    fi
done
((failed == 0)) || exit 1
printf '%s\n' "${dlls[@]}" | sort -fu
