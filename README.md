# Licentivo client SDKs

Integrate license activation, signed feature permissions, device binding and
usage limits into your own Go, Java, C, C++, C#, Python, Node.js, Rust or Ruby app.

Start with [QUICKSTART.md](QUICKSTART.md). To try a small application already
connected to the SDK, download the ready-to-run Windows application from the
[Quick example page](https://www.licentivo.com/zh/docs.html#example). Its runtime
is included. Optional source and build instructions are in [EXAMPLE.md](EXAMPLE.md);
the Windows desktop entry is
under 50 lines and demonstrates an allowed CSV export and a denied Pro export.
The public example issues a separate three-day license to each visitor, starting
at that license's first activation. Reclaiming/downloading does not extend it.

## Install in your application

Use the public package manager for your language. Run the command in your own
application project; no source download, extraction or local path override is
needed. All commands below select SDK 0.11.0.

| Language | Install |
| --- | --- |
| Go | `go get github.com/spf86/licentivo-sdk@v0.11.0` |
| Python | `python -m pip install "licentivo-sdk==0.11.0"` |
| Node.js | `npm install @licentivo/sdk@0.11.0` |
| C# | `dotnet add package Licentivo.SDK --version 0.11.0` |
| Java | Gradle: `implementation 'com.licentivo:licentivo-sdk:0.11.0'` with Maven Central enabled; [Maven configuration](sdk/java/README.md) |
| Rust | `cargo add licentivo@=0.11.0` |
| Ruby | `gem install licentivo-sdk --version 0.11.0` |
| C / C++ | Use the [automatic native package installer](https://www.licentivo.com/en/docs.html#sdk), then link `Licentivo::C` / `Licentivo::CPP`. |

Package installation installs the common library. Product ID and trusted public
keys are supplied by your application initialization code below.

## Product settings in code

Copy Product ID from Console > Products and the key ID/public key pairs from the
same product's Signing public keys. Pass these to direct product initialization,
then pass the customer's activation input as the license key. All sample sources
mark the two public settings with BEGIN/END LICENTIVO PRODUCT comments.

There is no product configuration file. The official HTTPS server is the default.
No management API key, manual device ID or mandatory 1.0.0 application version is
needed for basic activation. Different products have their own signing keys;
customers of the same product share its public verification pins. Customer
activation keys and machine credentials remain separate.

## Runtime behavior

The SDK makes requests, derives machine identity, verifies pinned signatures,
checks signed expiry, stores a private machine credential/cache and schedules
policy heartbeats. After first activation, an empty activation input restores
this device's credential. Keep one client alive through the application lifetime.
Ordinary close preserves a node-locked binding; explicit deactivation is for a
customer-requested device release. Closing a floating client attempts to return
its seat; the server reclaims unreturned seats at session expiry.

`RunFeature` checks entitlement before calling your real business operation. It
does not deduct usage. `FeatureStatus` explains a local denial; it does not make
a request or query remaining quota. Explicit refresh contacts the server; valid
offline behavior follows the signed policy and expiry. Every business path,
including alternate buttons/shortcuts/commands, must use the same feature gate.

## Feature consumption

`RunMeteredFeature(feature, quantity, stableJobID, callback)` handles reserve,
execution and commit/cancel. Pending confirmations are saved privately. Retain a
stable ID per real job and durable business results. A retry of an already
completed callback settles usage without running it again. If a process dies
during a callback, inspect the actual business result, then use the resolve
method with the true outcome. The SDK does not guess whether a business action
completed. Metering needs network access; the lower-level reserve/commit/cancel
and immediate-consume APIs remain available for custom flows.

## Toolchains and diagnostics

Go 1.26+, Java 17+, .NET 8+, Python 3.10+, Node.js 20+, Rust 1.77+ and Ruby 3.1+
are supported. C/C++ use C11/C++17, CMake and libcurl development files. Rust and
Ruby share that native core; on Windows use GNU/MinGW and matching libcurl, with
LICENTIVO_CURL_ROOT set for native builds. Python desktop examples require Tk.
SDK version: 0.11.0. Use the pinned package manager commands above.

Online errors expose a code and, when available, request ID/retry information.
Present the reason to the user. Local status does not guarantee a later network
operation will succeed. Keep TLS and pinned-key checks enabled. Never ship test
license keys, cached grants, machine credentials or vendor management API keys.
Offline request/response files are a separate feature for disconnected computers.

The shared signature vectors and protocol integration tests cover all bindings.
Run scripts/test-sdks.ps1 or scripts/test-sdks.sh for the supported local toolchain.
Platform build configuration alone is not evidence of execution on that platform.

MIT licensed; bundled dependencies retain their licenses, listed in NOTICE.
