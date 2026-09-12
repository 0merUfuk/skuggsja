#!/usr/bin/env python3
"""Archive regressions with real cross-built Go payloads and recomputed hashes.

Only the final installed-CLI subprocess is replaced: this suite isolates archive
validation and never executes a foreign binary or reads real harness histories.
"""
import contextlib
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import stat
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest import mock
import zipfile

# Keep the source checkout free of bytecode artifacts during release checks.
sys.dont_write_bytecode = True

SPEC = importlib.util.spec_from_file_location("package_verifier", Path(__file__).with_name("verify-packages.py"))
VERIFIER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(VERIFIER)
RUN = subprocess.run


class PackageTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temporary = tempfile.TemporaryDirectory(prefix="skuggsja-package-tests-")
        cls.addClassCleanup(cls.temporary.cleanup)
        cls.base = Path(cls.temporary.name)
        cls.repo = cls.base / "synthetic-repository"
        (cls.repo / "cmd/skuggsja").mkdir(parents=True)
        cls.assets = {
            "web/index.html": "<html>synthetic package verification fixture</html>",
            "web/app.js": "window.syntheticPackageFixture = true;",
            "web/styles.css": ".synthetic-package-fixture { color: black; }",
            "web/fonts/fixture-latin-var.woff2": "synthetic webfont fixture bytes\n",
            "web/fonts/OFL.txt": "Synthetic fixture licence placeholder, not the SIL Open Font License.\n",
        }
        for name, content in cls.assets.items():
            member = cls.repo / name
            member.parent.mkdir(parents=True, exist_ok=True)
            member.write_text(content)
        for name in ["README.md", "LICENSE"]:
            (cls.repo / name).write_text("Synthetic " + name + " fixture.\n")
        (cls.repo / "go.mod").write_text("module github.com/0merUfuk/skuggsja\n\ngo 1.27.1\n")
        source = "package main\nvar assets = []string{" + ",".join(json.dumps(value) for value in cls.assets.values()) + "}\nfunc main() { for _, value := range assets { println(value) } }\n"
        (cls.repo / "cmd/skuggsja/main.go").write_text(source)
        cls.env = {**os.environ, "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.devnull,
                   "GOWORK": "off", "GOTOOLCHAIN": "local", "GOPROXY": "off", "GOFLAGS": ""}
        for command in (["git", "init", "--quiet", "--template="], ["git", "add", "."],
                        ["git", "-c", "user.name=Synthetic Fixture", "-c", "user.email=fixture@example.invalid",
                         "-c", "commit.gpgsign=false", "commit", "--quiet", "-m", "test: synthetic package fixture"]):
            RUN(command, cwd=cls.repo, env=cls.env, check=True, capture_output=True, text=True)
        cls.revision = RUN(["git", "rev-parse", "HEAD"], cwd=cls.repo, env=cls.env,
                           check=True, capture_output=True, text=True).stdout.strip()
        # Use the toolchain selected by the real source checkout, then keep
        # synthetic fixture builds offline even when a local go shim is older.
        goroot = RUN(["go", "env", "GOROOT"], cwd=Path(__file__).resolve().parent.parent,
                     check=True, capture_output=True, text=True).stdout.strip()
        go = str(Path(goroot) / "bin" / ("go.exe" if os.name == "nt" else "go"))
        cls.binaries = {}
        for system in ["darwin", "linux", "windows"]:
            for architecture in ["amd64", "arm64"]:
                output = cls.base / (system + "-" + architecture)
                env = {**cls.env, "GOOS": system, "GOARCH": architecture, "CGO_ENABLED": "0"}
                RUN([go, "build", "-trimpath", "-ldflags=-s -w", "-o", str(output), "./cmd/skuggsja"],
                    cwd=cls.repo, env=env, check=True, capture_output=True, text=True, timeout=120)
                cls.binaries[(system, architecture)] = output.read_bytes()
        # A real checkout with the same shape but different webfont bytes proves
        # the embedded-asset comparison is not vacuous. No binary is rebuilt: the
        # archives keep the original payload and only the checkout diverges.
        cls.divergent = cls.base / "divergent-repository"
        for name, content in cls.assets.items():
            member = cls.divergent / name
            member.parent.mkdir(parents=True, exist_ok=True)
            member.write_text("divergent " + content if name.endswith(".woff2") else content)
        for name in ["README.md", "LICENSE"]:
            (cls.divergent / name).write_text("Synthetic " + name + " fixture.\n")
        (cls.divergent / "go.mod").write_text("module github.com/0merUfuk/skuggsja\n\ngo 1.27.1\n")
        for command in (["git", "init", "--quiet", "--template="], ["git", "add", "."],
                        ["git", "-c", "user.name=Synthetic Fixture", "-c", "user.email=fixture@example.invalid",
                         "-c", "commit.gpgsign=false", "commit", "--quiet", "-m", "test: divergent package fixture"]):
            RUN(command, cwd=cls.divergent, env=cls.env, check=True, capture_output=True, text=True)

    def setUp(self):
        self.directory = Path(tempfile.mkdtemp(prefix="archives-", dir=self.base))
        self.native_calls = []
        self.native_bytes = []

    def archives(self, replacement=None, zip_type=stat.S_IFREG, zip_member="skuggsja.exe"):
        sums = []
        (self.directory / "metadata.json").write_text(json.dumps({"version": "0.1.0"}))
        for target, original in self.binaries.items():
            system, architecture = target
            content = replacement.get(target, original) if replacement else original
            executable = "skuggsja.exe" if system == "windows" else "skuggsja"
            name = f"skuggsja_0.1.0_{system}_{architecture}." + ("zip" if system == "windows" else "tar.gz")
            archive = self.directory / name
            files = {executable: content, **{name: (self.repo / name).read_bytes() for name in ["README.md", "LICENSE"]}}
            if system == "windows":
                with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_DEFLATED) as handle:
                    for filename, payload in files.items():
                        info = zipfile.ZipInfo(filename)
                        info.create_system = 3
                        kind = zip_type if filename == zip_member else stat.S_IFREG
                        info.external_attr = (kind | (0o755 if filename == executable else 0o644)) << 16
                        handle.writestr(info, payload)
            else:
                with tarfile.open(archive, "w:gz") as handle:
                    for filename, payload in files.items():
                        info = tarfile.TarInfo(filename)
                        info.size = len(payload)
                        info.mode = 0o755 if filename == executable else 0o644
                        handle.addfile(info, io.BytesIO(payload))
            sums.append(hashlib.sha256(archive.read_bytes()).hexdigest() + "  " + name)
        # Tampered archives deliberately have valid checksums: metadata/type
        # validation must catch defects that checksum comparison cannot detect.
        (self.directory / "checksums.txt").write_text("\n".join(sums) + "\n")

    def verify(self, *, release=False, repository=None):
        def run(command, *args, **kwargs):
            if command[0] == "node":
                self.native_calls.append(command)
                self.native_bytes.append(Path(command[2]).read_bytes())
                return subprocess.CompletedProcess(command, 0)
            return RUN(command, *args, **kwargs)
        argv = ["verify-packages.py", str(self.directory)] + (["v0.1.0"] if release else [])
        with mock.patch.object(VERIFIER, "__file__", str((repository or self.repo) / "scripts/verify-packages.py")), \
             mock.patch.object(VERIFIER.sys, "argv", argv), \
             mock.patch.object(VERIFIER.subprocess, "run", side_effect=run), \
             contextlib.redirect_stdout(io.StringIO()):
            VERIFIER.main()

    def test_all_six_real_targets_pass_before_native_smoke(self):
        self.archives()
        self.verify(release=True)
        self.assertEqual(len(self.native_calls), 1)

    def test_wrong_operating_system_fails_despite_valid_checksums(self):
        self.archives({("linux", "amd64"): self.binaries[("darwin", "amd64")]})
        with self.assertRaisesRegex(RuntimeError, "GOOS differs"):
            self.verify()
        self.assertEqual(self.native_calls, [])

    def test_wrong_architecture_fails_despite_valid_checksums(self):
        self.archives({("linux", "arm64"): self.binaries[("linux", "amd64")]})
        with self.assertRaisesRegex(RuntimeError, "GOARCH differs"):
            self.verify()
        self.assertEqual(self.native_calls, [])

    def test_zip_symlink_payload_fails_despite_valid_checksums(self):
        self.archives(zip_type=stat.S_IFLNK)
        with self.assertRaisesRegex(RuntimeError, "non-regular file types"):
            self.verify()
        self.assertEqual(self.native_calls, [])

    def test_zip_document_special_file_fails_despite_valid_checksums(self):
        self.archives(zip_type=stat.S_IFIFO, zip_member="README.md")
        with self.assertRaisesRegex(RuntimeError, "non-regular file types"):
            self.verify()
        self.assertEqual(self.native_calls, [])

    def test_windows_uppercase_arm64_selects_the_correct_native_archive(self):
        self.archives()
        with mock.patch.object(VERIFIER.platform, "system", return_value="Windows"), \
             mock.patch.object(VERIFIER.platform, "machine", return_value="ARM64"):
            self.verify()
        self.assertEqual(self.native_bytes, [self.binaries[("windows", "arm64")]])
        self.assertTrue(self.native_calls[0][2].endswith("skuggsja.exe"))

    def test_windows_uppercase_amd64_selects_the_correct_native_archive(self):
        self.archives()
        with mock.patch.object(VERIFIER.platform, "system", return_value="Windows"), \
             mock.patch.object(VERIFIER.platform, "machine", return_value="AMD64"):
            self.verify()
        self.assertEqual(self.native_bytes, [self.binaries[("windows", "amd64")]])

    def test_stale_webfont_bytes_fail_despite_valid_checksums(self):
        self.archives()
        with self.assertRaisesRegex(RuntimeError, "stale or missing embedded web/fonts/fixture-latin-var.woff2"):
            self.verify(repository=self.divergent)
        self.assertEqual(self.native_calls, [])

    def test_payload_from_another_checkout_revision_is_rejected(self):
        with self.assertRaisesRegex(RuntimeError, "vcs.revision differs"):
            VERIFIER.verify_executable(self.binaries[("linux", "amd64")], "linux", "amd64", "0" * 40)


if __name__ == "__main__":
    unittest.main(verbosity=2)
