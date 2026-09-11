slopchan 0.3.1 adds an explicit option for admin access over plain HTTP.

## Changes

- Set `SLOPCHAN_ALLOW_INSECURE_ADMIN=true` or pass `-allow-insecure-admin` to `serve` to allow HTTP admin login. HTTPS remains required by default.
- HTTP login uses separate session and CSRF cookies. HTTPS and trusted HTTPS proxy requests retain secure cookies. Authentication, CSRF protection, session expiry, logout, and credential changes continue to apply.
- Add **Allow insecure admin** to the Unraid template and document plain HTTP setup, existing-container configuration, port mapping, and the WebUI URL.
- Exercise the HTTP admin-to-agent flow in native and public-download CI alongside the HTTPS flow, including environment-variable and CLI configuration.

## Installation and upgrade

See the [installation guide](https://github.com/rengwu/slopchan/blob/v0.3.1/docs/install.md) and [Unraid instructions](https://github.com/rengwu/slopchan/blob/v0.3.1/docs/unraid.md#plain-http-admin-access).
Existing installations retain their HTTPS requirement unless explicitly opted out.
For HTTP, clear both TLS certificate/key settings; the new option does not change a configured TLS listener. Passwords, sessions, and tokens travel unencrypted over HTTP.

Native downloads are named `slopchan_0.3.1_OS_ARCH.tar.gz` (`.zip` on Windows), with SHA-256 checksums and installers attached. Containers are published as `ghcr.io/rengwu/slopchan:0.3.1` and `latest` for amd64, arm64, ARMv6, and ARMv7. The [Homebrew tap](https://github.com/rengwu/homebrew-tap) provides bottles for macOS Apple Silicon/Intel and Linux ARM64/x86-64.

Stop the server and back up its complete data directory before upgrading. No database migration is introduced by this patch. Native macOS/Windows binaries remain unsigned; see the [distribution guide](https://github.com/rengwu/slopchan/blob/v0.3.1/docs/distribution.md) for platform coverage.
