#!/usr/bin/env python3
"""Prepare a Maven Central bundle from a verified SDK release; no upload or server access."""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import xml.etree.ElementTree as ET
import zipfile

ROOT = Path(__file__).resolve().parents[1]


def zip_directory(target, directory):
    with zipfile.ZipFile(target, 'w', compression=zipfile.ZIP_DEFLATED) as archive:
        for source in sorted(directory.rglob('*')):
            if source.is_file():
                if source.is_symlink():
                    raise ValueError('Symlink in public Java source')
                archive.write(source, source.relative_to(directory).as_posix())


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--packages', required=True, type=Path)
    parser.add_argument('--out', required=True, type=Path)
    parser.add_argument('--java-home', required=True, type=Path)
    signer = parser.add_mutually_exclusive_group(required=True)
    signer.add_argument('--gpg')
    signer.add_argument('--pgp-private-key', type=Path)
    parser.add_argument('--gpg-home', type=Path)
    parser.add_argument('--signing-key', required=True)
    parser.add_argument('--passphrase-file', required=True, type=Path)
    args = parser.parse_args()
    if args.gpg and not args.gpg_home:
        parser.error('--gpg requires --gpg-home')
    version = (ROOT / 'VERSION').read_text().strip()
    pom_path = ROOT / 'sdk/java/pom.xml'
    pom = ET.parse(pom_path).getroot()
    ns = {'m': 'http://maven.apache.org/POM/4.0.0'}
    group = pom.findtext('m:groupId', namespaces=ns)
    name = pom.findtext('m:artifactId', namespaces=ns)
    assert pom.findtext('m:version', namespaces=ns) == version
    for field in ['name', 'description', 'url', 'licenses', 'developers', 'scm']:
        assert pom.find('m:' + field, ns) is not None, 'Missing Central metadata: ' + field
    manifest = json.loads((args.packages / 'manifest.json').read_text())
    jar_name = f'{name}-{version}.jar'
    jar_source = args.packages / jar_name
    entry = next(artifact for artifact in manifest['artifacts'] if artifact['name'] == jar_name)
    assert hashlib.sha256(jar_source.read_bytes()).hexdigest() == entry['sha256']
    output = args.out.resolve()
    output.mkdir(parents=True, exist_ok=True)
    if any(output.iterdir()):
        raise ValueError('Central output must be empty')
    relative = Path(*group.split('.')) / name / version
    artifact_dir = output / 'content' / relative
    artifact_dir.mkdir(parents=True)
    shutil.copyfile(jar_source, artifact_dir / jar_name)
    shutil.copyfile(pom_path, artifact_dir / f'{name}-{version}.pom')
    source_dir = ROOT / 'sdk/java/src'
    zip_directory(artifact_dir / f'{name}-{version}-sources.jar', source_dir)
    with tempfile.TemporaryDirectory(prefix='licentivo-javadoc-') as temp:
        javadoc_dir = Path(temp)
        executable = args.java_home / 'bin' / ('javadoc.exe' if __import__('os').name == 'nt' else 'javadoc')
        subprocess.run([str(executable), '-quiet', '-encoding', 'UTF-8', '--release', '17', '-Xdoclint:none', '-d', str(javadoc_dir), *map(str, sorted(source_dir.rglob('*.java')))], check=True)
        for document in ['LICENSE', 'NOTICE']:
            shutil.copyfile(ROOT / document, javadoc_dir / document)
        zip_directory(artifact_dir / f'{name}-{version}-javadoc.jar', javadoc_dir)
    files = sorted(artifact_dir.iterdir())
    assert len(files) == 4
    pgp_key = None
    if args.pgp_private_key:
        from pgpy import PGPKey
        from pgpy.constants import HashAlgorithm
        pgp_key, _ = PGPKey.from_file(str(args.pgp_private_key))
        assert str(pgp_key.fingerprint) == args.signing_key
        assert not pgp_key.is_public and pgp_key.is_protected
    for artifact in files:
        raw = artifact.read_bytes()
        if pgp_key:
            with pgp_key.unlock(args.passphrase_file.read_text().strip()):
                signature = pgp_key.sign(raw, hash=HashAlgorithm.SHA256)
            assert bool(pgp_key.pubkey.verify(raw, signature)), 'OpenPGP signature verification failed'
            Path(str(artifact) + '.asc').write_text(str(signature), encoding='ascii')
        else:
            subprocess.run([args.gpg, '--homedir', str(args.gpg_home), '--batch', '--yes', '--pinentry-mode', 'loopback', '--passphrase-file', str(args.passphrase_file), '--local-user', args.signing_key, '--armor', '--detach-sign', '--output', str(artifact) + '.asc', str(artifact)], check=True)
            subprocess.run([args.gpg, '--homedir', str(args.gpg_home), '--batch', '--verify', str(artifact) + '.asc', str(artifact)], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        for algorithm in ['md5', 'sha1', 'sha256', 'sha512']:
            Path(str(artifact) + '.' + algorithm).write_text(hashlib.new(algorithm, raw).hexdigest(), encoding='ascii')
    bundle = output / f'{name}-{version}-central.zip'
    zip_directory(bundle, output / 'content')
    receipt = {'version': version, 'coordinates': f'{group}:{name}:{version}', 'bundle': bundle.name, 'sha256': hashlib.sha256(bundle.read_bytes()).hexdigest(), 'signed_artifacts': 4, 'network_actions': 0}
    (output / 'build.json').write_text(json.dumps(receipt, indent=2) + '\n', encoding='utf-8')
    print(json.dumps(receipt))


if __name__ == '__main__':
    main()
