#!/usr/bin/env python3
"""Exercise a built executable, including paths with spaces and durable storage."""
import base64
import json
import os
from pathlib import Path
import secrets
import signal
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

binary = str(Path(sys.argv[1]).resolve())
subprocess.run([binary, "version"], check=True)
# Password environment values must never become visible flag-help defaults.
help_password = secrets.token_hex(32)
help_env = dict(os.environ, SLOPCHAN_ADMIN_PASSWORD=help_password)
help_result = subprocess.run([binary, "serve", "-help"], env=help_env,
                             stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                             text=True, check=True)
assert help_password not in help_result.stdout, "Password exposed in flag help"
with tempfile.TemporaryDirectory(prefix="slopchan smoke ") as tmp:
    root = Path(tmp)
    token = secrets.token_hex(32)
    (root / "tokens").write_text(token)
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    address = f"http://127.0.0.1:{port}"
    process = None

    def request(path, data=None, auth=False):
        headers = {"Content-Type": "application/json"}
        if auth:
            headers["Authorization"] = "Bearer " + token
        req = urllib.request.Request(address + path, data=json.dumps(data).encode() if data else None, headers=headers)
        with urllib.request.urlopen(req, timeout=5) as response:
            return json.load(response)

    def start():
        env = {k: v for k, v in os.environ.items() if not k.startswith("SLOPCHAN_")}
        options = {"creationflags": subprocess.CREATE_NEW_PROCESS_GROUP} if os.name == "nt" else {}
        p = subprocess.Popen([binary, "serve", "-data", str(root / "board data"), "-token-file", str(root / "tokens"), "-listen", f"127.0.0.1:{port}"], env=env, **options)
        for _ in range(100):
            if p.poll() is not None:
                raise RuntimeError(f"Server exited: {p.returncode}")
            try:
                request("/api/threads")
                return p
            except (urllib.error.URLError, TimeoutError):
                time.sleep(0.1)
        p.kill()
        p.wait()
        raise RuntimeError("Server did not become ready")

    def stop(p):
        if os.name == "nt":
            # Windows runners may not have a console for CTRL_BREAK delivery.
            p.terminate()
        else:
            p.send_signal(signal.SIGTERM)
        try:
            code = p.wait(timeout=40)
        except subprocess.TimeoutExpired:
            p.kill()
            p.wait()
            raise
        if os.name != "nt":
            assert code == 0, code

    try:
        process = start()
        try:
            request("/api/threads", {"text": "unauthorized"})
            raise AssertionError("Unauthenticated write succeeded")
        except urllib.error.HTTPError as error:
            assert error.code == 401, error
        # Upload an actual image to catch platform-specific filesystem failures.
        boundary = "slopchan-smoke"
        gif = base64.b64decode("R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==")
        body = (f'--{boundary}\r\nContent-Disposition: form-data; name="text"\r\n\r\nportable persistence\r\n'
                f'--{boundary}\r\nContent-Disposition: form-data; name="image"; filename="test.gif"\r\nContent-Type: image/gif\r\n\r\n').encode() + gif + f'\r\n--{boundary}--\r\n'.encode()
        req = urllib.request.Request(address + "/api/threads", data=body, headers={"Authorization": "Bearer " + token, "Content-Type": "multipart/form-data; boundary=" + boundary})
        with urllib.request.urlopen(req) as response:
            post = json.load(response)["post"]
        stop(process)
        process = None
        process = start()
        assert request(f'/api/posts/{post["id"]}')["post"]["text"] == "portable persistence"
        assert len(request("/api/search?q=persistence")["posts"]) == 1
        with urllib.request.urlopen(address + post["image"]["url"]) as response:
            assert response.read() == gif
        stop(process)
        process = None
        subprocess.run([binary, "remove", "-data", str(root / "board data"), str(post["id"])], check=True)
    finally:
        if process is not None and process.poll() is None:
            stop(process)
print("Native smoke test passed")
