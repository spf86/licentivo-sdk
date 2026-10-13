#!/usr/bin/env bash
# Builds/runs the local source bundle; never downloads an unpublished GitHub release.
set -euo pipefail
cd "$(dirname "$0")"
language=""; test_license=0; check_only=0; config=product.json; feature=export; output=licensed-report.txt; metered=0; operation_id=""; python_exe=python3
[[ ! -f SDK-LANGUAGE ]] || language="$(cat SDK-LANGUAGE)"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --language) language="$2"; shift 2;;
    --test-license) test_license=1; shift;;
    --check-only) check_only=1; shift;;
    --config) config="$2"; shift 2;;
    --feature) feature="$2"; shift 2;;
    --output) output="$2"; shift 2;;
    --metered) metered=1; shift;;
    --operation-id) operation_id="$2"; shift 2;;
    --python) python_exe="$2"; shift 2;;
    *) printf 'Unknown option: %s. See QUICKSTART.md.\n' "$1" >&2; exit 2;;
  esac
done
need(){ command -v "$1" >/dev/null || { printf 'Missing %s. Install the development tools in QUICKSTART.md.\n' "$1" >&2; exit 2; }; }
[[ -f "$config" ]] || { echo 'Missing product.json. Download a configured ZIP from Quick integration.' >&2; exit 2; }
case "$language" in
  go) need go; go version;;
  java) need javac; need java; javac -version;;
  csharp) need dotnet; dotnet --version;;
  python) need "$python_exe"; "$python_exe" -c 'import sys; assert sys.version_info >= (3,10), "Python 3.10+ required"; print(sys.version.split()[0])';;
  javascript) need node; node -e 'if(Number(process.versions.node.split(".")[0])<20)throw Error("Node.js 20+ required");console.log(process.versions.node)';;
  rust) need cargo; need cc; cargo --version;;
  ruby) need ruby; need cc; ruby -e 'require "fiddle"; abort "Ruby 3.1+ required" if (RUBY_VERSION.split(".").map(&:to_i)[0,2] <=> [3,1]) == -1; puts RUBY_VERSION';;
  c|cpp) need cmake; cmake --version;;
  *) echo 'Select --language go, java, c, cpp, csharp, python, javascript, rust or ruby.' >&2; exit 2;;
esac
if [[ "$check_only" == 1 ]]; then echo 'Environment and product file found. Configuration, TLS and signatures will be validated by the SDK at startup.'; exit 0; fi
if [[ "$test_license" == 1 ]]; then
  [[ -f sdk-demo.json ]] || { echo 'Missing sdk-demo.json. Download a configured ZIP or supply LICENTIVO_LICENSE_KEY.' >&2; exit 2; }
  # Generated JSON contains a canonical ASCII test key. Never echo it or write it to product.json.
  LICENTIVO_LICENSE_KEY="$(sed -nE 's/.*"license_key"[[:space:]]*:[[:space:]]*"(lv_(lic|tmp)_[A-Za-z0-9_-]{43})".*/\1/p' sdk-demo.json)"
  [[ "$LICENTIVO_LICENSE_KEY" =~ ^lv_(lic|tmp)_[A-Za-z0-9_-]{43}$ ]] || { echo 'Missing or malformed test key in sdk-demo.json.' >&2; exit 2; }
  export LICENTIVO_LICENSE_KEY
fi
[[ "$metered" == 0 || -n "$operation_id" ]] || { echo 'Supply --operation-id with a stable job ID for --metered.' >&2; exit 2; }
export LICENTIVO_CONFIG="$config" LICENTIVO_FEATURE="$feature" LICENTIVO_OUTPUT="$output" LICENTIVO_METERED="$metered" LICENTIVO_OPERATION_ID="$operation_id"
case "$language" in
  go) entry=./sdk/go/quickstart; [[ -d "$entry" ]] || entry=./examples/go/quickstart; go run "$entry";;
  java) mkdir -p .local/quickstart/java; javac -encoding UTF-8 -d .local/quickstart/java sdk/java/src/licentivo/*.java; java -cp .local/quickstart/java licentivo.Quickstart;;
  csharp) dotnet run --project sdk/csharp/quickstart;;
  python)
    [[ -f .venv/bin/python ]] || "$python_exe" -m venv .venv
    .venv/bin/python -m pip install --disable-pip-version-check -e ./sdk/python
    .venv/bin/python sdk/python/quickstart.py;;
  javascript) node sdk/javascript/quickstart.mjs;;
  rust) cargo run --manifest-path sdk/rust/Cargo.toml --example quickstart;;
  ruby) ruby sdk/ruby/build_native.rb; ruby sdk/ruby/examples/quickstart.rb;;
  c|cpp)
    cmake -S sdk -B .local/quickstart/native -DCMAKE_BUILD_TYPE=Release -DLICENTIVO_BUILD_TESTS=OFF
    cmake --build .local/quickstart/native --config Release --target "licentivo_${language}_quickstart"
    ".local/quickstart/native/licentivo_${language}_quickstart";;
esac
