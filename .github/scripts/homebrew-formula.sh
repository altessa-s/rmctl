#!/bin/bash
# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license that can be found in
# the LICENSE file.
# Write a Homebrew formula for a release to stdout.
#
# Usage: homebrew-formula.sh [--lite] VERSION CHECKSUMS
#
# VERSION is the release version without the leading "v"; CHECKSUMS is the
# release's checksums.txt. The formula installs the prebuilt macOS and Linux
# archives of that release; a missing checksum fails the script before any of
# the formula is written.
#
# Without --lite it is rmctl.rb, class Rmctl: the full build. A Linux archive
# carries libpdfium.so, which the binary finds in its own directory through
# its $ORIGIN run path, so both go into libexec and bin links to the binary.
# Every command of the Linux binary loads Cairo, Pango and their libraries,
# so the formula depends on them.
#
# With --lite it is rmctl-lite.rb, class RmctlLite: the rmctl-lite archives,
# the build without the renderer, whose binary needs no library beyond the
# system's and goes straight into bin. Both binaries are named rmctl, so each
# formula conflicts with the other.
#
# Both install the completion scripts of bash, zsh and fish, which the
# installed binary writes with "rmctl completion <shell>", and define the
# mirror service, so "brew services start rmctl" keeps every mirror of
# config.toml in step: it runs "rmctl mirror run --service", which writes its
# own log; brew's log file catches what it writes before that log opens.
set -euo pipefail

lite=0
if [[ "${1:-}" == --lite ]]; then
    lite=1
    shift
fi
version=${1:?version}
checksums=${2:?checksums file}
base="https://github.com/altessa-s/rmctl/releases/download/v${version}"
if ((lite)); then
    name=rmctl-lite class=RmctlLite other=rmctl
    desc="Command-line client for the reMarkable cloud, without rendering"
else
    name=rmctl class=Rmctl other=rmctl-lite
    desc="Command-line client for the reMarkable cloud"
fi

# sum SUFFIX: the checksum of the archive ${name}_${version}_SUFFIX.
sum() {
    local file="${name}_${version}_$1"
    local value
    value=$(awk -v f="$file" '$2 == f || $2 == "*" f { print $1 }' "$checksums")
    [[ "$value" =~ ^[0-9a-f]{64}$ ]] || { printf 'no checksum for %s\n' "$file" >&2; exit 1; }
    printf '%s' "$value"
}

darwin_arm64=$(sum darwin_arm64.zip)
darwin_amd64=$(sum darwin_amd64.zip)
linux_arm64=$(sum linux_arm64.tar.gz)
linux_amd64=$(sum linux_amd64.tar.gz)

linux_deps=""
install='    bin.install "rmctl"'
if ((!lite)); then
    linux_deps='    depends_on "cairo"
    depends_on "fontconfig"
    depends_on "gdk-pixbuf"
    depends_on "harfbuzz"
    depends_on "pango"

'
    install='    if OS.linux?
      libexec.install "rmctl", "libpdfium.so"
      bin.install_symlink libexec/"rmctl"
    else
      bin.install "rmctl"
    end'
fi

cat <<EOF
# This file is written by the rmctl release workflow; edits are overwritten.
class ${class} < Formula
  desc "${desc}"
  homepage "https://github.com/altessa-s/rmctl"
  version "${version}"
  license "MIT"

  on_macos do
    on_arm do
      url "${base}/${name}_${version}_darwin_arm64.zip"
      sha256 "${darwin_arm64}"
    end
    on_intel do
      url "${base}/${name}_${version}_darwin_amd64.zip"
      sha256 "${darwin_amd64}"
    end
  end

  on_linux do
${linux_deps}    on_arm do
      url "${base}/${name}_${version}_linux_arm64.tar.gz"
      sha256 "${linux_arm64}"
    end
    on_intel do
      url "${base}/${name}_${version}_linux_amd64.tar.gz"
      sha256 "${linux_amd64}"
    end
  end

  conflicts_with "${other}", because: "both install an rmctl binary"

  def install
${install}
    generate_completions_from_executable(bin/"rmctl", "completion")
    doc.install "licenses" if File.directory?("licenses")
  end

  service do
    run [opt_bin/"rmctl", "mirror", "run", "--service"]
    keep_alive successful_exit: false
    log_path var/"log/rmctl-mirror.log"
    error_log_path var/"log/rmctl-mirror.log"
  end

  test do
    assert_equal version.to_s, shell_output("#{bin}/rmctl version").strip
  end
end
EOF
