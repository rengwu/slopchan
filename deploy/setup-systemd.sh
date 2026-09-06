#!/bin/sh
# Usage: sudo sh deploy/setup-systemd.sh /absolute/path/to/slopchan
set -eu
[ "$(id -u)" = 0 ] || { echo 'Run with sudo.' >&2; exit 1; }
command -v systemctl >/dev/null || { echo 'This installer requires systemd.' >&2; exit 1; }
binary=${1:?Pass the downloaded or locally built slopchan executable}
[ -f "$binary" ] || { echo 'Executable not found.' >&2; exit 1; }
script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
command -v openssl >/dev/null
if ! id slopchan >/dev/null 2>&1; then
    useradd --system --user-group --home-dir /var/lib/slopchan --shell /usr/sbin/nologin slopchan
fi
install -m 0755 "$binary" /usr/local/bin/slopchan.new
mv -f /usr/local/bin/slopchan.new /usr/local/bin/slopchan
umask 077
if [ ! -e /etc/slopchan.env ]; then
    printf 'SLOPCHAN_TOKENS=%s\n' "$(openssl rand -hex 32)" > /etc/slopchan.env
fi
install -m 0644 "$script_dir/slopchan.service" /etc/systemd/system/slopchan.service
systemctl daemon-reload
systemctl enable slopchan
systemctl restart slopchan
systemctl --no-pager status slopchan
echo 'Token: /etc/slopchan.env. Data: /var/lib/slopchan. Listen: 127.0.0.1:8080.'
