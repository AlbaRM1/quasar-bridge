#!/usr/bin/env python3
"""Build a YAM .yap from an explicit, public payload allowlist."""
import argparse
import hashlib
import os
from pathlib import Path
import re
import subprocess
import tempfile
import tomllib
import zipfile

ROOT = Path(__file__).resolve().parents[1]
PAYLOAD = ("plugin.toml", "start.sh", "control.sh", "S98quasar-yam", "config.device.example.json")

def build(output, binary=None, tag=None):
    manifest = tomllib.loads((ROOT / "packaging/yam/plugin.toml").read_text())
    if tag is not None:
        if not re.fullmatch(r"v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)", tag):
            raise ValueError("release tag must be vMAJOR.MINOR.PATCH (stable versions only)")
        if tag[1:] != manifest["version"]:
            raise ValueError("release tag does not match packaging/yam/plugin.toml version")
    output = Path(output or ROOT / f'dist/quasar-bridge-{manifest["version"]}-linux-arm64.yap').resolve()
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="quasar-yap-") as tmp:
        executable = Path(binary).resolve() if binary else Path(tmp) / "quasar-bridge"
        if not binary:
            env = dict(os.environ, CGO_ENABLED="0", GOOS="linux", GOARCH="arm64")
            subprocess.run(["go", "build", "-trimpath", "-ldflags=-s -w", "-o", str(executable), "./cmd/quasar-bridge"], cwd=ROOT, env=env, check=True)
        header = executable.read_bytes()[:20]
        if header[:5] != b"\x7fELF\x02" or header[18:20] != b"\xb7\x00":
            raise ValueError("package requires a Linux ARM64 ELF binary")
        files = [(name, ROOT / "packaging/yam" / name) for name in PAYLOAD]
        files.append(("bin/quasar-bridge", executable))
        if sum(path.stat().st_size for _, path in files) > 32 * 1024 * 1024:
            raise ValueError("YAM unpacked size exceeds 32 MiB")
        with zipfile.ZipFile(output, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
            for name, path in files:
                info = zipfile.ZipInfo(name, (2026, 1, 1, 0, 0, 0))
                info.create_system = 3
                info.external_attr = (0o100755 if name.endswith('.sh') or name.startswith(('bin/', 'S98')) else 0o100644) << 16
                archive.writestr(info, path.read_bytes(), compress_type=zipfile.ZIP_DEFLATED, compresslevel=9)
    digest = hashlib.sha256(output.read_bytes()).hexdigest()
    output.with_suffix(output.suffix + ".sha256").write_text(f"{digest}  {output.name}\n")
    print(output)
    return output

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output")
    parser.add_argument("--binary", help="prebuilt static Linux ARM64 executable")
    parser.add_argument("--tag", help="require a stable vMAJOR.MINOR.PATCH tag matching plugin.toml")
    args = parser.parse_args()
    build(args.output, args.binary, args.tag)
