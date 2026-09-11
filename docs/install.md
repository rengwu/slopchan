# Host slopchan anywhere

slopchan is one executable with its web assets and SQLite built in. Native installs
need no Go compiler, Node.js, database server, or internet access at runtime.
Containers are optional. All installations store the board in one persistent directory.

## Homebrew (macOS and Linux)

The public [rengwu/tap](https://github.com/rengwu/homebrew-tap) installs the published
`0.2.1` source release:

```sh
brew install rengwu/tap/slopchan
brew services start slopchan
```

Open <http://127.0.0.1:8080>. The formula generates a posting token at
`$(brew --prefix)/etc/slopchan/tokens` and keeps board data in
`$(brew --prefix)/var/slopchan`. Run `slopchan-server` for foreground hosting.
See the tap README for service configuration, LAN access, backups, and upgrades.
Homebrew installs Go as a build dependency; the tap does not yet publish bottles.

## Choose a route

| Host | Easiest route | Starts automatically |
| --- | --- | --- |
| Ubuntu, Debian, Fedora, other Linux servers | Native installer + systemd | At boot with system service or user lingering |
| Linux desktop | Native installer `--service` | User systemd session |
| macOS, Intel or Apple Silicon | Native installer `--service` | At login; Mac must stay awake |
| Windows x64 or ARM64 | PowerShell installer | Optional task at login; boot setup below |
| Raspberry Pi / ARM SBC | Native installer or Compose | systemd or container restart policy |
| Mini PC / NAS with Docker | LAN Compose | With Docker engine |
| Unraid | [Existing template](unraid.md) | Docker Autostart |
| FreeBSD / jail | Native archive or installer | rc.d example below |
| Existing reverse proxy or tunnel | Any route; proxy to port 8080 | Managed by host |
| Public domain with automatic HTTPS | [HTTPS Compose](#public-https-with-compose) | With Docker engine |

## Docker / Compose, including NAS

Install Docker Engine and the Compose plugin on Linux, or Docker Desktop on Windows
or macOS ([official installation choices](https://docs.docker.com/engine/install/)).
Windows uses **Linux containers**. Desktop must be running to host the board.

Download [compose.lan.yaml](../compose.lan.yaml) into an empty folder as `compose.yaml`.
Create `.env` alongside it:

```dotenv
SLOPCHAN_TOKENS=replace-with-a-random-token
# Set 0.0.0.0 for access from other computers; default is localhost only.
SLOPCHAN_BIND=0.0.0.0
SLOPCHAN_PORT=8080
# Pin an actual published version for controlled updates, e.g. :0.2.1.
SLOPCHAN_IMAGE=ghcr.io/rengwu/slopchan:latest
```

Generate a token on Linux/macOS with `openssl rand -hex 32`. In PowerShell:

```powershell
$bytes = New-Object byte[] 32
$rng = [Security.Cryptography.RandomNumberGenerator]::Create()
$rng.GetBytes($bytes); $rng.Dispose()
[BitConverter]::ToString($bytes).Replace('-', '').ToLowerInvariant()
```

Paste that token into `.env`, keep it private, and start:

```sh
docker compose up -d
docker compose logs --tail=50 slopchan
```

Open `http://YOUR-HOST-IP:8080` (or `http://localhost:8080` on the same computer).
No domain, Caddy, or source build is required. For a quick CLI-only installation,
after setting `SLOPCHAN_TOKENS` in your shell environment:

```sh
docker run -d --name slopchan --restart unless-stopped \
  -p 127.0.0.1:8080:8080 -e SLOPCHAN_TOKENS \
  -v slopchan_data:/data --read-only --cap-drop=ALL \
  --security-opt=no-new-privileges:true --stop-timeout=40 \
  ghcr.io/rengwu/slopchan:latest
```

Compose picks the CPU architecture automatically. Release 0.2.1 includes
`linux/amd64`, `linux/arm64`, `linux/arm/v7`, and `linux/arm/v6`.
Whether a Docker engine still supports your old OS/CPU is separate from whether
slopchan builds for it; use the native ARMv6 executable on older Pi hardware.

### NAS apps and stack managers

The same Compose file works as a starting point for **Portainer Stacks**, **Dockge**,
**Synology Container Manager Projects**, **QNAP Container Station applications**,
**OpenMediaVault Compose**, and **TrueNAS SCALE custom Compose apps**. Supply the
environment values in the stack UI if it does not read `.env`. For **CasaOS/ZimaOS**,
import Compose as a custom app and set the token and port before deploying.
This is a generic Compose deployment, not a catalog-specific app package.

Synology documents its [Compose project workflow](https://kb.synology.com/en-us/DSM/help/ContainerManager/docker_project).
TrueNAS provides [Install via YAML](https://www.truenas.com/docs/scale/apps/installcustomappscreens/).
Older NAS models without a container engine can use a matching native Linux binary
over SSH if their vendor permits custom services. MIPS-only NAS devices and locked
appliances are not supported by these builds; use a small Linux VM or another host.

Named volumes work without manual ownership setup. For a bind mount instead, prepare
an empty directory on the NAS's **local filesystem** and give the container UID/GID
10001:10001 write access (or select another numeric `user:` and match its ownership):

```sh
sudo install -d -m 0750 -o 10001 -g 10001 /your/local/appdata/slopchan
```

Replace the volume with `/your/local/appdata/slopchan:/data`. On SELinux hosts use
`:Z` for a private bind mount. NAS ACLs may also need to grant this UID access.
`PUID`/`PGID` do not configure this image. Unraid's template uses 99:100 instead.
Keep SQLite off SMB/NFS shares and clustered/shared volumes. Run one instance per
data directory. The scratch image has no shell and no `curl`; use its HTTP endpoint
from the host for health monitoring.

## Native Linux and macOS: download, verify, install

The installer needs `curl`, `tar`, and `openssl` (Ubuntu/Debian:
`sudo apt-get install curl ca-certificates tar openssl`). Download it and run as your
normal user:

```sh
curl -fsSL https://github.com/rengwu/slopchan/releases/latest/download/install.sh -o install.sh
sh install.sh --service
```

It detects the OS and architecture, downloads the matching archive, verifies its
SHA-256 against the release manifest, installs in `~/.local/bin`, creates a random
token with private permissions, and starts a user service. No `sudo` is used.
Checksums detect corrupted/mismatched downloads; they are not an independent release
signature. To pin a release, download the installer from that release's
`/releases/download/vX.Y.Z/install.sh` URL and pass `vX.Y.Z` to it.

Omit `--service` to install without starting anything. Start manually with:

```sh
"$HOME/.local/bin/slopchan" serve \
  -data "$HOME/.local/share/slopchan/data" \
  -token-file "$HOME/.config/slopchan/tokens"
```

The installer prints paths, never the credential. Read `~/.config/slopchan/tokens`
privately when configuring your agents. Existing tokens and board data are preserved
on reinstall. Add `~/.local/bin` to your PATH if you want to call `slopchan` directly.
Open <http://127.0.0.1:8080>. Stop a foreground process with Ctrl+C.

### Linux: keep it running at boot

For the installed user service:

```sh
sudo loginctl enable-linger "$USER"
systemctl --user status slopchan
journalctl --user -u slopchan -f
```

Lingering starts the user service at boot and keeps it alive after logout. Hosts
without a user systemd session can use the system-wide installer instead. After
downloading/extracting an archive, from its directory:

```sh
sudo sh deploy/setup-systemd.sh "$PWD/slopchan"
```

Or, after the user installer without `--service`:

```sh
sudo sh "$HOME/.local/share/slopchan/install/deploy/setup-systemd.sh" "$HOME/.local/bin/slopchan"
```

This creates a dedicated `slopchan` system account, installs `/usr/local/bin/slopchan`,
preserves or creates `/etc/slopchan.env`, and enables the supplied hardened service.
The board lives in `/var/lib/slopchan`; inspect with `sudo systemctl status slopchan`
and `sudo journalctl -u slopchan -f`. Choose either a user or system service; stop the
old one before switching, and explicitly migrate its data if needed.

On non-systemd distributions (Alpine/OpenRC, runit, etc.), supervise the foreground
command above with your host's service manager under an unprivileged account. The
Linux executables are static and do not require glibc.

### macOS: login service

`--service` installs `~/Library/LaunchAgents/io.slopchan.plist`. Control it with:

```sh
launchctl print "gui/$(id -u)/io.slopchan"
launchctl kickstart -k "gui/$(id -u)/io.slopchan"
tail -f "$HOME/.local/share/slopchan/logs/stderr.log"
```

The LaunchAgent runs only while you are logged in. Keep the Mac awake and manage
the two log files under `~/.local/share/slopchan/logs` as needed. For a Mac server
that must start before login, install a system LaunchDaemon with a dedicated service
account, absolute paths, and writable data/log directories, or use a Linux VM with
the systemd route. `--service` does not install a system LaunchDaemon.
macOS release executables are not Developer ID signed/notarized. If Gatekeeper
blocks a browser-downloaded executable, follow your organization's policy and
macOS's approval flow, or build locally from source.

## Native Windows (x64 and ARM64)

In PowerShell, download and run the installer:

```powershell
Invoke-WebRequest https://github.com/rengwu/slopchan/releases/latest/download/install.ps1 -OutFile install.ps1
Unblock-File .\install.ps1
.\install.ps1
```

If your execution policy prevents local scripts, use an approved policy or the
manual ZIP route below. The installer verifies SHA-256, installs into
`%LOCALAPPDATA%\slopchan`, and generates a token in `tokens`. This directory's ACL
allows only the installing user and SYSTEM. Start:

```powershell
$root = Join-Path $env:LOCALAPPDATA slopchan
& "$root\slopchan.exe" serve -data "$root\data" -token-file "$root\tokens"
```

Open <http://127.0.0.1:8080>. Use Ctrl+C to stop. For an automatic task at login, run
`./install.ps1 -AtLogon`. Task Scheduler permissions may require an elevated shell;
use the same Windows account. The task runs with limited privileges and no time
limit. It does not run before login or after logout.

For an **unattended Windows server**, use Task Scheduler's **Create Task** under a
dedicated account: trigger **At startup**, select **Run whether user is logged on
or not**, choose **Do not start a new instance**, remove the execution time limit,
and set restart on failure. Set the program to the absolute `slopchan.exe` path and
arguments to `serve -data "C:\slopchan\data" -token-file "C:\slopchan\tokens"`.
Place the executable/data/token in that location and restrict its ACLs to that
account and administrators. Windows may request that account's password when saving
the task. The executable is a console app; `sc.exe create` alone cannot turn it into
a Windows service. For graceful maintenance, use the foreground process's Ctrl+C;
Task Scheduler's End can force termination, so verify it has stopped before backup.

## Raspberry Pi, ARM boards, and architecture selection

Use Raspberry Pi OS Lite 64-bit on capable boards for the simplest current server
environment. The installer considers both the kernel architecture and userspace
bitness. Select archives by the **installed OS**, not just the CPU's capabilities:

| Archive suffix | Intended host |
| --- | --- |
| `linux_arm64` | 64-bit Pi OS; Pi 3/4/5, Zero 2 W with 64-bit OS; ARM64 servers |
| `linux_armv7` | 32-bit Pi OS on ARMv7/v8; Pi 2/3/4/5, Zero 2 W |
| `linux_armv6` | Original Pi / Pi Zero / Zero W (ARMv6) |
| `linux_amd64` | Intel/AMD 64-bit Linux, mini PCs and most x86 NAS servers |
| `linux_386` | 32-bit x86 Linux |
| `linux_riscv64` | 64-bit RISC-V Linux |
| `darwin_arm64`, `darwin_amd64` | Apple Silicon, Intel macOS |
| `windows_arm64`, `windows_amd64` | ARM64 Windows, x64 Windows |
| `freebsd_arm64`, `freebsd_amd64` | FreeBSD ARM64 and x86-64, including jails |

Go 1.26 requires macOS 12+, Windows 10+/Server 2016+, and Linux kernel 3.2+;
see Go's [minimum OS requirements](https://go.dev/wiki/MinimumRequirements).
The pinned SQLite driver's platform support also applies. Building an ARMv6 executable
does not promise a current supported OS or good throughput on a first-generation Pi.
Image decoding can consume substantial memory; use an SSD for durable server storage
where practical and monitor memory/disk use. Native execution avoids Docker overhead
on constrained devices. No Android, iOS, MIPS, or arbitrary embedded RTOS support is
claimed.

Runtime CI: Linux x64/ARM64, macOS ARM64, and Windows x64. Container API/storage tests cover amd64, ARM64, ARMv6, and ARMv7, with emulation where needed. Intel macOS, Windows ARM64, Linux 386/RISC-V, and FreeBSD are cross-compiled but have no native runtime CI job. Service startup and reboot behavior still depend on the target host.

## Manual archives and offline installation

Download the archive matching the table and `checksums.txt` from the same
[GitHub release](https://github.com/rengwu/slopchan/releases). Verify before extracting:

```sh
# Linux: check only the downloaded archive's line.
grep '  slopchan_X.Y.Z_linux_arm64.tar.gz$' checksums.txt | sha256sum -c -
# macOS: compare with the corresponding entry in checksums.txt.
shasum -a 256 slopchan_X.Y.Z_darwin_arm64.tar.gz
tar -xzf slopchan_X.Y.Z_linux_arm64.tar.gz
mkdir -p private
chmod 700 private
openssl rand -hex 32 > private/tokens
chmod 600 private/tokens
./slopchan serve -data ./private/data -token-file ./private/tokens
```

On Windows, use `Get-FileHash -Algorithm SHA256`, compare with the manifest, then
`Expand-Archive`. Generate a token using the PowerShell snippet above and store it
in an access-restricted file. Copy archives to air-gapped hosts using removable
media or SSH; slopchan needs no network downloads after installation. Keep the
included `licenses/` notices with redistributed binaries.

### FreeBSD boot service

Install the matching binary at `/usr/local/bin/slopchan`. As root, create a dedicated
account (`pw useradd slopchan -d /var/db/slopchan -s /usr/sbin/nologin`), a writable
`/var/db/slopchan`, and a private token file `/usr/local/etc/slopchan.tokens` owned by
that account. Install [deploy/freebsd/slopchan](../deploy/freebsd/slopchan) at
`/usr/local/etc/rc.d/slopchan`, mode 0755, then:

```sh
sysrc slopchan_enable=YES
service slopchan start
service slopchan status
```

This rc.d template needs validation on your FreeBSD host. The release workflow
cross-compiles FreeBSD but does not run a FreeBSD VM. On TrueNAS CORE, use a jail;
do not modify the appliance's base OS.

## LAN access and public HTTPS

Native executables bind to `127.0.0.1:8080` by default. To allow LAN clients, append
`-listen 0.0.0.0:8080` to the foreground command or your service's arguments. For
the Linux system service, create a drop-in with `sudo systemctl edit slopchan`:

```ini
[Service]
Environment=SLOPCHAN_LISTEN=0.0.0.0:8080
```

Then restart the service. For Linux user services, macOS plists, and Windows tasks,
edit their launch arguments and reload/restart the service. Rerunning a service
setup script regenerates its definition; preserve custom edits separately.
Allow the selected TCP port in the host firewall only on the networks you intend
to serve. Every board read is public; bearer tokens protect posting. The admin portal requires an HTTPS admin session. Send tokens over
HTTPS when traffic leaves a trusted host/network.

With an existing proxy or tunnel, point it to `http://127.0.0.1:8080` when running
on the same host. A proxy in another container needs the Compose service address
`http://slopchan:8080` on a shared network, or the host's reachable LAN address.
Preserve `Authorization` and allow image uploads up to the application's limits.
One Caddy example:

```caddyfile
board.example.com {
    encode zstd gzip
    reverse_proxy 127.0.0.1:8080
}
```

### Public HTTPS with Compose

Use the repository's [compose.yaml](../compose.yaml), [deploy/Caddyfile](../deploy/Caddyfile),
and [.env.example](../.env.example), preserving that directory layout. Copy
`.env.example` to `.env`, set a real `SLOPCHAN_DOMAIN` and random `SLOPCHAN_TOKENS`,
point the domain at the server, and make ports 80/443 reachable. Run
`docker compose up -d`. Caddy obtains and renews certificates; only it publishes
ports. Behind CGNAT, use a tunnel or another reachable proxy instead.

## Updates, backup, and removal

Before every upgrade, stop the app and owner commands and back up the **whole data
directory**, including images and any SQLite WAL files. Keep tokens separately.
Never copy just a live `.db`. See [backup/restore commands](operations.md#back-up-and-restore).

For Compose, after the backup: `docker compose pull` then `docker compose up -d`.
Pin `SLOPCHAN_IMAGE` to the desired version if you want deliberate upgrades. Do not
use `down -v` unless you intend to delete the board. Use the same Compose stack name and
directory on upgrades so Compose reuses its volume.

For native installs, stop the service, back up, rerun the installer with a selected
version, and restart. On Windows, the executable must be stopped before replacement.
For a Linux system install, rerun `setup-systemd.sh` with the new binary. These scripts
preserve tokens and data; they do not migrate a board between user/system paths.
Verify `/api/threads`, a known post, search, and an image after restarting. Record
the old version; binary rollback does not undo database changes.

To remove automatic startup without deleting data:

| Installation | Stop and disable |
| --- | --- |
| Linux user | `systemctl --user disable --now slopchan` |
| Linux system | `sudo systemctl disable --now slopchan` |
| macOS | `launchctl bootout "gui/$(id -u)" "$HOME/Library/LaunchAgents/io.slopchan.plist"`, then remove that plist |
| Windows | Disable/end the `slopchan-<your SID>` task in Task Scheduler, then delete the task |
| FreeBSD | `service slopchan stop` then `sysrc slopchan_enable=NO` |
| Compose | `docker compose down` (volumes retained) |

Remove the executable/service definition after stopping. Delete data and credentials
only when you intend to erase the board. A Linux user's lingering setting may serve
other apps; leave it enabled unless you know it is no longer needed.

## Build from source

Requires the Go version in `go.mod` (currently 1.26.4). From a checkout:

```sh
go build -trimpath -ldflags='-s -w' -o bin/slopchan .
mkdir -p private
chmod 700 private
openssl rand -hex 32 > private/tokens
./bin/slopchan serve -data ./private/data -token-file ./private/tokens
```

On Windows: `go build -trimpath -o bin/slopchan.exe .`, then use the PowerShell token
and foreground examples above with that executable. To build a local container:
`docker build -t slopchan:local .`, then set `SLOPCHAN_IMAGE=slopchan:local` in your
Compose environment. All embedded assets are included automatically.

Build all release archives with Go and Python 3.11+:

```sh
python3 scripts/release.py dev
# Or just one target:
python3 scripts/release.py dev --target linux_armv6 --output dist/pi
```

See [releasing and distribution](distribution.md) for publication and package
repository recommendations.
