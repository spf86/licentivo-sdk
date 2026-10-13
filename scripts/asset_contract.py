"""Shared allowlist and checksum checks for assembled SDK releases."""
import hashlib
import json
import pathlib
import re

LANGUAGES = ("go", "java", "c", "cpp", "csharp", "python", "javascript", "rust", "ruby")


def required_names(version):
    return {f"licentivo-{language}-{version}.zip" for language in LANGUAGES} | {
        f"licentivo_sdk-{version}-py3-none-any.whl", f"licentivo-sdk-{version}.tgz",
        f"licentivo-sdk-{version}.jar", f"Licentivo.SDK.{version}.nupkg",
        f"licentivo-{version}.crate", f"licentivo-sdk-{version}.gem", "install.ps1", "install.sh",
        f"licentivo-native-{version}-linux-x64.zip", f"licentivo-native-{version}-windows-x64-mingw.zip",
        f"licentivo-sdk-{version}-x86_64-linux.gem", f"licentivo-sdk-{version}-x64-mingw-ucrt.gem",
    }


def read_assets(directory, version, complete=False):
    """Verify before copying; unlisted files and conflicting checksums fail closed."""
    if not re.fullmatch(r"\d+\.\d+\.\d+", version):
        raise ValueError("Invalid release version")
    directory = pathlib.Path(directory)
    for name in ("manifest.json", "SHA256SUMS"):
        if (directory / name).is_symlink():
            raise ValueError("Release metadata cannot be a symlink")
    raw = (directory / "manifest.json").read_bytes()
    manifest = json.loads(raw)
    if manifest.get("version") != version or manifest.get("repository") != "spf86/licentivo-sdk":
        raise ValueError("Artifact manifest version or repository mismatch")
    allowed = required_names(version) | {f"licentivo-native-{version}-windows-x64-msvc.zip"}
    records = {}
    for item in manifest["artifacts"]:
        name = item["name"]
        if name not in allowed or name in records:
            raise ValueError(f"Unexpected or duplicate release asset: {name}")
        file = directory / name
        size, digest = item["bytes"], item["sha256"]
        if type(size) is not int or size < 0 or not re.fullmatch(r"[a-f0-9]{64}", digest):
            raise ValueError(f"Invalid asset record: {name}")
        if file.is_symlink() or not file.is_file() or file.stat().st_size != size or hashlib.sha256(file.read_bytes()).hexdigest() != digest:
            raise ValueError(f"Release asset checksum mismatch: {name}")
        records[name] = item
    expected = {name: item["sha256"] for name, item in records.items()}
    expected["manifest.json"] = hashlib.sha256(raw).hexdigest()
    checksums = {}
    for line in (directory / "SHA256SUMS").read_text(encoding="utf-8").splitlines():
        match = re.fullmatch(r"([a-f0-9]{64})  ([A-Za-z0-9_.-]+)", line)
        if not match or match[2] in checksums:
            raise ValueError("Invalid or duplicate SHA256SUMS entry")
        checksums[match[2]] = match[1]
    if checksums != expected:
        raise ValueError("Manifest and SHA256SUMS disagree")
    if {file.name for file in directory.iterdir()} != set(expected) | {"SHA256SUMS"}:
        raise ValueError("Release directory contains unlisted files")
    if complete and (missing := required_names(version) - records.keys()):
        raise ValueError(f"Missing client packages: {sorted(missing)}")
    return manifest
