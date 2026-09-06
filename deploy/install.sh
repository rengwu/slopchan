#!/bin/sh
# Download a stable release, verify SHA-256, and install without root.
# Usage: sh install.sh [v0.2.0] [--service]
set -eu
version=latest
service=no
for arg in "$@"; do
    case "$arg" in
        --service) service=yes ;;
        v*) version=$arg ;;
        *) echo "Usage: sh install.sh [vMAJOR.MINOR.PATCH] [--service]" >&2; exit 1 ;;
    esac
done
if [ "$(id -u)" = 0 ]; then
    echo 'Run as your normal user. Use deploy/setup-systemd.sh for a system-wide Linux service.' >&2
    exit 1
fi
case "$(uname -s)" in
    Linux) os=linux ;;
    Darwin) os=darwin ;;
    FreeBSD) os=freebsd ;;
    *) echo 'Unsupported OS; use install.ps1 on Windows.' >&2; exit 1 ;;
esac
case "$(uname -m)" in
    x86_64|amd64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    armv6*) arch=armv6 ;;
    armv7*|armv8l) arch=armv7 ;;
    i386|i486|i586|i686) arch=386 ;;
    riscv64) arch=riscv64 ;;
    *) echo 'Unsupported CPU; see docs/install.md for source builds.' >&2; exit 1 ;;
esac
# A 64-bit Linux kernel can host 32-bit userspace (notably Raspberry Pi OS).
if [ "$os" = linux ] && [ "$(getconf LONG_BIT)" = 32 ]; then
    case "$arch" in arm64) arch=armv7 ;; amd64) arch=386 ;; esac
fi
for tool in curl tar openssl; do
    command -v "$tool" >/dev/null || { echo "Required command missing: $tool" >&2; exit 1; }
done
base=https://github.com/rengwu/slopchan/releases
if [ "$version" = latest ]; then
    url=$(curl --proto '=https' --tlsv1.2 -fsSL -o /dev/null -w '%{url_effective}' "$base/latest")
    version=${url##*/}
fi
printf '%s\n' "$version" | LC_ALL=C grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$' || {
    echo 'Expected a stable vMAJOR.MINOR.PATCH release.' >&2; exit 1;
}
archive="slopchan_${version#v}_${os}_${arch}.tar.gz"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
download="$base/download/$version"
curl --proto '=https' --tlsv1.2 -fsSL "$download/$archive" -o "$tmp/$archive"
curl --proto '=https' --tlsv1.2 -fsSL "$download/checksums.txt" -o "$tmp/checksums.txt"
expected=$(awk -v name="$archive" '$2 == name {print $1}' "$tmp/checksums.txt")
actual=$(openssl dgst -sha256 "$tmp/$archive" | awk '{print $NF}')
[ -n "$expected" ] && [ "$expected" = "$actual" ] || { echo 'SHA-256 verification failed.' >&2; exit 1; }
tar -xzf "$tmp/$archive" -C "$tmp"
bin="$HOME/.local/bin"
config="$HOME/.config/slopchan"
data="$HOME/.local/share/slopchan"
umask 077
mkdir -p "$bin" "$config" "$data"
if [ ! -e "$config/tokens" ]; then openssl rand -hex 32 > "$config/tokens"; fi
# Rename supports upgrades while the old executable is still running on Unix.
install -m 0755 "$tmp/slopchan" "$bin/slopchan.new"
mv -f "$bin/slopchan.new" "$bin/slopchan"
mkdir -p "$data/install"
cp -R "$tmp/deploy" "$tmp/docs" "$tmp/licenses" "$tmp/skills" "$data/install/"
cp "$tmp/README.md" "$tmp/DESIGN.md" "$tmp/compose.yaml" "$tmp/compose.lan.yaml" "$tmp/.env.example" "$data/install/"
if [ "$service" = yes ]; then
    sh "$data/install/deploy/setup-user-service.sh"
else
    printf '\nInstalled %s. Start with:\n' "$version"
    printf '"%s/slopchan" serve -data "%s/data" -token-file "%s/tokens"\n' "$bin" "$data" "$config"
fi
printf '\nPosting token saved in %s/tokens (preserved on upgrades).\n' "$config"
echo 'Open http://127.0.0.1:8080 after starting. See docs/install.md for LAN access.'
