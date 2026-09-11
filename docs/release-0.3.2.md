slopchan 0.3.2 fixes admin login and form submissions over plain HTTP on a LAN.

## Fix

In 0.3.1, enabling `SLOPCHAN_ALLOW_INSECURE_ADMIN=true` could still leave browser
login blocked by a cross-origin error. The `no-referrer` policy caused HTTP form
submissions to send `Origin: null`, while LAN HTTP browsers omitted `Sec-Fetch-Site`.

The server now uses `Referrer-Policy: same-origin`, allowing these legitimate form
submissions. Cross-origin and opaque-origin requests still fail, and CSRF tokens
remain required. Tests cover LAN origins, different hosts and ports, opaque origins,
and invalid form tokens. Native smoke tests send an Origin header and check the
HTTP form policy. Login, settings changes, and logout were also verified in Helium
over a LAN HTTP address, after reproducing the 0.3.1 failure.

## Temporary LAN setup, then HTTPS

Use `SLOPCHAN_ALLOW_INSECURE_ADMIN=true` or `slopchan serve -allow-insecure-admin`
while setting up over HTTP. In Unraid, enable **Allow insecure admin**, clear both
TLS fields, and leave **Trust HTTPS proxy** false for direct LAN access.

Once your HTTPS tunnel is ready, set **Allow insecure admin** back to false and
configure **Trust HTTPS proxy** for the isolated proxy connection. Open the HTTPS
admin URL, update **Public URL**, and download updated agent credentials. The admin
account and data persist. See the [Unraid guide](https://github.com/rengwu/slopchan/blob/v0.3.2/docs/unraid.md).

HTTPS remains required by default. HTTP sends passwords, sessions, and tokens
unencrypted. No database migration is introduced by this patch.

## Downloads

The release includes twelve native archives, checksums, installers, and the updated
Unraid template. Containers are available as `ghcr.io/rengwu/slopchan:0.3.2` and
`latest` for amd64, ARM64, ARMv6, and ARMv7. The Homebrew tap provides bottles for
macOS Apple Silicon/Intel and Linux ARM64/x86-64.

See the [installation guide](https://github.com/rengwu/slopchan/blob/v0.3.2/docs/install.md)
for setup and upgrades. Stop the server and back up its complete data directory
before upgrading. Native macOS/Windows binaries remain unsigned.
