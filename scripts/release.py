#!/usr/bin/env python3
"""Build portable, CGO-free release archives using only Go and Python 3."""
import argparse
import hashlib
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parent.parent
TARGETS = {
    "linux_amd64": ("linux", "amd64", ""),
    "linux_arm64": ("linux", "arm64", ""),
    "linux_armv6": ("linux", "arm", "6"),
    "linux_armv7": ("linux", "arm", "7"),
    "linux_386": ("linux", "386", ""),
    "linux_riscv64": ("linux", "riscv64", ""),
    "darwin_amd64": ("darwin", "amd64", ""),
    "darwin_arm64": ("darwin", "arm64", ""),
    "windows_amd64": ("windows", "amd64", ""),
    "windows_arm64": ("windows", "arm64", ""),
    "freebsd_amd64": ("freebsd", "amd64", ""),
    "freebsd_arm64": ("freebsd", "arm64", ""),
}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("version", help="stable tag, e.g. v0.2.1, or dev")
    parser.add_argument("--target", action="append", choices=TARGETS)
    parser.add_argument("--output", type=Path, default=ROOT / "dist")
    args = parser.parse_args()
    if not re.fullmatch(r"v\d+\.\d+\.\d+|dev", args.version):
        parser.error("version must be dev or a stable vMAJOR.MINOR.PATCH tag")
    version = args.version.removeprefix("v")
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    archives = []
    for target in args.target or TARGETS:
        goos, goarch, goarm = TARGETS[target]
        name = f"slopchan_{version}_{target}"
        with tempfile.TemporaryDirectory(prefix="slopchan-build-") as tmp:
            stage = Path(tmp)
            binary = stage / ("slopchan.exe" if goos == "windows" else "slopchan")
            print(f"Building {target}", flush=True)
            subprocess.run(
                ["go", "build", "-trimpath", "-ldflags", f"-s -w -X main.version={version}",
                 "-o", str(binary), "."], cwd=ROOT, check=True,
                env={**os.environ, "CGO_ENABLED": "0", "GOOS": goos,
                     "GOARCH": goarch, "GOARM": goarm, "GOAMD64": "v1",
                     "GOARM64": "v8.0", "GO386": "sse2"},
            )
            for filename in ("README.md", "DESIGN.md", "compose.yaml", "compose.lan.yaml", ".env.example"):
                shutil.copy2(ROOT / filename, stage)
            shutil.copytree(ROOT / "docs", stage / "docs")
            shutil.copytree(ROOT / "deploy", stage / "deploy")
            shutil.copytree(ROOT / "skills", stage / "skills")
            # Include a project license automatically once the owner chooses one.
            for license_file in ROOT.glob("LICENSE*"):
                if license_file.is_file():
                    shutil.copy2(license_file, stage)
            # Preserve the licenses and notices of every bundled Go module.
            modules = subprocess.check_output(
                ["go", "list", "-deps", "-f", "{{with .Module}}{{.Path}} {{.Dir}}{{end}}", "."],
                cwd=ROOT, text=True,
                env={**os.environ, "CGO_ENABLED": "0", "GOOS": goos, "GOARCH": goarch, "GOARM": goarm},
            )
            for module in sorted(set(modules.splitlines())):
                if not module or module.startswith("slopchan "):
                    continue
                module_name, module_dir = module.split(" ", 1)
                if not module_dir:
                    raise RuntimeError(f"Missing source directory for bundled module {module_name}")
                for notice in Path(module_dir).iterdir():
                    if notice.is_file() and notice.name.upper().startswith(("LICENSE", "COPYING", "NOTICE", "AUTHORS")):
                        dest = stage / "licenses" / module_name / notice.name
                        dest.parent.mkdir(parents=True, exist_ok=True)
                        shutil.copy2(notice, dest)
            if goos == "windows":
                archive = output / (name + ".zip")
                with zipfile.ZipFile(archive, "w", zipfile.ZIP_DEFLATED) as bundle:
                    for path in sorted(stage.rglob("*")):
                        if path.is_file():
                            bundle.write(path, path.relative_to(stage))
            else:
                archive = output / (name + ".tar.gz")
                with tarfile.open(archive, "w:gz") as bundle:
                    for path in sorted(stage.iterdir()):
                        bundle.add(path, arcname=path.name)
            archives.append(archive)
    # Do not include stale artifacts from previous invocations in this manifest.
    checksums = ""
    for path in archives:
        with path.open("rb") as stream:
            checksums += f"{hashlib.file_digest(stream, 'sha256').hexdigest()}  {path.name}\n"
    (output / "checksums.txt").write_text(checksums)
    print(f"Wrote {len(archives)} archives and checksums.txt to {output}")


if __name__ == "__main__":
    main()
