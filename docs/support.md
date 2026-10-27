# Supported runtimes and verification

Version 0.11.5 uses one signed-lease protocol across nine client languages.

| Runtime | Minimum | Execution evidence |
| --- | --- | --- |
| Go | 1.26 | Windows x64, Linux x64 |
| Java | JDK 17 | Windows x64, Linux x64 |
| C / C++ | C11 / C++17, libcurl; OpenSSL off Windows | Windows x64 (MinGW), Linux x64 |
| C# | .NET 8 | Windows x64, Linux x64; WinForms example is Windows-only |
| Python | 3.10, cryptography | Windows x64, Linux x64; window example needs Tk |
| Node.js | 20 | Windows x64, Linux x64; optional undici for an explicit proxy |
| Rust | 1.77, C11, libcurl; OpenSSL off Windows | Windows x64 (modern GNU/SEH MinGW), Linux x64 |
| Ruby | 3.1, Fiddle; native C core and libcurl | Windows x64, Linux x64; console examples and source/platform gems |

Shared checks cover 48 signed-lease vectors, wrong signatures, product/device mismatch, session context, expiry and maintenance. Runtime tests cover activation, encrypted credential restoration, offline cache rules, floating seats, device transfer and metered reserve/commit/cancel. These tests use isolated local test environments.

macOS code and commands exist. Physical Mac acceptance and ARM64 execution are pending; the presence of a CI configuration is not a claim of completed acceptance. Android and iOS adapters are not included.

Native binaries depend on compiler ABI and system libraries. A MinGW static library is not an MSVC library. The common installer selects matching CMake binary packages on tested Windows/Linux x64 targets and falls back to sources on other targets; compile sources with the application's own toolchain.

Rust and Ruby share the canonical C licensing core. Rust builds it through Cargo.
Ruby loads it through Fiddle; the platform gem contains the compiled core, while
the source gem builds it during installation. Source gems require RubyInstaller
Devkit/MSYS2 on Windows. Rust does not currently claim Windows MSVC acceptance.
Their physical Mac and ARM64 acceptance remains pending.

Metered operations require a network connection. RunMeteredFeature manages reserve/business/commit/cancel and a private task journal across restarts. Replaying a pending commit never reruns completed business. Your application must persist business operation IDs and results: if it crashes during business, inspect that result before ResolveMeteredFeature. A task ID cannot be reused with a different feature or quantity; use different IDs across processes and keep records while retries remain possible. One workflow runs per client. Remote revocation cannot immediately reach a machine that remains completely offline. Full VM clones, spoofed identities and modified applications are outside the simple cache-copy protection guarantee.

Package installation and client tests do not validate payment-provider transactions. The SDK does not charge your software's customers; it enforces and reports configured functional consumption.
