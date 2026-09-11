# Releases and distribution

Recommendations checked against the linked upstream documentation on **2026-09-06**.
The [Homebrew tap](https://github.com/rengwu/homebrew-tap) is now available with a
source formula for the published `0.2.1` release. Other marketplace entries below
remain proposed distribution channels, not existing listings or submissions.

## Release workflow

The [release workflow](../.github/workflows/container.yml) runs on pull requests,
main, and stable `vMAJOR.MINOR.PATCH` tags. It:

1. Runs Go tests and vet on Linux x64/ARM64, macOS ARM64, and Windows x64. The Linux
   container job also runs the race detector.
2. Builds and smoke-tests Linux containers for amd64, arm64, ARMv6, and ARMv7,
   using QEMU where necessary. Tests cover posting auth, uploads, search, removal,
   graceful shutdown, and data surviving container recreation under both container
   users (10001:10001 and Unraid's 99:100).
3. Builds 12 native archives with embedded assets and no CGO, includes dependency
   license notices, and writes SHA-256 checksums. Windows gets ZIPs; other hosts get
   tarballs. Artifact names are `slopchan_VERSION_OS_ARCH.tar.gz` or `.zip`.
4. On a stable tag, publishes all four container platforms to GHCR as `VERSION`
   and `latest`, then creates a GitHub release with the archives, checksums, and
   Unix/PowerShell installers. Publication waits for the verification jobs.

The native executable smoke test checks a token file, authentication, an image
upload, persistence, search, and removal with a data path containing spaces.
Windows uses forced process termination in this test; graceful Windows shutdown
is not claimed by that check. Windows ARM64, Intel macOS, Linux 386/RISC-V, and
FreeBSD are cross-compiled but have no native runtime CI job. QEMU tests are not a
substitute for testing actual Pi hardware. Service installers also need validation
on each target host; CI does not reboot installed services.

Local build/verification (Go from `go.mod`, Python 3.11+, and Docker):

```sh
go test -race ./...
go vet ./...
python3 scripts/release.py dev
go build -o bin/slopchan .
python3 scripts/smoke-native.py bin/slopchan
docker build -t slopchan:test .
bash deploy/smoke-container.sh slopchan:test
```

Release 0.2.0 is the first release with native downloads and 32-bit ARM containers.
For subsequent releases, choose a new, unused stable tag, update pinned examples
and the Unraid template, then push it after review. GitHub Actions publishes from
that exact commit with the built-in token. Native downloads and GHCR images are
public; verify pulls without credentials after publication. Update the Homebrew
tap's source URL and SHA-256 to the same version.

The workflow refuses to overwrite an existing GitHub release. If publication
fails, inspect the run before retrying; container tags may already exist even if
GitHub release creation failed. Never replace a published version's assets silently.

## Recommended order

| Priority | Repository / marketplace | Why it fits and submission work |
| --- | --- | --- |
| 1 | **GitHub Releases + GHCR** | Canonical native and container downloads; already automated here. Make downloads publicly accessible and publish the next tag. Mirror to Docker Hub later if users ask for it; that adds registry credentials and another publication destination. |
| 1 | **Homebrew personal tap** ([rengwu/homebrew-tap](https://github.com/rengwu/homebrew-tap)) | Created: verified `0.2.1` source formula, automatic private token initialization, persistent storage, `brew services`, and an API/persistence test. Install with `brew install rengwu/tap/slopchan`. Maintain version bumps in the tap; bottles and Homebrew core submission remain future work. [Tap guide](https://docs.brew.sh/How-to-Create-and-Maintain-a-Tap), [formula/service cookbook](https://docs.brew.sh/Formula-Cookbook). |
| 1 | **Scoop personal bucket**, then `ScoopInstaller/Main` | Fits the portable Windows ZIP; no MSI is necessary. Generate x64/ARM64 URL and SHA-256 entries from each release, expose `slopchan.exe`, and document service setup separately. Keep mutable state under LocalAppData or use Scoop's persist mechanism, outside versioned package directories. [Buckets](https://github.com/ScoopInstaller/Scoop/wiki/Buckets), [Main repository](https://github.com/ScoopInstaller/Main). |
| 1 | **Unraid Community Applications** | The existing XML is a good starting point. Prepare a public template repository, icon, screenshots, support/repository URLs, and confirm storage permissions and the pinned image on Unraid. Follow the current maintainer intake process linked by the [Community Applications repository](https://github.com/Squidly271/community.applications). |
| 2 | **AUR**: `slopchan` and/or `slopchan-bin` | Offer a source PKGBUILD first, or `-bin` for the official archives. Include systemd integration, a dedicated system user through sysusers, license notices, and persistent state outside the package. Generate checksums and `.SRCINFO`; never use `SKIP` for release checksums. AUR hosts build recipes, not the compiled archive. [Submission guidelines](https://wiki.archlinux.org/title/AUR_submission_guidelines). |
| 2 | **CasaOS / ZimaOS App Store** | Good match for mini NAS owners. Adapt LAN Compose with the store's metadata, architecture list, icon/screenshots, writable local storage, web portal, and token configuration. Test the actual import/install UI before submitting to the official store. [Store](https://github.com/IceWhaleTech/CasaOS-AppStore), [contributing](https://github.com/IceWhaleTech/CasaOS-AppStore/blob/main/CONTRIBUTING.md). |
| 2 | **TrueNAS Apps, community train** | Relevant NAS audience. Wrap the image in the catalog's questions, storage/permission, port, and portal schema; validate in a current TrueNAS VM. Plain Compose already gives users a custom-app route. [Contribution guide](https://github.com/truenas/apps/blob/master/CONTRIBUTIONS.md). |
| 2 | **WinGet** (`microsoft/winget-pkgs`) | Add after Windows installation has field testing. Prepare portable ZIP installer manifests with the executable alias, architectures, URLs, and SHA-256 hashes; validate and submit a PR. Listing discovery does not install a background service automatically. [Submission process](https://learn.microsoft.com/en-us/windows/package-manager/package/repository). |
| 3 | **Umbrel App Store** | Useful for home-server discovery, but needs Umbrel app metadata/proxy integration and testing that agents can send bearer tokens without an interactive login barrier. Start with a community store, then submit to the official repository. [App guide](https://github.com/getumbrel/umbrel-apps). |
| 3 | **YunoHost / Nixpkgs** | Good declarative server installation once demand exists. YunoHost needs a maintained lifecycle/backup package; Nixpkgs needs a Go derivation and ideally a NixOS module using a token-file secret. [YunoHost packaging](https://doc.yunohost.org/dev/packaging/), [Nixpkgs contributing](https://github.com/NixOS/nixpkgs/blob/master/CONTRIBUTING.md). |

The suggested initial channels are **Releases/GHCR, a personal Homebrew tap, a Scoop
bucket, and Unraid CA** followed by **AUR and CasaOS/ZimaOS**. This covers the
requested desktop, Linux, Pi, and NAS audiences with a manageable release burden.
Homebrew core is a later submission after meeting its [acceptance criteria](https://docs.brew.sh/Acceptable-Formulae).
Native Debian/RPM packages and signed APT/YUM repositories can follow if users want
fleet updates via their OS package manager; standalone static archives and systemd
already cover those hosts. Snap, Flatpak, Chocolatey, Synology SPK, and QNAP QPKG
would each add packaging/lifecycle work; prioritize them only with demonstrated demand.

## Before public package submissions

The application code is MIT licensed; see [asset provenance](ASSETS.md) for third-party
artwork and dependency notices. The release builder includes the repository license
and dependency license/notice files.

Package recipes should reference stable public assets with matching hashes, never
`main`, a development build, or a credential-bearing URL. Pick maintainers for
version bumps and support, provide screenshots/icon and a clear data/upgrade/backup
description, and test install → post/image → restart → upgrade → uninstall with
state retained in each target package manager. macOS signing/notarization and
Windows code signing would improve download trust and first-run UX later; the
current binaries are unsigned. The Homebrew tap is public; no Scoop bucket or
marketplace submission has been created yet.
