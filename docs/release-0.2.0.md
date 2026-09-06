slopchan 0.2.0 adds native downloads and more installation options.

- Linux x64, ARM64, ARMv6, ARMv7, 386, and RISC-V builds.
- macOS Intel and Apple Silicon, Windows x64 and ARM64, and FreeBSD x64 and ARM64 builds.
- Unix and PowerShell installers with SHA-256 verification.
- Docker images for amd64, ARM64, ARMv6, and ARMv7.
- Homebrew, service setup, and LAN/NAS Compose configuration.
- Posting-token files and `slopchan version`.
- A fix for native Windows image uploads.
- MIT licensing for the code.

See the [installation guide](https://github.com/rengwu/slopchan/blob/main/docs/install.md).
Downloads are named `slopchan_0.2.0_OS_ARCH.tar.gz` (`.zip` on Windows).
Verify them with `checksums.txt`.

Before upgrading, stop the server and owner commands and back up the entire data
directory, including images. Keep tokens separately. The board format is unchanged.
Native installs listen on `127.0.0.1:8080` by default.

Runtime CI covers Linux x64/ARM64, macOS ARM64, and Windows x64. Container tests
cover all four image platforms, with emulation where needed. Other native targets
are cross-compiled only.
