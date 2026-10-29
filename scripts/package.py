#!/usr/bin/env python3
"""Create the official CPA store zip layout and SHA-256 checksum list."""
import argparse
import hashlib
from pathlib import Path
import re
import subprocess
import zipfile

ROOT = Path(__file__).resolve().parents[1]


def go_output(*args):
    return subprocess.check_output(["go", *args], cwd=ROOT, text=True).strip()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--arch", choices=("amd64", "arm64"), required=True)
    parser.add_argument("--version")
    args = parser.parse_args()
    version = re.search(r'const pluginVersion = "([^"]+)"', (ROOT / "config.go").read_text())[1]
    if args.version and args.version.removeprefix("v") != version:
        parser.error("release tag does not match pluginVersion in config.go")
    library = ROOT / "build/linux" / args.arch / "egress-timezone.so"
    if not library.is_file():
        parser.error(f"missing library: {library}; run scripts/build.sh first")
    elf = library.read_bytes()[:20]
    machine = int.from_bytes(elf[18:20], "little")
    if elf[:4] != b"\x7fELF" or machine != {"amd64": 62, "arm64": 183}[args.arch]:
        parser.error("library is not an ELF for the selected architecture")
    dist = ROOT / "dist"
    dist.mkdir(exist_ok=True)
    target = dist / f"egress-timezone_{version}_linux_{args.arch}.zip"
    files = [(library, "egress-timezone.so")]
    for name in ("README.md", "README.en.md", "LICENSE", "THIRD_PARTY_NOTICES.md", "CHANGELOG.md"):
        files.append((ROOT / name, name))
    for document in sorted((ROOT / "docs").glob("*.md")):
        files.append((document, "docs/" + document.name))
    for asset in sorted((ROOT / "assets").glob("*.svg")):
        files.append((asset, "assets/" + asset.name))
    files.append((ROOT / "examples/plugin-config.yaml", "examples/plugin-config.yaml"))
    goroot = Path(go_output("env", "GOROOT"))
    files.append((goroot / "LICENSE", "licenses/Go-LICENSE"))
    files.append((goroot / "lib/time/README", "licenses/Go-time-README"))
    for module, label in (("golang.org/x/net", "x-net"), ("gopkg.in/yaml.v3", "yaml-v3")):
        module_dir = Path(go_output("list", "-m", "-f", "{{.Dir}}", module))
        for name in ("LICENSE", "NOTICE", "PATENTS"):
            if (module_dir / name).is_file():
                files.append((module_dir / name, "licenses/" + label + "-" + name))
    with zipfile.ZipFile(target, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
        for path, arcname in files:
            item = zipfile.ZipInfo(arcname, date_time=(2026, 1, 1, 0, 0, 0))
            item.compress_type = zipfile.ZIP_DEFLATED
            item.external_attr = (0o100755 if arcname.endswith(".so") else 0o100644) << 16
            archive.writestr(item, path.read_bytes())
    entries = []
    for artifact in sorted(dist.glob(f"egress-timezone_{version}_linux_*.zip")):
        entries.append(hashlib.sha256(artifact.read_bytes()).hexdigest() + "  " + artifact.name)
    (dist / "checksums.txt").write_text("\n".join(entries) + "\n")
    print(target)


if __name__ == "__main__":
    main()
