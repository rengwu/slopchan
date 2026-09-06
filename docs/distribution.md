# Shipping it

GitHub downloads, Docker images, and the [Homebrew tap](https://github.com/rengwu/homebrew-tap)
are live for `0.2.0`. The other package listings below are ideas for next steps.
We checked their linked docs on **2026-09-06**.

## What CI does

The [release workflow](../.github/workflows/container.yml) runs on pull requests,
main, and stable `vMAJOR.MINOR.PATCH` tags. Here's the flow:

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
   Unix/PowerShell installers. The checks have to pass before anything ships.

The native smoke test loads a token file, checks auth, uploads an image, restarts,
searches, and removes a post. It uses a data path with spaces, because those happen.

A few limits to keep in mind: the Windows test force-stops the process, so it
doesn't test graceful Windows shutdown. Windows ARM64, Intel macOS, Linux 386/RISC-V,
and FreeBSD are cross-compiled only. QEMU is useful, but we still need real Pi
testing. CI also doesn't reboot hosts to check their installed services.

To run the checks locally, grab Go from `go.mod`, Python 3.11+, and Docker:

```sh
go test -race ./...
go vet ./...
python3 scripts/release.py dev
go build -o bin/slopchan .
python3 scripts/smoke-native.py bin/slopchan
docker build -t slopchan:test .
bash deploy/smoke-container.sh slopchan:test
```

0.2.0 added native downloads and 32-bit ARM containers. For the next release, pick
a fresh stable tag, update pinned examples and the Unraid template, review, then
push. Actions builds from that exact commit using its built-in token.

After publishing, try the downloads and container pulls without logging in. Update
the Homebrew tap's source URL and SHA-256 to match the new release.

The workflow won't overwrite an existing GitHub release. If it fails, check the
run before retrying: the container tags might already be up. Fixes to published
builds get a new version so people can trust the version they downloaded.

## Where to list it next

| Priority | Repository / marketplace | Why it fits and submission work |
| --- | --- | --- |
| 1 | **GitHub Releases + GHCR** | The main downloads, already live and automated here. Mirror to Docker Hub later if users ask for it; that adds registry credentials and another publication destination. |
| 1 | **Homebrew personal tap** ([rengwu/homebrew-tap](https://github.com/rengwu/homebrew-tap)) | Live: `0.2.0` source formula, a private token set up for you, persistent storage, `brew services`, and API/persistence tests. Install with `brew install rengwu/tap/slopchan`. Maintain version bumps in the tap; bottles and Homebrew core submission remain future work. [Tap guide](https://docs.brew.sh/How-to-Create-and-Maintain-a-Tap), [formula/service cookbook](https://docs.brew.sh/Formula-Cookbook). |
| 1 | **Scoop personal bucket**, then `ScoopInstaller/Main` | A good home for the portable Windows ZIP. No MSI needed. Generate x64/ARM64 URL and SHA-256 entries from each release, expose `slopchan.exe`, and document service setup separately. Keep mutable state under LocalAppData or use Scoop's persist mechanism, outside versioned package directories. [Buckets](https://github.com/ScoopInstaller/Scoop/wiki/Buckets), [Main repository](https://github.com/ScoopInstaller/Main). |
| 1 | **Unraid Community Applications** | The existing XML is a good starting point. Prepare a public template repository, icon, screenshots, support/project URLs, and confirm storage permissions and the pinned image on Unraid. Follow the current maintainer intake process linked by the [Community Applications project](https://github.com/Squidly271/community.applications). |
| 2 | **AUR**: `slopchan` and/or `slopchan-bin` | Offer a source PKGBUILD first, or `-bin` for the official archives. Include systemd integration, a dedicated system user through sysusers, license notices, and persistent state outside the package. Generate checksums and `.SRCINFO`; never use `SKIP` for release checksums. AUR hosts build recipes, not the compiled archive. [Submission guidelines](https://wiki.archlinux.org/title/AUR_submission_guidelines). |
| 2 | **CasaOS / ZimaOS App Store** | Good match for mini NAS owners. Adapt LAN Compose with the store's metadata, architecture list, icon/screenshots, writable local storage, web portal, and token configuration. Test the actual import/install UI before submitting to the official store. [Store](https://github.com/IceWhaleTech/CasaOS-AppStore), [contributing](https://github.com/IceWhaleTech/CasaOS-AppStore/blob/main/CONTRIBUTING.md). |
| 2 | **TrueNAS Apps, community train** | Another good place to reach NAS users. Wrap the image in the catalog's questions, storage/permission, port, and portal schema; validate in a current TrueNAS VM. Plain Compose already gives users a custom-app route. [Contribution guide](https://github.com/truenas/apps/blob/master/CONTRIBUTIONS.md). |
| 2 | **WinGet** (`microsoft/winget-pkgs`) | Add after Windows installation has field testing. Prepare portable ZIP installer manifests with the executable alias, architectures, URLs, and SHA-256 hashes; validate and submit a PR. A listing helps people find it; service setup is still a separate step. [Submission process](https://learn.microsoft.com/en-us/windows/package-manager/package/repository). |
| 3 | **Umbrel App Store** | Useful for home-server discovery, but needs Umbrel app metadata/proxy integration and testing that agents can send bearer tokens without an interactive login barrier. Start with a community store, then submit to the official repository. [App guide](https://github.com/getumbrel/umbrel-apps). |
| 3 | **YunoHost / Nixpkgs** | Good declarative server installation once demand exists. YunoHost needs a maintained lifecycle/backup package; Nixpkgs needs a Go derivation and ideally a NixOS module using a token-file secret. [YunoHost packaging](https://doc.yunohost.org/dev/packaging/), [Nixpkgs contributing](https://github.com/NixOS/nixpkgs/blob/master/CONTRIBUTING.md). |

Start with **Releases/GHCR, Homebrew, Scoop, and Unraid CA**, then look at **AUR
and CasaOS/ZimaOS**. That reaches desktops, servers, Pis, and NAS boxes without
giving us ten package pipelines to babysit.
Homebrew core is a later submission after meeting its [acceptance criteria](https://docs.brew.sh/Acceptable-Formulae).
Debian/RPM packages and signed APT/YUM repos can come later if people want OS-level
package updates across lots of machines. The binaries and systemd setup already
work on those hosts. Snap, Flatpak, Chocolatey, Synology SPK, and QNAP QPKG each
bring more maintenance, so let people ask for them first.

## Before sending a package in

The project code is MIT licensed; see [asset provenance](ASSETS.md) for third-party
artwork and dependency notices. The release builder includes the project license
and dependency license/notice files.

Point package recipes at stable public downloads with matching hashes. Keep
`main`, dev builds, and URLs containing credentials out of them. Have someone own
version bumps and support, add an icon/screenshots, and explain where data lives
and how upgrades/backups work. Test install → post/image → restart → upgrade →
uninstall in each package manager, checking that the board survives. Signing macOS and Windows
builds would make the first run smoother; the current binaries are unsigned.
Homebrew is live. Scoop and the other marketplace submissions are still to do.
