#!/usr/bin/env python3
"""Validate release/snapshot archives, then exercise the native installed binary."""
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import stat
import subprocess
import sys
import tarfile
import tempfile
import zipfile


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def verify_executable(content, system, architecture, revision, release=False):
    # Read build metadata without executing binaries for another platform.
    with tempfile.TemporaryDirectory(prefix="skuggsja-buildinfo-") as temporary:
        executable = Path(temporary) / ("skuggsja.exe" if system == "windows" else "skuggsja")
        executable.write_bytes(content)
        result = subprocess.run(["go", "version", "-m", "-json", str(executable)],
                                capture_output=True, text=True, check=True, timeout=30)
    info = json.loads(result.stdout)
    settings = {entry["Key"]: entry["Value"] for entry in info.get("Settings", [])}
    require(info.get("Path") == "github.com/0merUfuk/skuggsja/cmd/skuggsja" and
            info.get("Main", {}).get("Path") == "github.com/0merUfuk/skuggsja",
            "archive executable has the wrong Go module or command path")
    for key, value in (("GOOS", system), ("GOARCH", architecture),
                       ("CGO_ENABLED", "0"), ("-trimpath", "true"),
                       ("vcs.revision", revision)):
        require(settings.get(key) == value, f"archive executable {key} differs from expected {value}")
    if release:
        require(settings.get("vcs.modified") == "false", "release executable was built from a modified checkout")


def main():
    require(len(sys.argv) in (2, 3), "usage: verify-packages.py DIST [RELEASE_TAG]")
    directory = Path(sys.argv[1]).resolve()
    repository = Path(__file__).resolve().parent.parent
    revision = subprocess.run(["git", "rev-parse", "HEAD"], cwd=repository,
                              capture_output=True, text=True, check=True, timeout=30).stdout.strip()
    metadata = directory / "metadata.json"
    if metadata.exists():
        version = json.loads(metadata.read_text())["version"]
    else:
        # Published releases do not carry metadata.json. A stable tag supplies
        # the version for the documented post-publication verification; a
        # snapshot build must still provide its own generated metadata.
        require(len(sys.argv) == 3, "metadata.json is required without a release tag")
        version = sys.argv[2][1:]
    if len(sys.argv) == 3:
        require(re.fullmatch(r"v(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)", sys.argv[2]), "release tag must be stable vMAJOR.MINOR.PATCH")
        require(version == sys.argv[2][1:], "archive version differs from release tag")
    require(re.fullmatch(r"[0-9A-Za-z.+-]+", version), "unexpected package version")
    checksums = {}
    for line in (directory / "checksums.txt").read_text().splitlines():
        parts = line.split()
        require(len(parts) == 2 and re.fullmatch(r"[a-f0-9]{64}", parts[0]), "invalid checksum line")
        name = parts[1].lstrip("*")
        require(Path(name).name == name and name not in checksums, "duplicate or unsafe checksum filename")
        checksums[name] = parts[0]
    expected = {f"skuggsja_{version}_{system}_{arch}.{'zip' if system == 'windows' else 'tar.gz'}": (system, arch)
                for system in ("darwin", "linux", "windows") for arch in ("amd64", "arm64")}
    require(set(checksums) == set(expected), "checksum file must cover exactly all six release archives")
    native_os = {"Darwin": "darwin", "Linux": "linux", "Windows": "windows"}.get(platform.system())
    native_arch = {"arm64": "arm64", "aarch64": "arm64", "x86_64": "amd64", "amd64": "amd64"}.get(platform.machine().lower())
    native = None
    for name in sorted(expected):
        archive = directory / name
        system, architecture = expected[name]
        require(hashlib.sha256(archive.read_bytes()).hexdigest() == checksums[name], f"checksum mismatch: {name}")
        executable = "skuggsja.exe" if name.endswith(".zip") else "skuggsja"
        if name.endswith(".zip"):
            with zipfile.ZipFile(archive) as handle:
                members = handle.infolist()
                require(len(members) == 3 and all(not entry.is_dir() and
                        stat.S_IFMT(entry.external_attr >> 16) == stat.S_IFREG for entry in members),
                        "unexpected ZIP members or non-regular file types")
                files = {entry.filename: handle.read(entry) for entry in members}
        else:
            with tarfile.open(archive, "r:gz") as handle:
                members = handle.getmembers()
                require(len(members) == 3 and all(entry.isfile() for entry in members), "unexpected tar members or links")
                require(all(entry.mode & 0o111 for entry in members if entry.name == executable), "archive executable permission missing")
                files = {entry.name: handle.extractfile(entry).read() for entry in members}
        require(set(files) == {executable, "README.md", "LICENSE"}, "unexpected archive contents")
        for document in ("README.md", "LICENSE"):
            require(files[document] == (repository / document).read_bytes(), f"stale packaged {document}")
        for asset in ("web/index.html", "web/app.js", "web/styles.css"):
            require((repository / asset).read_bytes() in files[executable], f"stale or missing embedded {asset}")
        verify_executable(files[executable], system, architecture, revision, release=len(sys.argv) == 3)
        if system == native_os and architecture == native_arch:
            native = (executable, files[executable])
    require(native is not None, "no archive for the verification host")
    with tempfile.TemporaryDirectory(prefix="skuggsja-package-") as temporary:
        binary = Path(temporary) / native[0]
        binary.write_bytes(native[1])
        binary.chmod(0o700)
        subprocess.run(["node", str(repository / "scripts/verify-install.cjs"), str(binary), version], check=True)
    print("PASS: 6/6 archive checksums, regular-file contents, embedded assets and Go target/provenance metadata; native installed-package smoke")


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, OSError, ValueError, subprocess.CalledProcessError, subprocess.TimeoutExpired) as error:
        print(f"package verification failed: {error}", file=sys.stderr)
        sys.exit(1)
