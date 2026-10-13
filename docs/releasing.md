# Maintainer release procedure

Distribute one SDK version through its public language registries, GitHub Releases and the website installers. These tools stage client packages; they never deploy a service or contact a license/payment API.

## Build one version

Update VERSION and all package metadata together. `build-release.py` checks every language version and the standalone Go module. Export this client-only repository using the server workspace's `scripts/export-sdk.py`; never publish the server checkout or private configured examples.

Build in separate empty directories:

```bash
# Linux x64: all nine toolchains and native build dependencies
python scripts/build-release.py --out build-assets/linux
python scripts/test-install.py --packages build-assets/linux
```

```powershell
# Windows GNU/SEH MinGW, Rust GNU, RubyInstaller UCRT and matching libcurl
$env:LICENTIVO_CURL_ROOT='C:/tools/curl'
python scripts/build-release.py --bindings-only --curl-root $env:LICENTIVO_CURL_ROOT --out build-assets/windows-mingw
python scripts/test-install.py --native-only --curl-root $env:LICENTIVO_CURL_ROOT --packages build-assets/windows-mingw
```

An additional MSVC `--native-only` build supplies the optional MSVC C/C++ package. A full Windows build/test can check all nine clients separately. Universal packages come from one full build; platform jobs supply only distinct native libraries and Ruby platform gems. Different bytes under the same asset name are rejected.

```bash
python scripts/merge-release.py --input build-assets --out dist
```

The merged contract requires all nine source ZIPs, wheel, npm tarball, JAR, NuGet package, crate, source gem, Linux/Windows UCRT platform gems, Linux/Windows MinGW CMake packages, and both installers. Input manifests and checksums are verified before copying. Missing/unexpected files, duplicate records, symlinks, invalid versions and conflicting contents stop publication.

## Stage for the website

```bash
python scripts/publish-website.py --packages dist --output WEBSITE_ASSETS/sdk
```

This creates `releases/vVERSION/`, manifest/checksums, stable installers and `latest.json`. Published versions are immutable: changed bytes require a new version, and latest cannot move backwards. Staging performs no network request. Deploy through the website's existing procedure after consumer checks and a recoverable backup.

In the Licentivo server workspace, use `web/assets/sdk` as the output. Assets are embedded when the website binary is rebuilt. Generated packages are ignored by server Git. Update `web/assets/js/sdk-install.js`, installer defaults and translated documentation when changing VERSION.

After deployment, verify delivered files against the local manifest. SDK activation and consumption tests run only locally or on the designated test machine. Production release checks only read delivered files and service health.

## GitHub and registry publication

The `.github/workflows/sdk.yml` builds Linux clients, Windows MSVC native libraries and Windows GNU Rust/Ruby/native packages. A matching `vVERSION` tag assembles assets before GitHub Release creation. Its Python publication job uses a PyPI trusted publisher restricted to this repository, workflow and the `pypi` environment. Build/test jobs do not receive publishing credentials.

Go can then use `go get github.com/spf86/licentivo-sdk@vVERSION`. npm, PyPI, NuGet.org, Maven Central, crates.io and RubyGems require their own package-owner authentication and publication. Advertise registry commands only after a specific version is publicly available and verified in a fresh consumer. The website installer works independently of these registries.

For Maven Central, run `scripts/build-central.py --help` and provide the verified release directory, JDK 17+, a protected release-signing key and its private passphrase file. The tool creates the JAR, sources, Javadoc and complete POM, verifies detached signatures, adds checksums and produces a Portal upload bundle. Upload only the public bundle and public signing key; keep private keys, passwords and registry tokens outside Git.
