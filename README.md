# Licentivo client SDKs

[简体中文](README.zh-CN.md) · [First integration](QUICKSTART.md) · [Integration documentation](https://www.licentivo.com/en/docs.html)

Add activation, device binding, expiry, feature access and metered operations to your software. This repository contains the Go, Java, C, C++, C#, Python, Node.js, Rust and Ruby clients, examples and release tools. The server and administration application are separate.

## Features and how they work

| Capability | SDK behavior | What your application supplies |
| --- | --- | --- |
| Activation and restart | Activates a customer's license, saves a machine-scoped credential and restores it on subsequent starts | An activation screen and reusable `product.json` settings |
| Device binding and transfer | Derives the device identity and supports explicit deactivation and replacement-device codes | A customer-requested transfer action |
| License and feature decisions | Checks expiry and feature grants before invoking a protected callback | Your actual export, save, compute or other business function |
| Signature verification | Verifies signed grants against pinned public keys; invalid or mismatched grants are rejected | Product settings containing the correct public signing keys |
| Heartbeat and offline use | Schedules policy heartbeats and uses verified offline grants within the configured policy | Keep the client alive; choose the policy in the Licentivo console |
| Floating seats | Obtains a running instance's seat and attempts to return it when the client closes | Close the client when the application exits |
| Metered features | Reserves usage, executes the callback, commits success or cancels failure and journals pending settlement | A stable operation ID and a durable record of the actual business result |
| Offline file activation | Creates an offline request and verifies an imported activation response | File selection/import UI for disconnected computers |
| Diagnostics | Provides local status/feature decisions and structured errors for online operations | Present an appropriate message and retain the request ID for support |

Metered business operations require an online connection. A pending confirmation retries settlement without repeating an already successful callback. If a process crashes during the callback, the application must inspect its durable result before reporting success or failure. The SDK does not decide whether an external business operation completed.

## Choose an installation method

The [live installation guide](https://www.licentivo.com/en/docs.html#sdk) shows each language's verified registry version and exact package-manager command. Use Go modules, pip/PyPI, npm, NuGet, Maven/Gradle, Cargo/crates.io or RubyGems where the version is available. C/C++ use the versioned CMake source/native packages. The website installers below cover all nine languages and also work with previously downloaded packages offline.

Version **0.10.0** is available through these public routes:

| Language | Install in your application project |
| --- | --- |
| Go | `go get github.com/spf86/licentivo-sdk@v0.10.0` |
| Python | `python -m pip install "licentivo-sdk==0.10.0"` |
| Node.js | `npm install @licentivo/sdk@0.10.0` |
| C# | `dotnet add package Licentivo.SDK --version 0.10.0` |
| Java | Maven dependency `com.licentivo:licentivo-sdk:0.10.0`; [Maven and Gradle declarations](sdk/java/README.md) |
| Rust | `cargo add licentivo@=0.10.0` |
| Ruby | `gem install licentivo-sdk --version 0.10.0`; Gemfile: `gem 'licentivo-sdk', '= 0.10.0'` |
| C / C++ | [Download the package matching your compiler](https://github.com/spf86/licentivo-sdk/releases/tag/v0.10.0), set `CMAKE_PREFIX_PATH`, then `find_package(Licentivo 0.10 REQUIRED)` |

These packages contain public client code. Installation does not require a Licentivo account or management API key; activating your application uses the product configuration and a customer's license. [Requirements, native dependencies and offline installation](docs/install.md).

## Run locally first

Version **0.10.0** is distributed by the [Licentivo website](https://www.licentivo.com/en/docs.html#sdk) and the verified language-specific registries listed there. All nine languages use the same version, common installers, release manifest and SHA-256 checksums. A configured source ZIP from your workspace also works offline; follow [QUICKSTART.md](QUICKSTART.md).

At its root, run `./start.ps1 -CheckOnly` and `./start.ps1 -TestLicense`, or `bash start.sh --check-only` and `bash start.sh --test-license`. Success writes `licensed-report.txt`. Run without the test flag to restore activation. The guide shows the exact starter files and local dependency commands for moving the gate into your existing application.

RunFeature wraps an entitlement check and your callback. RunMeteredFeature wraps reserve, business, commit/cancel and a private pending-task journal. Keep the customer UI and durable business results in your application; the SDK handles the licensing protocol.

## Common installation

Run in your application's project directory. Choose one language; the installer verifies its versioned package before invoking your package manager or extracting it. Keep the `.licentivo-sdk` directory with your project. Python users should activate a virtual environment first. Create `go.mod`, `package.json` or `Cargo.toml` with the normal language tools if starting a new project.

| Language | Installation | Requirements |
| --- | --- | --- |
| Go | `-Language go` / `bash install-licentivo.sh go`: adds a pinned requirement and local replacement for the independently packaged module | Go 1.26+; existing `go.mod`; no server checkout or GitHub access |
| Python | `-Language python` / `bash install-licentivo.sh python`: installs the wheel and declared dependencies with pip | Python 3.10+; virtual environment |
| Node.js | `-Language javascript` / `bash install-licentivo.sh javascript`: installs the npm tarball | Node.js 20+; existing `package.json`; import `@licentivo/sdk` |
| Java | Download `licentivo-sdk-0.10.0.jar`, or use the installer below | Java 17+; add the JAR to the classpath or Gradle dependencies |
| C# | Use the installer below to add the Release package and restore dependencies | .NET 8+; an existing `.csproj` |
| C / C++ | `-Language c` or `cpp`: extracts matching CMake binary packages, otherwise portable sources | C11 / C++17, CMake, matching compiler ABI and libcurl; OpenSSL on Linux/macOS |
| Rust | `-Language rust` / `bash install-licentivo.sh rust`: runs `cargo add --path` on your project; also ships `licentivo-0.10.0.crate` | Rust 1.77+, existing Cargo project, C11, libcurl; Windows GNU/SEH MinGW; OpenSSL off Windows |
| Ruby | Use the installer for the matching platform `.gem`; a source `.gem` is also provided | Ruby 3.1+ and Fiddle; native runtime dependencies. Source builds require C11, libcurl and make. |

The common installers verify every downloaded package against `SHA256SUMS` before installing or extracting it. They operate inside your project and do not change global package-manager settings. Review an installer before running it.

### Windows PowerShell

```powershell
Invoke-WebRequest https://www.licentivo.com/assets/sdk/releases/v0.10.0/install.ps1 -OutFile install-licentivo.ps1
./install-licentivo.ps1 -Language python -Version 0.10.0
# Supported values: go, python, javascript, java, csharp, c, cpp, rust, ruby
```

### Linux / macOS shell

```bash
curl --fail --location --proto '=https' --proto-redir '=https' --tlsv1.2 -o install-licentivo.sh \
  https://www.licentivo.com/assets/sdk/releases/v0.10.0/install.sh
bash install-licentivo.sh python --version 0.10.0
# Supported values: go, python, javascript, java, csharp, c, cpp, rust, ruby
```

[Detailed installation, CMake and NuGet instructions](docs/install.md) · [Release checks and supported platforms](docs/support.md)

## Activate your application

1. Open **Quick integration** in your Licentivo workspace, choose a language, and download your configured example.
2. Keep the reusable `product.json` containing your product UUID, HTTPS server address and pinned public signing keys. This SDK repository intentionally contains no customer credentials or usable product configuration.
3. Read a customer's license key from your activation screen. Open the SDK with that key. On subsequent launches, supply an empty key to restore the saved device credential.
4. Pass your business function to RunFeature, or RunMeteredFeature for configured counters. Keep the client alive while your application runs.

```go
package main

import (
    "context"
    "log"
    "os"
    licentivo "github.com/spf86/licentivo-sdk"
)

func main() {
    client, err := licentivo.OpenWithLicense(context.Background(), "product.json", "CUSTOMER_LICENSE_KEY", func(err error) { log.Print(err) })
    if err != nil { log.Fatal(err) }
    defer client.Close()
    if err = client.RunFeature("export", func() error {
        return os.WriteFile("report.txt", []byte("Report\n"), 0600)
    }); err != nil { log.Print(err); return }
    // In your real app, keep client alive until the application exits.
}
```

Python: `from licentivo import Client`. Node.js: `import {Client} from '@licentivo/sdk'`. Java: `import licentivo.Client`. C#: `using Licentivo;`. [All nine startup methods and runnable applications](sdk/README.md).

Device-bound licenses retain their binding when the application closes. Use explicit deactivation for a customer-requested release. Floating sessions attempt to return their seat on close. For configured counters, pass your callback and stable job ID to RunMeteredFeature; it handles reservation, success/failure settlement and pending confirmations across restarts. See [the runtime guide](sdk/README.md#feature-consumption).

## Platforms and trust

Windows x64 and Linux x64 have local execution evidence. macOS source adaptation exists; physical Mac and ARM64 acceptance are pending. Android and iOS dedicated SDKs are not included in this release.

Use HTTPS and your pinned signing public keys. Vendor management API keys belong on your backend, never in a customer application. Product settings, private activation test files, device credentials and cached leases have distinct roles; keep the private files out of Git.

## Build and release

```bash
python scripts/build-release.py --out dist
```

The tool validates the common `VERSION`, creates nine source ZIPs and installable packages, includes license notices, and produces `manifest.json` and `SHA256SUMS`. Linux full builds and Windows binding/native builds are merged with `scripts/merge-release.py`. Fresh consumer projects are checked by `scripts/test-install.py`.

To stage the verified assets for the website, run `python scripts/publish-website.py --packages dist --output WEBSITE_ASSETS/sdk`. This verifies all nine clients and both tested platforms, refuses missing/tampered packages, and keeps published versions immutable. Deploy the staged assets with the website. GitHub Actions build the clients in independent consumer projects and publish tagged releases after those checks pass. No release tool contacts a licensing service, database or payment provider. See [the maintainer release guide](docs/releasing.md).

Client code and installation tools: [MIT](LICENSE). Bundled third-party components retain their own licenses; see [NOTICE](NOTICE).

## License validity

Set validity days when issuing each timed or trial license, not in its authorization policy. Supply `duration_days` from 1 to 36500; perpetual licenses omit it. Counting starts at the first successful activation. Applying a policy preserves the license term, expiry and renewals. Existing SDK 0.10.0 clients enforce the signed expiry without code or configuration changes.

[Console and management API documentation](https://www.licentivo.com/en/docs.html#management)
