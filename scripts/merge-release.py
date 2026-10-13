#!/usr/bin/env python3
"""Verify CI assets before assembling the single public release."""
import argparse
import importlib.util
import pathlib
import shutil
from asset_contract import missing_required_names, read_assets

ROOT = pathlib.Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("build_release", ROOT / "scripts/build-release.py")
build = importlib.util.module_from_spec(spec)
spec.loader.exec_module(build)


def merge(source, output):
    version = (ROOT / "VERSION").read_text().strip()
    if output.exists() and any(output.iterdir()):
        raise ValueError("Output must be empty; release files cannot be mixed")
    assets = {}
    manifests = list(source.glob("*/manifest.json"))
    if not manifests:
        raise ValueError("No verified build manifests were found")
    for manifest_path in manifests:
        data = read_assets(manifest_path.parent, version)
        for item in data["artifacts"]:
            name = item["name"]
            file = manifest_path.parent / name
            if name in assets and assets[name].read_bytes() != file.read_bytes():
                raise ValueError(f"Conflicting build assets: {name}")
            assets[name] = file
    missing = missing_required_names(version, assets)
    if missing:
        raise ValueError(f"Missing client packages: {sorted(missing)}")
    output.mkdir(parents=True, exist_ok=True)
    for name, source_file in assets.items():
        shutil.copyfile(source_file, output / name)
    build.checksum(output, assets, version)
    read_assets(output, version, complete=True)
    print(f"Verified and assembled {len(assets)} release assets for {version}")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", required=True, type=pathlib.Path)
    parser.add_argument("--out", required=True, type=pathlib.Path)
    args = parser.parse_args()
    merge(args.input.resolve(), args.out.resolve())
