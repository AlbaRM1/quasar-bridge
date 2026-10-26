#!/usr/bin/env python3
"""Verify package contract and persistent launcher state without speaker hardware."""
import importlib.util
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import tomllib
import unittest
import zipfile

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("builder", ROOT / "scripts/build-yap.py")
builder = importlib.util.module_from_spec(spec)
spec.loader.exec_module(builder)

class PackageTest(unittest.TestCase):
    def test_release_tag_must_match_manifest_before_building(self):
        version = tomllib.loads((ROOT / "packaging/yam/plugin.toml").read_text())["version"]
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp) / "release.yap"
            missing_binary = Path(tmp) / "missing"
            for tag in ("v999.0.0", version, "v01.2.3", "v1.2", "v1.2.3-rc.1", "v1.2.3\n"):
                with self.subTest(tag=tag), self.assertRaisesRegex(ValueError, "release tag"):
                    builder.build(output, missing_binary, tag=tag)
                self.assertFalse(output.exists())

    def test_matching_tag_produces_named_package_and_checksum(self):
        version = tomllib.loads((ROOT / "packaging/yam/plugin.toml").read_text())["version"]
        with tempfile.TemporaryDirectory() as tmp:
            binary = Path(tmp) / "arm64"
            binary.write_bytes(b"\x7fELF\x02" + bytes(13) + b"\xb7\x00" + bytes(44))
            output = Path(tmp) / f"quasar-bridge-{version}-linux-arm64.yap"
            package = builder.build(output, binary, tag="v" + version)
            checksum = package.with_suffix(".yap.sha256").read_text().split()
            self.assertEqual(checksum[1], output.name)
            self.assertEqual(checksum[0], hashlib.sha256(package.read_bytes()).hexdigest())

    def test_payload_and_persistent_state(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            binary = root / "arm64"
            binary.write_bytes(b"\x7fELF\x02" + bytes(13) + b"\xb7\x00" + bytes(44))
            package = builder.build(root / "package.yap", binary)
            with zipfile.ZipFile(package) as z:
                self.assertEqual(set(z.namelist()), set(builder.PAYLOAD) | {"bin/quasar-bridge"})
                manifest = tomllib.loads(z.read("plugin.toml").decode())
                self.assertEqual(manifest["launch"]["mode"], "service")
                self.assertEqual(manifest["launch"]["kind"], "binary")
                self.assertEqual(manifest["permissions"], ["exec"])
                self.assertFalse(json.loads(z.read("config.device.example.json"))["conversation_log"]["enabled"])
                self.assertTrue(z.getinfo("bin/quasar-bridge").external_attr >> 16 & 0o111)
                z.extractall(root / "plugin")
            # A fake executable isolates shell behavior: config preservation, env and PID via exec.
            executable = root / "plugin/bin/quasar-bridge"
            executable.write_text('#!/bin/sh\nprintf "%s\\n" "$GOMAXPROCS|$GOMEMLIMIT|$PRIVATE_MARKER|$*"\n')
            executable.chmod(0o755)
            state = root / "persistent"
            env = dict(os.environ, QUASAR_DATA_DIR=str(state))
            launcher = ["sh", str(root / "plugin/start.sh")]
            subprocess.run(launcher + ["-check-config"], env=env, check=True, capture_output=True)
            self.assertEqual((state / "config.json").stat().st_mode & 0o777, 0o600)
            (state / "config.json").write_text('{"mode":"observe","user":"preserved"}')
            (state / "runtime.env").write_text("PRIVATE_MARKER='fixture'\nGOMAXPROCS=2\n")
            result = subprocess.run(launcher + ["-check-config"], env=env, check=True, capture_output=True, text=True)
            self.assertIn("2|64MiB|fixture|", result.stdout)
            self.assertEqual(json.loads((state / "config.json").read_text())["user"], "preserved")
            self.assertTrue(package.with_suffix('.yap.sha256').is_file())

    def test_wrong_architecture_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            binary = Path(tmp) / "amd64"
            binary.write_bytes(b"\x7fELF\x02" + bytes(13) + b"\x3e\x00")
            with self.assertRaises(ValueError):
                builder.build(Path(tmp) / "bad.yap", binary)

if __name__ == "__main__":
    unittest.main()
