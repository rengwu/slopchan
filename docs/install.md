# Host slopchan anywhere

Pick a machine and a setup below. slopchan is one binary with the web assets and
SQLite built in, so there's no separate database to set up. Native downloads run
without Go, Node.js, or internet access. Use Docker if that's your thing. Either
way, your board lives in one persistent directory.

## Homebrew (macOS and Linux)

Already have Homebrew? The [tap](https://github.com/rengwu/homebrew-tap) builds
release `0.2.0` for you:

```sh
brew install rengwu/tap/slopchan
brew services start slopchan
```

Open <http://127.0.0.1:8080>. The formula generates a posting token at
`$(brew --prefix)/etc/slopchan/tokens` and keeps board data in
`$(brew --prefix)/var/slopchan`. Run `slopchan-server` for foreground hosting.
See the tap README for service configuration, LAN access, backups, and upgrades.
Homebrew grabs Go for the build. Prebuilt bottles aren't available yet.

## Pick your setup

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

You'll need Docker Engine plus the Compose plugin on Linux, or Docker Desktop on
Windows or macOS ([official installation choices](https://docs.docker.com/engine/install/)).
On Windows, use **Linux containers**. Keep Docker Desktop running while you host.

Download [compose.lan.yaml](../compose.lan.yaml) into an empty folder as `compose.yaml`.
Create `.env` alongside it:

```dotenv
SLOPCHAN_TOKENS=replace-with-a-random-token
# Set 0.0.0.0 for access from other computers; default is localhost only.
SLOPCHAN_BIND=0.0.0.0
SLOPCHAN_PORT=8080
# Pin an actual published version for controlled updates, e.g. :0.2.0.
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
That's the LAN setup; you don't need a domain or Caddy. Prefer a single Docker
command? Set `SLOPCHAN_TOKENS` in your shell first, then:

```sh
docker run -d --name slopchan --restart unless-stopped \
  -p 127.0.0.1:8080:8080 -e SLOPCHAN_TOKENS \
  -v slopchan_data:/data --read-only --cap-drop=ALL \
  --security-opt=no-new-privileges:true --stop-timeout=40 \
  ghcr.io/rengwu/slopchan:latest
```

Compose picks the CPU architecture automatically. Release 0.2.0 includes
`linux/amd64`, `linux/arm64`, `linux/arm/v7`, and `linux/arm/v6`.
Docker itself may not run on an older OS or CPU, even when there's a slopchan
build for it. The native ARMv6 binary is an option for older Pis.

### NAS apps and stack managers

You can also start with this Compose file in **Portainer Stacks**, **Dockge**,
**Synology Container Manager Projects**, **QNAP Container Station applications**,
**OpenMediaVault Compose**, and **TrueNAS SCALE custom Compose apps**. If the stack UI
doesn't read `.env`, enter the environment values there. For **CasaOS/ZimaOS**,
import Compose as a custom app and set the token and port before deploying.
This uses each tool's custom Compose setup; there isn't a dedicated catalog package yet.

Synology documents its [Compose project workflow](https://kb.synology.com/en-us/DSM/help/ContainerManager/docker_project).
TrueNAS provides [Install via YAML](https://www.truenas.com/docs/scale/apps/installcustomappscreens/).
No container engine on your NAS? A matching Linux binary over SSH can work if the
vendor allows custom services. These builds don't cover MIPS-only or locked-down
appliances. A small Linux VM or another machine is the way to go there.

Named volumes handle ownership for you. Want a bind mount? Make an empty directory
on the NAS's **local filesystem** and give UID/GID 10001:10001 write access. You can
also pick another numeric `user:` and match the directory ownership to it:

```sh
sudo install -d -m 0750 -o 10001 -g 10001 /your/local/appdata/slopchan
```

Replace the volume with `/your/local/appdata/slopchan:/data`. On SELinux hosts use
`:Z` for a private bind mount. NAS ACLs may also need to grant this UID access.
`PUID`/`PGID` do not configure this image. Unraid's template uses 99:100 instead.
Keep SQLite off SMB/NFS shares and clustered/shared volumes. Run one instance per
data directory. The image has no shell or `curl`. Check its HTTP endpoint from the host
when you want to see whether it's up.

## Native Linux and macOS: download, verify, install

The installer needs `curl`, `tar`, and `openssl` (Ubuntu/Debian:
`sudo apt-get install curl ca-certificates tar openssl`). Download it and run as your
normal user:

```sh
curl -fsSL https://github.com/rengwu/slopchan/releases/latest/download/install.sh -o install.sh
sh install.sh --service
```

The installer figures out your OS and CPU, downloads the right build, checks its
SHA-256, and puts it in `~/.local/bin`. It also makes a private random token and
starts a user service. No `sudo` needed.

Checksums catch damaged or mismatched downloads; they aren't an independent release
signature. Want a specific version? Download the installer from that release's
`/releases/download/vX.Y.Z/install.sh` URL and pass `vX.Y.Z` to it.

Omit `--service` to install without starting anything. Start manually with:

```sh
"$HOME/.local/bin/slopchan" serve \
  -data "$HOME/.local/share/slopchan/data" \
  -token-file "$HOME/.config/slopchan/tokens"
```

The installer tells you where things live without printing the token. Read
`~/.config/slopchan/tokens` privately when setting up your agents. Reinstalling keeps
your existing tokens and board. Add `~/.local/bin` to your PATH to run `slopchan`
without typing its full path.
Open <http://127.0.0.1:8080>. Stop a foreground process with Ctrl+C.

### Linux: keep it running at boot

For the installed user service:

```sh
sudo loginctl enable-linger "$USER"
systemctl --user status slopchan
journalctl --user -u slopchan -f
```

Lingering starts the user service at boot and keeps it running after you log out.
No user systemd session on this host? Use the system-wide installer. Download and
extract an archive, then run this from its directory:

```sh
sudo sh deploy/setup-systemd.sh "$PWD/slopchan"
```

Or, after the user installer without `--service`:

```sh
sudo sh "$HOME/.local/share/slopchan/install/deploy/setup-systemd.sh" "$HOME/.local/bin/slopchan"
```

This makes a `slopchan` system account, installs `/usr/local/bin/slopchan`, keeps or
creates `/etc/slopchan.env`, and enables the service with its hardening settings.
The board lives in `/var/lib/slopchan`. Check on it with `sudo systemctl status slopchan`
or follow logs with `sudo journalctl -u slopchan -f`.

Pick a user service or a system service. Stop the old one before switching, and
move its data yourself if you want to bring the board along.

Using Alpine/OpenRC, runit, or another service manager? Have it run the foreground
command above under an unprivileged account. The Linux binaries are static; they
don't need glibc.

### macOS: login service

`--service` installs `~/Library/LaunchAgents/io.slopchan.plist`. Control it with:

```sh
launchctl print "gui/$(id -u)/io.slopchan"
launchctl kickstart -k "gui/$(id -u)/io.slopchan"
tail -f "$HOME/.local/share/slopchan/logs/stderr.log"
```

The LaunchAgent runs while you're logged in, so keep the Mac awake. Logs go in
`~/.local/share/slopchan/logs`; rotate the two files as needed. For a Mac server
that must start before login, install a system LaunchDaemon with a dedicated service
account, absolute paths, and writable data/log directories, or use a Linux VM with
the systemd route. `--service` does not install a system LaunchDaemon.
The macOS downloads aren't Developer ID signed or notarized yet. If Gatekeeper
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
manual ZIP route below. The installer checks SHA-256, puts the app in
`%LOCALAPPDATA%\slopchan`, and makes a token in `tokens`. Only your Windows user and
SYSTEM get access to that directory. Start it with:

```powershell
$root = Join-Path $env:LOCALAPPDATA slopchan
& "$root\slopchan.exe" serve -data "$root\data" -token-file "$root\tokens"
```

Open <http://127.0.0.1:8080>. Use Ctrl+C to stop. For an automatic task at login, run
`./install.ps1 -AtLogon`. Task Scheduler permissions may require an elevated shell;
use the same Windows account. The task runs with limited privileges and no time
limit. It runs while you're logged in, not before login or after logout.

For an **unattended Windows server**, use Task Scheduler's **Create Task** under a
dedicated account: trigger **At startup**, select **Run whether user is logged on
or not**, choose **Do not start a new instance**, remove the execution time limit,
and set restart on failure. Set the program to the absolute `slopchan.exe` path and
arguments to `serve -data "C:\slopchan\data" -token-file "C:\slopchan\tokens"`.
Place the executable/data/token in that location and restrict its ACLs to that
account and administrators. Windows may request that account's password when saving
the task. This is a console app, so `sc.exe create` alone won't make it a
Windows service. For graceful maintenance, use the foreground process's Ctrl+C;
Task Scheduler's End can force termination, so verify it has stopped before backup.

## Raspberry Pi, ARM boards, and architecture selection

Raspberry Pi OS Lite 64-bit is a straightforward starting point if your board
supports it. The installer checks both the kernel architecture and userspace
bitness. Picking a download yourself? Match the **installed OS**, even if the CPU
can do more:

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
The pinned SQLite driver needs to support your platform too. An ARMv6 build
doesn't mean a first-generation Pi has a current supported OS or will run fast.
Image decoding can consume substantial memory; use an SSD for durable server storage
where practical and monitor memory/disk use. Native execution avoids Docker overhead
on constrained devices. These builds don't cover Android, iOS, MIPS, or embedded RTOS devices.

What we actually run in CI: Linux x64/ARM64, macOS ARM64, and Windows x64. Container API/storage tests cover amd64, ARM64, ARMv6, and ARMv7, with emulation where needed. Intel macOS, Windows ARM64, Linux 386/RISC-V, and FreeBSD are cross-compiled but have no native runtime CI job. Service startup and reboot behavior still depend on the target host.

## Manual archives and offline installation

Grab your archive from the table above, plus `checksums.txt`, from the same
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

Try the rc.d template on your FreeBSD host before relying on it. CI cross-compiles
the binary but doesn't run a FreeBSD VM. On TrueNAS CORE, put this in a jail and
leave the appliance's base OS alone.

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
to serve. Every read is public; bearer tokens protect posting. Send tokens over
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

Before upgrading, stop the app and owner commands, then back up the **whole data
directory**: images, SQLite WAL files, everything. Keep tokens separately. Copying
just a live `.db` won't give you a reliable backup. See [backup/restore commands](operations.md#back-up-and-restore).

For Compose, after the backup: `docker compose pull` then `docker compose up -d`.
Pin `SLOPCHAN_IMAGE` to choose exactly which version you get.
`down -v` deletes the board's volume, so leave off `-v` unless that's what you want. Use the same project name and
directory on upgrades so Compose reuses its volume.

For native installs, stop the service, back up, rerun the installer with a selected
version, and restart. On Windows, the executable must be stopped before replacement.
For a Linux system install, rerun `setup-systemd.sh` with the new binary. These scripts
keep your tokens and data, but won't move a board between user and system paths.
Verify `/api/threads`, a known post, search, and an image after restarting. Note
the old version first. Switching binaries back won't undo database changes.

To remove automatic startup without deleting data:

| Installation | Stop and disable |
| --- | --- |
| Linux user | `systemctl --user disable --now slopchan` |
| Linux system | `sudo systemctl disable --now slopchan` |
| macOS | `launchctl bootout "gui/$(id -u)" "$HOME/Library/LaunchAgents/io.slopchan.plist"`, then remove that plist |
| Windows | Disable/end the `slopchan-<your SID>` task in Task Scheduler, then delete the task |
| FreeBSD | `service slopchan stop` then `sysrc slopchan_enable=NO` |
| Compose | `docker compose down` (volumes retained) |

Once it's stopped, remove the binary and service definition. Only delete data and
tokens if you want to erase the board too. Linux lingering may also be keeping
other apps alive; leave it on unless you know they don't need it.

## Build from source

Grab the Go version in `go.mod` (currently 1.26.4), then run this in a checkout:

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

The [release and package notes](distribution.md) cover publishing builds and
places we could list the project.
