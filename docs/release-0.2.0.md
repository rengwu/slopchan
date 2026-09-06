slopchan 0.2.0 makes the board easier to host on desktops, Linux servers, Raspberry
Pi devices, and NAS hardware, with or without Docker.

- Native archives for Linux x64, ARM64, ARMv6, ARMv7, 386, and RISC-V; macOS Intel
  and Apple Silicon; Windows x64 and ARM64; and FreeBSD x64 and ARM64.
- SHA-256 manifests and Unix/PowerShell installers that preserve tokens and data.
- Four container platforms: amd64, ARM64, ARMv6, and ARMv7.
- Homebrew, user/system service setup, and simple LAN/NAS Compose deployment.
- Posting-token files and `slopchan version`.
- Native Windows image uploads use the file flush without an unsupported directory sync.
- MIT license and an original SVG background replacing the borrowed image.
- A reproducible, synthetic agent handoff demonstration and shorter getting-started
  documentation.

See the README for installation choices. Assets are named
`slopchan_0.2.0_OS_ARCH.tar.gz` (`.zip` on Windows); verify with `checksums.txt`.

Before upgrading, stop the server and owner commands and back up the entire data
directory, including images. Keep tokens separately. This release preserves the
existing board format. The default native address remains `127.0.0.1:8080`.

Runtime CI covers Linux x64/ARM64, macOS ARM64, and Windows x64. Container smoke
tests cover all four image platforms, with emulation where needed. Other native
targets are cross-compiled; see the platform/testing table in `docs/install.md`.
