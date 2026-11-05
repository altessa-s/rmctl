#!/bin/bash
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license that can be found in
# the LICENSE file.
# Put the DLLs a Windows rmctl.exe loads, and their licences, beside it, or
# check that an unpacked rmctl.exe loads nothing else.
#
# Usage: bundle-windows-dlls.sh EXE
#        bundle-windows-dlls.sh --verify EXE
#
# Runs in the MSYS2 CLANG64 shell setup-render-windows.sh ran in. A Windows
# rmctl draws with CLANG64's Cairo stack and the PDFium SDK's pdfium.dll,
# which are DLLs, so the release archive carries them next to rmctl.exe,
# where Windows looks first. ntldd -R lists every DLL the program loads,
# directly or through another DLL.
#
# Bundling (PDFIUM_DIR set): each DLL ntldd resolves under CLANG64's bin or
# PDFium's bin is copied into the directory of EXE. DLLs under the Windows
# directory are the system's and stay out. A DLL found anywhere else, or not
# found at all (but an API set, which Windows maps itself), fails the script.
# Each CLANG64 DLL belongs to an MSYS2 package (pacman -Qo); the files that
# package installs under $MSYSTEM_PREFIX/share/licenses go to
# licenses/msys2/<package>/, with its pacman -Qi record as PACKAGE.txt
# (version, upstream URL and licence names). A package that installs no
# licence file gets only PACKAGE.txt and a warning. PDFium's licences are the
# caller's to copy, as on Linux. The names of the copied DLLs are printed.
#
# --verify: ntldd resolves with PATH reduced to the Windows directory, as on a
# machine without MSYS2, and every DLL must come from the directory of EXE or
# from Windows; the DLLs it loads are printed.
set -euo pipefail

verify=0
if [[ "${1:-}" == --verify ]]; then
    verify=1
    shift
fi
exe=${1:?exe}
[[ -f "$exe" ]] || { printf '%s: no such file\n' "$exe" >&2; exit 1; }
: "${MSYSTEM_PREFIX:?run this in an MSYS2 shell}"
prefix=${MINGW_PACKAGE_PREFIX:-mingw-w64-clang-x86_64}
out=$(dirname "$exe")
ntldd="$MSYSTEM_PREFIX/bin/ntldd"

# Paths are compared in mixed form, lowercased, with a trailing slash.
lower() {
    local s
    s=$(cat)
    s=${s//\\//}
    printf '%s' "${s,,}"
}
dir_key() { printf '%s/' "$(cygpath -m "$1" | lower)"; }
windows=$(dir_key "${SYSTEMROOT:-C:/Windows}")
if ((verify)); then
    allowed=("$(dir_key "$out")")
    system=$(cygpath -u "${SYSTEMROOT:-C:/Windows}")
    search="$system/System32:$system"
else
    : "${PDFIUM_DIR:?}"
    clang_bin=$(dir_key "$MSYSTEM_PREFIX/bin")
    allowed=("$clang_bin" "$(dir_key "$PDFIUM_DIR/bin")")
    search="$(cygpath -u "$PDFIUM_DIR/bin"):$PATH"
fi

# ntldd writes "name => path (0xADDRESS)" or "name => not found", with CRLF.
listing=$(PATH="$search" "$ntldd" -R "$exe")
found_re='^[[:space:]]*([^[:space:]]+) => (.+) \(0x[0-9a-fA-F]+\)[[:space:]]*$'
missing_re='^[[:space:]]*([^[:space:]]+) => not found'
declare -A packages=()
loaded=()
failed=0
while IFS= read -r line; do
    if [[ "$line" =~ $missing_re ]]; then
        name=${BASH_REMATCH[1]}
        [[ "$name" =~ ^(api|ext)-ms- ]] && continue
        printf '::error::%s needs %s, which is not found\n' "$exe" "$name" >&2
        failed=1
        continue
    fi
    [[ "$line" =~ $found_re ]] || continue
    name=${BASH_REMATCH[1]}
    path=${BASH_REMATCH[2]}
    where=$(printf '%s' "$path" | lower)
    [[ "$where" == "$windows"* ]] && continue
    ok=0
    for dir in "${allowed[@]}"; do
        [[ "$where" == "$dir"* ]] && ok=1
    done
    if ((!ok)); then
        printf '::error::%s loads %s from %s, outside %s\n' "$exe" "$name" "$path" "${allowed[*]}" >&2
        failed=1
        continue
    fi
    unix=$(cygpath -u "$path")
    loaded+=("$(basename "$unix")")
    ((verify)) && continue
    cp "$unix" "$out/"
    if [[ "$where" == "$clang_bin"* ]]; then
        packages[$(pacman -Qqo "$unix")]=1
    fi
done <<<"$listing"
((failed == 0)) || exit 1
((${#loaded[@]} > 0)) || { printf '::error::ntldd lists no DLL of %s\n' "$exe" >&2; exit 1; }

for package in "${!packages[@]}"; do
    dest="$out/licenses/msys2/${package#"$prefix"-}"
    mkdir -p "$dest"
    pacman -Qi "$package" > "$dest/PACKAGE.txt"
    found=0
    while IFS= read -r file; do
        [[ "$file" == "$MSYSTEM_PREFIX/share/licenses/"* && -f "$file" ]] || continue
        # The path below the package's own licences directory.
        rel=${file#"$MSYSTEM_PREFIX/share/licenses/"}
        rel=${rel#*/}
        mkdir -p "$dest/$(dirname "$rel")"
        cp "$file" "$dest/$rel"
        found=1
    done < <(pacman -Qlq "$package")
    ((found)) || printf '::warning::%s installs no licence file; only its PACKAGE.txt is shipped\n' "$package" >&2
done

printf '%s\n' "${loaded[@]}" | sort -fu
