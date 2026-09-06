#!/usr/bin/env python3
"""Run only in a disposable container/user account; installs into that user's home.

docker run --rm -v "$PWD:/src:ro" python:3.13-alpine sh -c \
  'apk add --no-cache openssl && adduser -D installer && su installer -c "python /src/scripts/test-installer.py"'
"""
import hashlib
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile

# Avoid changing a real user's installation if invoked outside the documented test.
if not Path("/.dockerenv").exists() or Path.home() != Path("/home/installer"):
    raise SystemExit("Run inside the disposable container described in this script's docstring.")

script = Path(__file__).resolve().parent.parent / "deploy/install.sh"
with tempfile.TemporaryDirectory() as tmp:
    root = Path(tmp)
    stage = root / "stage"
    stage.mkdir()
    (stage / "slopchan").write_text("#!/bin/sh\necho fixture\n")
    for filename in ("LICENSE", "README.md", "DESIGN.md", "compose.yaml", "compose.lan.yaml", ".env.example"):
        (stage / filename).write_text("fixture")
    for directory in ("deploy", "docs", "licenses", "skills"):
        (stage / directory).mkdir()
        (stage / directory / "fixture").write_text("fixture")
    archive = root / "archive.tar.gz"
    with tarfile.open(archive, "w:gz") as bundle:
        for path in stage.iterdir():
            bundle.add(path, arcname=path.name)
    digest = hashlib.sha256(archive.read_bytes()).hexdigest()
    stubs = root / "stubs"
    stubs.mkdir()
    # Stub the release transport and host CPU, keeping checksum/extract/install real.
    (stubs / "curl").write_text('''#!/usr/bin/env python3
import os, pathlib, shutil, sys
args = sys.argv[1:]
url = next(a for a in args if a.startswith("https://"))
out = args[args.index("-o") + 1]
if url.endswith("/latest"):
    print("https://github.com/rengwu/slopchan/releases/tag/v0.2.0", end="")
elif url.endswith("checksums.txt"):
    digest = "0" * 64 if os.environ.get("CORRUPT") else os.environ["DIGEST"]
    pathlib.Path(out).write_text(digest + "  " + os.environ["ASSET"] + "\\n")
else:
    assert url.endswith("/" + os.environ["ASSET"]), url
    shutil.copyfile(os.environ["ARCHIVE"], out)
''')
    (stubs / "uname").write_text('#!/bin/sh\ncase "$1" in -s) echo Linux ;; -m) echo "$CPU" ;; esac\n')
    (stubs / "getconf").write_text('#!/bin/sh\necho "$BITS"\n')
    for path in stubs.iterdir():
        path.chmod(0o755)
    env = {**os.environ, "PATH": str(stubs) + ":" + os.environ["PATH"], "ARCHIVE": str(archive), "DIGEST": digest}
    binary = Path.home() / ".local/bin/slopchan"
    tokens = Path.home() / ".config/slopchan/tokens"
    data = Path.home() / ".local/share/slopchan/data"
    old_token = None
    for cpu, bits, arch in [("x86_64", "64", "amd64"), ("aarch64", "64", "arm64"),
                            ("aarch64", "32", "armv7"), ("armv7l", "32", "armv7"),
                            ("armv6l", "32", "armv6"), ("i686", "32", "386")]:
        current = {**env, "CPU": cpu, "BITS": bits, "ASSET": f"slopchan_0.2.0_linux_{arch}.tar.gz"}
        subprocess.run(["sh", str(script)], env=current, check=True, stdout=subprocess.DEVNULL)
        assert binary.stat().st_mode & 0o111
        assert tokens.stat().st_mode & 0o777 == 0o600
        assert len(tokens.read_text().strip()) == 64
        if old_token:
            assert tokens.read_bytes() == old_token, "Upgrade rotated the token"
            assert (data / "sentinel").read_text() == "keep", "Upgrade changed data"
        old_token = tokens.read_bytes()
        data.mkdir(exist_ok=True)
        (data / "sentinel").write_text("keep")
    binary.write_text("existing installation")
    result = subprocess.run(["sh", str(script), "v0.2.0"], env={**current, "CORRUPT": "1"}, capture_output=True, text=True)
    assert result.returncode != 0 and "SHA-256" in result.stderr, result
    assert binary.read_text() == "existing installation", "Bad download replaced executable"
    result = subprocess.run(["sh", str(script), "v0.2.0"], env={**current, "CPU": "mips"}, capture_output=True, text=True)
    assert result.returncode != 0 and "Unsupported CPU" in result.stderr, result
print("Unix installer passed: architecture selection, checksums, private token, upgrade preservation, bad-download rejection")
