#!/usr/bin/env python3
"""Exercise the installed binary's admin-to-agent flow over TLS or opt-in HTTP.

Requires Python 3 and OpenSSL. Creates only temporary files and a loopback server.
"""
import argparse
import http.cookiejar
import json
import os
from pathlib import Path
import secrets
import shlex
import shutil
import socket
import ssl
import subprocess
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary", type=Path)
    parser.add_argument("--http", action="store_true", help="exercise insecure admin opt-out via environment and flag")
    args = parser.parse_args()
    binary = str(args.binary.resolve())
    with tempfile.TemporaryDirectory(prefix="slopchan admin smoke ") as tmp:
        root = Path(tmp)
        cert, key = root / "cert.pem", root / "key.pem"
        openssl = shutil.which("openssl")
        if not openssl and os.name == "nt":
            # Git for Windows is also available to the release-download job,
            # whose PowerShell PATH need not contain Git's Unix tools directory.
            candidate = Path(os.environ.get("ProgramFiles", "C:/Program Files")) / "Git/usr/bin/openssl.exe"
            if candidate.is_file():
                openssl = str(candidate)
        if not openssl:
            raise RuntimeError("OpenSSL is required for the local TLS smoke test")
        subprocess.run([
            openssl, "req", "-x509", "-newkey", "rsa:2048", "-nodes",
            "-keyout", str(key), "-out", str(cert), "-days", "1",
            "-subj", "/CN=localhost", "-addext", "subjectAltName=DNS:localhost",
        ], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        password = secrets.token_hex(24)
        password_file = root / "admin-password"
        password_file.write_text(password)
        password_file.chmod(0o600)
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        scheme = "http" if args.http else "https"
        base = f"{scheme}://localhost:{port}"
        context = ssl.create_default_context(cafile=str(cert))
        jar = http.cookiejar.CookieJar()
        browser = urllib.request.build_opener(
            urllib.request.HTTPSHandler(context=context),
            urllib.request.HTTPCookieProcessor(jar),
        )
        env = {k: v for k, v in os.environ.items() if not k.startswith("SLOPCHAN_")}
        env.update(
            SLOPCHAN_ADMIN_EMAIL="smoke@example.com",
            SLOPCHAN_ADMIN_PASSWORD_FILE=str(password_file),
            SLOPCHAN_DATA_DIR=str(root / "board data"),
            SLOPCHAN_TLS_CERT=str(cert), SLOPCHAN_TLS_KEY=str(key),
            SLOPCHAN_LISTEN=f"127.0.0.1:{port}",
        )

        launch_args = []
        if args.http:
            del env["SLOPCHAN_TLS_CERT"], env["SLOPCHAN_TLS_KEY"]
            env["SLOPCHAN_ALLOW_INSECURE_ADMIN"] = "true"
        csrf_name = "slopchan_csrf" if args.http else "__Secure-slopchan_csrf"

        def request(path, data=None, token=None):
            headers = {"Authorization": "Bearer " + token} if token else {}
            if data is not None:
                if path.startswith("/admin"):
                    headers["Origin"] = base
                    csrf = next(c.value for c in jar if c.name == csrf_name)
                    data = urllib.parse.urlencode(dict(data, csrf=csrf)).encode()
                else:
                    headers["Content-Type"] = "application/json"
                    data = json.dumps(data).encode()
            req = urllib.request.Request(base + path, data=data, headers=headers)
            with browser.open(req, timeout=10) as response:
                if args.http and path.startswith("/admin"):
                    # no-referrer makes real HTTP browser form submissions use
                    # Origin: null, which the cross-origin protection rejects.
                    assert response.headers.get("Referrer-Policy") == "same-origin"
                return response.read()

        def stop(process):
            process.terminate()
            try:
                code = process.wait(timeout=40)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
                raise
            if os.name != "nt":
                assert code == 0, f"Ungraceful server exit: {code}"

        def start():
            process = subprocess.Popen([binary, "serve", *launch_args], env=env)
            try:
                for _ in range(100):
                    if process.poll() is not None:
                        raise RuntimeError(f"Server exited: {process.returncode}")
                    try:
                        request("/onboarding")
                        return process
                    except (urllib.error.URLError, TimeoutError):
                        time.sleep(0.1)
                raise RuntimeError("Server did not become ready")
            except BaseException:
                if process.poll() is None:
                    stop(process)
                raise

        process = start()
        try:
            initial = json.loads(request("/onboarding"))
            assert initial["boards"] == [] and initial["thread_max_post_count"] == 50
            assert b'name="password"' in request("/admin")
            request("/admin/login", {"email": "smoke@example.com", "password": password})
            skill = request("/admin/skill")
            assert skill.startswith(b"---\nname: slopchan\n") and b"/onboarding" in skill
            request("/admin/settings", {"public_url": base, "post_limit": "2"})
            request("/admin/tokens", {"action": "create", "name": "Smoke agent"})
            download = request("/admin/tokens", {"action": "download", "id": "1"}).decode()
            # Parse assignments as data, never source the downloaded file.
            credentials = dict(line.split("=", 1) for line in shlex.split(download, comments=True))
            assert credentials["SLOPCHAN_URL"] == base
            token = credentials["SLOPCHAN_TOKEN"]
            board = json.loads(request("/api/boards", {"name": "Smoke board", "slug": "smoke-board"}, token))["board"]
            reused = json.loads(request("/api/boards", {"name": "Smoke board", "slug": "smoke-board"}, token))
            assert not reused["created"] and reused["board"]["id"] == board["id"]
            thread = json.loads(request(board["api_url"], {"text": "Release smoke opener"}, token))["thread"]
            reply_path = f'/api/threads/{thread["id"]}/posts'
            request(reply_path, {"text": "Persistence check"}, token)
            try:
                request(reply_path, {"text": "Must be full"}, token)
                raise AssertionError("Full thread accepted a reply")
            except urllib.error.HTTPError as error:
                assert error.code == 409
            request("/admin/onboarding", {"action": "save", "prompt": "Smoke instructions"})
            overview = json.loads(request("/onboarding"))
            assert next(iter(overview)) == "instructions" and overview["instructions"] == "Smoke instructions"
            brief = overview["boards"][0]["latest_threads"][0]
            assert brief["preview"] == "Release smoke opener" and brief["full"]
            assert "posts" not in brief
            stop(process)
            process = None
            # Saved admin account, token/key, settings, and prompt must survive
            # without bootstrap credentials or launch tokens on the next start.
            del env["SLOPCHAN_ADMIN_EMAIL"]
            del env["SLOPCHAN_ADMIN_PASSWORD_FILE"]
            jar.clear()
            if args.http:
                # Verify the CLI flag overrides a false environment setting.
                env["SLOPCHAN_ALLOW_INSECURE_ADMIN"] = "false"
                launch_args.append("-allow-insecure-admin")
            process = start()
            request("/admin")
            request("/admin/login", {"email": "smoke@example.com", "password": password})
            assert json.loads(request(thread["api_url"]))["post_count"] == 2
            assert request("/admin/skill") == skill
            overview = json.loads(request("/onboarding"))
            assert overview["thread_max_post_count"] == 2 and overview["instructions"] == "Smoke instructions"
            assert request("/admin/tokens", {"action": "download", "id": "1"}).decode() == download
            request("/api/threads", {"text": "Free thread after restart"}, token)
            request("/admin/onboarding", {"action": "reset"})
            assert json.loads(request("/onboarding"))["instructions"] == initial["instructions"]
            request("/admin/tokens", {"action": "revoke", "id": "1"})
            try:
                request("/api/threads", {"text": "Revoked token must fail"}, token)
                raise AssertionError("Revoked token accepted")
            except urllib.error.HTTPError as error:
                assert error.code == 401
            request("/admin/logout", {})
            assert b'name="password"' in request("/admin/settings")
        finally:
            if process is not None and process.poll() is None:
                stop(process)
    print(f"Admin {scheme.upper()} smoke test passed")


if __name__ == "__main__":
    main()
