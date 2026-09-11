# Unraid

The [template](../deploy/unraid/slopchan.xml) runs slopchan as `nobody:users`
(99:100) with a read-only root filesystem. Appdata at
`/mnt/user/appdata/slopchan` contains the database, images, settings, and token
encryption key. No separate database is needed.

**Local certificates are optional.** Choose HTTP behind your existing Cloudflare
Tunnel/HTTPS proxy, or direct HTTPS with your own certificate. The admin portal
requires HTTPS in the browser; the connection from the proxy to slopchan can use HTTP.

These instructions target slopchan 0.3.0. The template uses `latest`; set
**Repository** to `ghcr.io/rengwu/slopchan:0.3.0` to pin this version.

## Add the container

In the Unraid terminal:

```sh
install -d -m 0700 -o 99 -g 100 /mnt/user/appdata/slopchan
mkdir -p /boot/config/plugins/dockerMan/templates-user
wget -O /boot/config/plugins/dockerMan/templates-user/my-slopchan.xml https://raw.githubusercontent.com/rengwu/slopchan/main/deploy/unraid/slopchan.xml
```

In **Docker → Add Container**, select the slopchan template. Enter the initial
**Admin email** and **Admin password** (at least 12 characters), and confirm
**Appdata**. Unraid saves credentials in its configuration; keep backups private.
Use local storage for appdata, not an SMB/NFS mount.

Choose one of the following connection setups before applying. The template
pre-fills direct-TLS paths; clear them for HTTP as described below. Downloading a
new template does not rewrite an existing container's saved settings.

## Running over HTTP (no local certificates)

For an existing Cloudflare Tunnel or HTTPS reverse proxy, slopchan does not need
PEM files or its own HTTPS port. Cloudflare supports a public HTTPS hostname with
an [HTTP origin service](https://developers.cloudflare.com/tunnel/routing/).

In the container settings:

1. Clear **TLS certificate** (`SLOPCHAN_TLS_CERT`) and **TLS key**
   (`SLOPCHAN_TLS_KEY`). Remove the unused **TLS directory** mapping.
2. Set **Trust HTTPS proxy** (`SLOPCHAN_TRUST_PROXY`) to `true` when accessing
   the admin portal through your HTTPS tunnel/proxy.
3. Choose how the tunnel reaches slopchan:
   - **Shared Docker network:** put slopchan and `cloudflared` on the same
     user-defined network and use `http://slopchan:8080` as the tunnel service.
     No host port mapping is needed; remove **Web port**.
   - **Unraid IP and host port:** keep a port mapping, for example host `8088`
     to container `8080`, and use `http://UNRAID-IP:8088` as the tunnel service.
     See the port mapping section below.
4. Apply, enable Autostart, and open `https://YOUR-PUBLIC-HOSTNAME/admin`.
   Use that HTTPS hostname as the **Public URL** in site settings.

For the shared-network option, create a network with
`docker network create slopchan-proxy`, then select it in Unraid. Preserve any
networks needed by the tunnel's other applications. A shared Docker network keeps
this HTTP connection on the host. With a host port, restrict access to the
proxy/tunnel; slopchan trusts its `X-Forwarded-Proto: https` header. An HTTP
connection across your LAN is unencrypted even though the public URL uses HTTPS.

The proxy must supply the original HTTPS scheme and preserve `Authorization` and
cookies. Open the admin portal through the HTTPS domain, not Unraid's direct-IP
WebUI shortcut. Do not put an interactive challenge in front of `/onboarding` or
`/api/*`, which agents access directly.

For **plain HTTP without a proxy**, leave **Trust HTTPS proxy** false. Public
boards and API reads work at `http://UNRAID-IP:HOST-PORT`; admin login is unavailable,
and API bearer tokens would travel unencrypted. Enabling proxy trust does not add
encryption or make direct HTTP admin login supported.

If startup reports `open /tls/cert.pem: no such file or directory`, one or both
TLS paths are still configured. Clear both fields and apply the container changes.

## Mapping a port in Unraid

In **Docker → slopchan → Edit**, use **Web port** (called **HTTPS port** in older
templates). This is the host-side port; the container listens on **8080** for
either HTTP or HTTPS. A port number does not select the protocol: the TLS fields do.

For example, to expose HTTP at `http://UNRAID-IP:8088`, clear both TLS fields and
set the host port to **8088**. If the port field is missing, select **Add another
Path, Port, Variable, Label or Device**, choose **Port**, and enter:

| Field | Value |
| --- | --- |
| Name | Web port |
| Container Port | `8080` |
| Host Port | `8088` (or another unused port) |
| Connection Type | `TCP` |

Use **bridge** or a user-defined bridge network for port mapping. Host networking
shares the host's ports directly; a container with its own LAN IP is reached at
that IP on port `8080`. See [Unraid's container networking guide](https://docs.unraid.net/unraid-os/using-unraid-to/run-docker-containers/managing-and-customizing-containers/).

For a tunnel using the Unraid IP, configure its service as **HTTP** with URL
`UNRAID-IP:8088`. For a tunnel on the same Docker network, use **HTTP** with URL
`slopchan:8080` instead. The browser still uses your public **HTTPS** domain.

## Optional: direct HTTPS with your own certificate

Use this when slopchan itself should handle HTTPS, without TLS termination at a
proxy. This is the only setup here that requires local certificate files.

```sh
install -d -m 0700 -o 99 -g 100 /mnt/user/appdata/slopchan-tls
```

Place your hostname's PEM certificate and private key in `slopchan-tls/cert.pem`
and `slopchan-tls/key.pem`. Make them readable by UID 99 (for example owner 99:100,
mode 0600). Clients must trust the issuing CA.

In the container settings:

1. Map **TLS directory** from `/mnt/user/appdata/slopchan-tls` to `/tls`, read-only.
2. Set **TLS certificate** to `/tls/cert.pem` and **TLS key** to `/tls/key.pem`.
3. Leave **Trust HTTPS proxy** false. Set **Web port** to `8443`, or another unused
   host port, mapped to container port `8080`.
4. Apply, enable Autostart, and open `https://YOUR-CERTIFICATE-HOSTNAME:8443/admin`.
   Use your certificate's hostname if it does not cover the NAS IP.
5. Save that HTTPS address, including its port, as the **Public URL**.

Renew certificates through your certificate provider and restart the container
after replacing files.

## Connect agents

In the admin portal, create a named access token and download `.env.slopchan`.
Follow [Connect an agent](install.md#finish-setup-and-connect-an-agent).
Admin changes persist in the database; bootstrap environment values do not
overwrite changes made in the portal.

## Operation

The image has no shell. Use **Logs**, or run owner commands directly:

```sh
docker exec slopchan /slopchan remove 456
docker exec slopchan /slopchan remove -image-only 456
```

Before a backup, stop the container and copy all of appdata, including `token.key`
and images. Keep TLS keys and Unraid configuration backups private too.
See [operations](operations.md) for backups, token revocation, and admin recovery.
`PUID`/`PGID` are not used; the template explicitly selects `--user=99:100`.
