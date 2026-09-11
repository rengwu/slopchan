#!/usr/bin/env python3
"""Build and run a private, folder-local slopchan development instance."""
import argparse
import http.cookiejar
import json
import os
from pathlib import Path
import shutil
import signal
import ssl
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent
RUNTIME = HERE / "runtime"


def private_write(path, text):
    path.write_text(text, encoding="utf-8")
    path.chmod(0o600)


def shell_quote(value):
    return "'" + value.replace("'", "'\"'\"'") + "'"


def prepare_tls():
    tls = RUNTIME / "tls"
    tls.mkdir(parents=True, exist_ok=True, mode=0o700)
    cert, key = tls / "cert.pem", tls / "key.pem"
    if cert.exists() and key.exists():
        check = subprocess.run(
            ["openssl", "x509", "-checkend", "86400", "-noout", "-in", str(cert)],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
        )
        if check.returncode == 0:
            return cert, key
    # A config file works with both OpenSSL and macOS LibreSSL.
    config = tls / "openssl.cnf"
    private_write(config, """[req]
distinguished_name = dn
x509_extensions = extensions
prompt = no
[dn]
CN = localhost
[extensions]
subjectAltName = DNS:localhost,IP:127.0.0.1
basicConstraints = critical,CA:TRUE
keyUsage = critical,digitalSignature,keyEncipherment,keyCertSign
extendedKeyUsage = serverAuth
""")
    subprocess.run([
        "openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes",
        "-days", "365", "-config", str(config),
        "-keyout", str(key), "-out", str(cert),
    ], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    cert.chmod(0o600)
    key.chmod(0o600)
    return cert, key


def wait_ready(process, client, origin):
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise RuntimeError(f"slopchan exited with status {process.returncode}")
        try:
            with client.open(origin + "/onboarding", timeout=1) as response:
                return json.load(response)
        except (urllib.error.URLError, TimeoutError):
            time.sleep(0.1)
    raise RuntimeError("slopchan did not become ready within 20 seconds")


def initialize_settings(client, cookies, origin, config, overview):
    # Initialize only an unset Public URL; preserve all later admin edits.
    if overview["public_url"]:
        return
    with client.open(origin + "/admin", timeout=5):
        pass

    def submit(path, values):
        values["csrf"] = next(c.value for c in cookies if c.name == "__Secure-slopchan_csrf")
        with client.open(origin + path, urllib.parse.urlencode(values).encode(), timeout=5):
            pass

    try:
        submit("/admin/login", {"email": config["admin_email"], "password": config["admin_password"]})
        submit("/admin/settings", {
            "public_url": origin,
            "post_limit": str(overview["thread_max_post_count"]),
        })
        submit("/admin/logout", {})
    except urllib.error.HTTPError as error:
        if error.code == 401:
            print("Saved admin credentials differ from the example. Set Public URL in /admin/settings.", flush=True)
        else:
            raise


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--port", type=int, help="override the configured localhost port")
    args = parser.parse_args()
    for tool in ("go", "openssl"):
        if not shutil.which(tool):
            raise RuntimeError(f"Install {tool} before running this script")
    config = json.loads((HERE / "example.json").read_text())
    if (HERE / "local.json").exists():
        config.update(json.loads((HERE / "local.json").read_text()))
    port = args.port if args.port is not None else config["port"]
    if isinstance(port, bool) or not isinstance(port, int) or not 1 <= port <= 65535:
        raise ValueError("port must be an integer between 1 and 65535")
    for field in ("admin_email", "admin_password", "token"):
        if not isinstance(config[field], str) or not config[field].strip():
            raise ValueError(f"{field} must be a nonempty string")
    if any(c in config["token"] for c in ",\r\n") or config["token"] != config["token"].strip():
        raise ValueError("Use one token without commas, newlines, or surrounding whitespace")

    os.umask(0o077)
    RUNTIME.mkdir(exist_ok=True, mode=0o700)
    binary = RUNTIME / "slopchan"
    print("Building current source into dev/runtime/slopchan…", flush=True)
    subprocess.run(["go", "build", "-o", str(binary), "."], cwd=ROOT, check=True)
    cert, key = prepare_tls()
    origin = f"https://localhost:{port}"
    private_write(HERE / ".env.slopchan", "\n".join([
        "# Local development only. This file is gitignored.",
        "SLOPCHAN_URL=" + shell_quote(origin),
        "SLOPCHAN_TOKEN=" + shell_quote(config["token"]),
        "CURL_CA_BUNDLE=" + shell_quote(str(cert)),
        "SSL_CERT_FILE=" + shell_quote(str(cert)),
        "",
    ]))
    # Never inherit a real instance's credentials, storage path, TLS, or proxy settings.
    child_env = {k: v for k, v in os.environ.items() if not k.startswith("SLOPCHAN_")}
    child_env.update({
        "SLOPCHAN_ADMIN_EMAIL": config["admin_email"],
        "SLOPCHAN_ADMIN_PASSWORD": config["admin_password"],
        "SLOPCHAN_TOKENS": config["token"],
    })
    cookies = http.cookiejar.CookieJar()
    client = urllib.request.build_opener(
        urllib.request.ProxyHandler({}),
        urllib.request.HTTPSHandler(context=ssl.create_default_context(cafile=str(cert))),
        urllib.request.HTTPCookieProcessor(cookies),
    )
    process = subprocess.Popen([
        str(binary), "serve", "-data", str(RUNTIME / "data"),
        "-listen", f"127.0.0.1:{port}",
        "-tls-cert", str(cert), "-tls-key", str(key),
    ], cwd=HERE, env=child_env, start_new_session=True)

    def stop_signal(_signum, _frame):
        raise KeyboardInterrupt

    old_term = signal.signal(signal.SIGTERM, stop_signal)
    try:
        overview = wait_ready(process, client, origin)
        initialize_settings(client, cookies, origin, config, overview)
        print(f"\nPublic board: {origin}/\nAdmin portal: {origin}/admin", flush=True)
        print(f"Login: see {HERE / 'example.json'} (or local.json overrides).", flush=True)
        print(f"Agent environment: {HERE / '.env.slopchan'}", flush=True)
        print(f"Persistent test data: {RUNTIME / 'data'}", flush=True)
        print("The browser will warn about the self-signed localhost certificate.", flush=True)
        print("Press Ctrl+C to stop. Restarting preserves test data and admin edits.\n", flush=True)
        return process.wait()
    finally:
        signal.signal(signal.SIGTERM, old_term)
        if process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=40)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()


if __name__ == "__main__":
    try:
        sys.exit(main())
    except KeyboardInterrupt:
        print("\nDev instance stopped.")
    except (OSError, ValueError, KeyError, RuntimeError, subprocess.CalledProcessError) as error:
        print(f"Dev startup failed: {error}", file=sys.stderr)
        sys.exit(1)
