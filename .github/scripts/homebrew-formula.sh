#!/bin/bash
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license that can be found in
# the LICENSE file.
# Write the Homebrew formula for a release to stdout.
#
# Usage: homebrew-formula.sh VERSION CHECKSUMS
#
# VERSION is the release version without the leading "v"; CHECKSUMS is the
# release's checksums.txt. The formula installs the prebuilt macOS and Linux
# archives of that release. A Linux archive carries libpdfium.so, which the
# binary finds in its own directory through its $ORIGIN run path, so both go
# into libexec and bin links to the binary. Every command of the Linux binary
# loads Cairo, Pango and their libraries, so the formula depends on them.
set -euo pipefail

version=${1:?version}
checksums=${2:?checksums file}
base="https://github.com/altessa-s/rmctl/releases/download/v${version}"

sum() {
    local file="rmctl_${version}_$1.tar.gz"
    local value
    value=$(awk -v f="$file" '$2 == f || $2 == "*" f { print $1 }' "$checksums")
    [[ "$value" =~ ^[0-9a-f]{64}$ ]] || { printf 'no checksum for %s\n' "$file" >&2; exit 1; }
    printf '%s' "$value"
}

cat <<EOF
# This file is written by the rmctl release workflow; edits are overwritten.
class Rmctl < Formula
  desc "Command-line client for the reMarkable cloud"
  homepage "https://github.com/altessa-s/rmctl"
  version "${version}"
  license "MIT"

  on_macos do
    on_arm do
      url "${base}/rmctl_${version}_darwin_arm64.tar.gz"
      sha256 "$(sum darwin_arm64)"
    end
    on_intel do
      url "${base}/rmctl_${version}_darwin_amd64.tar.gz"
      sha256 "$(sum darwin_amd64)"
    end
  end

  on_linux do
    depends_on "cairo"
    depends_on "fontconfig"
    depends_on "gdk-pixbuf"
    depends_on "harfbuzz"
    depends_on "pango"

    on_arm do
      url "${base}/rmctl_${version}_linux_arm64.tar.gz"
      sha256 "$(sum linux_arm64)"
    end
    on_intel do
      url "${base}/rmctl_${version}_linux_amd64.tar.gz"
      sha256 "$(sum linux_amd64)"
    end
  end

  def install
    if OS.linux?
      libexec.install "rmctl", "libpdfium.so"
      bin.install_symlink libexec/"rmctl"
    else
      bin.install "rmctl"
    end
    doc.install "licenses" if File.directory?("licenses")
  end

  test do
    assert_equal version.to_s, shell_output("#{bin}/rmctl version").strip
  end
end
EOF
