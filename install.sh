#!/usr/bin/env bash
set -euo pipefail
language="${1:-}"
if [[ -z "$language" ]]; then echo 'Usage: bash install.sh LANGUAGE [--version 0.11.0] [--destination DIR] [--download-only]' >&2; exit 2; fi
shift
version=0.11.0
destination=.licentivo-sdk
project=''
python_exe=python3
package_dir=''
release_base_url='https://www.licentivo.com/assets/sdk/releases'
toolchain=mingw
download_only=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --version|--destination|--project|--python|--package-dir|--base-url|--toolchain)
      [[ $# -ge 2 ]] || { echo "Missing argument: $1" >&2; exit 2; }
      case "$1" in
        --version) version="$2";; --destination) destination="$2";; --project) project="$2";;
        --python) python_exe="$2";; --package-dir) package_dir="$2";;
        --base-url) release_base_url="$2";; --toolchain) toolchain="$2";;
      esac
      shift 2;;
    --download-only) download_only=1; shift;;
    *) echo "Unknown option: $1" >&2; exit 2;;
  esac
done
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo 'Use a semantic release version such as 0.11.0.' >&2; exit 2; }
case "$language" in go|python|javascript|java|csharp|c|cpp|rust|ruby) ;; *) echo "Unsupported language: $language" >&2; exit 2;; esac
# Normal online installation uses the language registry, without a source tree.
if [[ -z "$package_dir" && "$download_only" == 0 && "$language" != c && "$language" != cpp ]]; then
  case "$language" in
    go) go get "github.com/spf86/licentivo-sdk@v$version";;
    python) "$python_exe" -m pip install "licentivo-sdk==$version";;
    javascript) npm install "@licentivo/sdk@$version";;
    csharp) if [[ -n "$project" ]]; then dotnet add "$project" package Licentivo.SDK --version "$version"; else dotnet add package Licentivo.SDK --version "$version"; fi;;
    rust) if [[ -n "$project" ]]; then cargo add "licentivo@=$version" --manifest-path "$project"; else cargo add "licentivo@=$version"; fi;;
    ruby) gem install licentivo-sdk --version "$version";;
    java) echo "Add to Gradle dependencies (with mavenCentral()): implementation 'com.licentivo:licentivo-sdk:$version'";;
  esac
  exit 0
fi
if [[ "$language" == go || "$language" == c || "$language" == cpp || "$language" == rust ]]; then
  command -v unzip >/dev/null 2>&1 || { echo 'Install unzip before extracting the SDK source package.' >&2; exit 1; }
fi
[[ "$release_base_url" =~ ^https://[^/?#@]+(/[^?#]*)?$ ]] || { echo 'Base URL must be an absolute HTTPS directory URL.' >&2; exit 2; }
case "$toolchain" in mingw|msvc) ;; *) echo 'Toolchain must be mingw or msvc.' >&2; exit 2;; esac
release_url="${release_base_url%/}/v$version"
mkdir -p "$destination/$language/v$version"
install_root="$(cd "$destination/$language/v$version" && pwd)"
if [[ -n "$package_dir" ]]; then package_dir="$(cd "$package_dir" && pwd)"; fi
fetch() {
  local name="$1"
  if [[ -n "$package_dir" ]]; then
    if [[ "$package_dir/$name" != "$install_root/$name" ]]; then cp "$package_dir/$name" "$install_root/$name"; fi
  else
    curl --fail --location --proto '=https' --proto-redir '=https' --tlsv1.2 --output "$install_root/$name" "$release_url/$name"
  fi
}
verify() {
  local name="$1" expected actual count
  expected="$(awk -v file="$name" 'length($1)==64 && substr($0,67)==file {print $1}' "$install_root/SHA256SUMS")"
  count="$(printf '%s\n' "$expected" | awk 'NF {n++} END {print n+0}')"
  [[ "$count" == 1 && "$expected" =~ ^[a-f0-9]{64}$ ]] || { echo "Missing or ambiguous checksum: $name" >&2; exit 1; }
  if command -v sha256sum >/dev/null 2>&1; then actual="$(sha256sum "$install_root/$name" | awk '{print $1}')";
  else actual="$(shasum -a 256 "$install_root/$name" | awk '{print $1}')"; fi
  [[ "$actual" == "$expected" ]] || { echo "SHA-256 mismatch: $name. Installation stopped." >&2; exit 1; }
}
has_asset() {
  local count
  count="$(awk -v file="$1" 'length($1)==64 && substr($0,67)==file {n++} END {print n+0}' "$install_root/SHA256SUMS")"
  [[ "$count" == 1 ]]
}
fetch SHA256SUMS
case "$language" in
  python) asset="licentivo_sdk-$version-py3-none-any.whl";;
  javascript) asset="licentivo-sdk-$version.tgz";;
  java) asset="licentivo-sdk-$version.jar";;
  csharp) asset="Licentivo.SDK.$version.nupkg";;
  ruby)
    ruby_platform="$(ruby -e "require 'rubygems'; print Gem::Platform.local")"
    [[ "$ruby_platform" =~ ^[a-zA-Z0-9_-]+$ ]] || { echo 'Cannot determine the installed Ruby platform.' >&2; exit 1; }
    asset="licentivo-sdk-$version-$ruby_platform.gem"
    if ! has_asset "$asset"; then asset="licentivo-sdk-$version.gem"; fi;;
  c|cpp)
    candidate=''
    case "$(uname -s):$(uname -m)" in
      Linux:x86_64) candidate="licentivo-native-$version-linux-x64.zip";;
      MINGW*:x86_64|MSYS*:x86_64) candidate="licentivo-native-$version-windows-x64-$toolchain.zip";;
    esac
    if [[ -n "$candidate" ]] && has_asset "$candidate"; then asset="$candidate";
    else echo 'No verified native package matches this platform/compiler. See the supported platforms in the SDK installation guide.' >&2; exit 1; fi;;
  *) asset="licentivo-$language-$version.zip";;
esac
fetch "$asset"
verify "$asset"
echo "Downloaded and verified $asset"
if [[ "$language" == go || "$language" == c || "$language" == cpp || "$language" == rust ]]; then
  archive_paths="$(unzip -Z1 "$install_root/$asset")" || { echo 'Unreadable SDK archive.' >&2; exit 1; }
  if printf '%s\n' "$archive_paths" | grep -E '(^/|^[A-Za-z]:|(^|/)\.\.(/|$)|\\)' >/dev/null; then
    echo 'Unsafe path in SDK archive.' >&2; exit 1
  fi
  extracted="$install_root/source"
  if [[ "$asset" == licentivo-native-* ]]; then extracted="$install_root/native"; fi
  mkdir -p "$extracted"
  unzip -q -o "$install_root/$asset" -d "$extracted"
  echo "SDK files: $extracted"
fi
if [[ "$download_only" == 1 ]]; then echo 'Download complete; no package manager was run.'; exit 0; fi
case "$language" in
  rust)
    if [[ -z "$project" ]]; then project=Cargo.toml; fi
    [[ -f "$project" && "$(basename "$project")" == Cargo.toml ]] || { echo 'Run inside your Cargo project, or pass --project path/to/Cargo.toml. Use --download-only to fetch only.' >&2; exit 1; }
    cargo add --manifest-path "$project" --path "$install_root/source/sdk/rust"
    echo 'Import: use licentivo::Client; Cargo builds the C core. See docs/install.md for native prerequisites.';;
  ruby)
    ruby -S gem install --local "$install_root/$asset" --no-document --install-dir "$install_root/gems"
    echo "Ruby package installed. In this terminal run: export GEM_PATH=\"$install_root/gems:\$(ruby -e 'print Gem.path.join(File::PATH_SEPARATOR)')\""
    echo "Import: require 'licentivo'. Keep this directory with your application.";;
  go)
    [[ -f go.mod ]] || { echo 'Run inside your application module (go mod init first), or use --download-only.' >&2; exit 1; }
    go mod edit "-require=github.com/spf86/licentivo-sdk@v$version"
    go mod edit "-replace=github.com/spf86/licentivo-sdk=$install_root/source"
    echo 'Import: github.com/spf86/licentivo-sdk';;
  python)
    "$python_exe" -m pip install "$install_root/$asset"
    echo 'Import: from licentivo import Client';;
  javascript)
    [[ -f package.json ]] || { echo 'Run npm init first, or use --download-only.' >&2; exit 1; }
    npm install "$install_root/$asset"
    echo "Import: import {Client} from '@licentivo/sdk'";;
  java)
    echo "Add this JAR to your classpath or Gradle dependencies: $install_root/$asset";;
  csharp)
    if [[ -z "$project" ]]; then
      shopt -s nullglob
      projects=(*.csproj)
      if [[ ${#projects[@]} == 1 ]]; then project="${projects[0]}"; fi
    fi
    if [[ -z "$project" ]]; then echo 'Package ready. Rerun with --project your-app.csproj.'; exit 0; fi
    [[ -f "$project" && "$project" == *.csproj ]] || { echo 'Project must name an existing .csproj file.' >&2; exit 1; }
    project_folder="$(cd "$(dirname "$project")" && pwd)"
    config=''
    while IFS= read -r candidate; do
      [[ -z "$config" ]] || { echo 'Multiple project NuGet configurations; consolidate them first.' >&2; exit 1; }
      config="$candidate"
    done < <(find "$project_folder" -maxdepth 1 -type f -iname nuget.config)
    if [[ -z "$config" ]]; then
      config="$project_folder/NuGet.Config"
      printf '<configuration><packageSources /></configuration>\n' > "$config"
    fi
    feed_name="licentivo-release-$version"
    # Change only this installer's source; existing feeds and credentials remain.
    if dotnet nuget list source --configfile "$config" | grep -F " $feed_name " >/dev/null; then
      dotnet nuget remove source "$feed_name" --configfile "$config"
    fi
    dotnet nuget add source "$install_root" --name "$feed_name" --configfile "$config"
    dotnet add "$project" package Licentivo.SDK --version "$version" --no-restore
    dotnet restore "$project"
    echo 'Import: using Licentivo;';;
  c|cpp)
    if [[ "$asset" == licentivo-native-* ]]; then
      echo "CMake prefix: $extracted. Configure with -DCMAKE_PREFIX_PATH=$extracted, then find_package(Licentivo $version REQUIRED) and link Licentivo::C or Licentivo::CPP."
    else echo "Source package: add_subdirectory($extracted/sdk licentivo-build), then link Licentivo::C or Licentivo::CPP."; fi
    echo 'Use matching compiler/ABI and libcurl dependencies. See docs/install.md.';;
esac
