# Small SDK application

The Windows desktop example has a license input, status, Export CSV, Pro export
and Refresh license. Its main program is under 50 lines, excluding the SDK.
The product ID and signing public keys are already in the downloaded source.
There is no product configuration file to download or place beside your app.

## Run the desktop application

1. On the website's [Quick example page](https://www.licentivo.com/zh/docs.html#example),
   download the Windows x64 application. Extract the ZIP and double-click
   `LicentivoExample.exe`. Its .NET runtime is included; no development tools,
   compiler, CMake or .NET installation are needed to try the application.
2. Claim your personal three-day license on that page.
   Each visitor has a separate license. Its three days begin at first activation;
   reclaiming or downloading again does not renew it.
3. Paste your trial key into the application and activate it.
4. Export CSV saves a real report. Pro export is blocked by the SDK before any
   report is written. This trial enables `export` and does not enable `pro`.
5. Close and open the application again; activate with an empty input to restore
   the SDK's saved device credential. This does not start a new trial.

## Study or modify the optional source

Expand **Source (optional)** on the same page to download the desktop application's
C# source, or select another language example. Source downloads do not claim or
activate a trial. Extract the source, prepare its development tools and run the
matching command below from the extracted directory.

| Application source | Command | Development tools |
| --- | --- | --- |
| Windows desktop (C#) | `dotnet run --project sdk/csharp/winforms` | .NET 8+ SDK, Windows |
| Desktop (Java) | `javac -encoding UTF-8 -d build sdk/java/src/licentivo/*.java` then `java -cp build licentivo.DesktopDemo` | JDK 17+ |
| Desktop (Python) | `python sdk/python/desktop.py` | Python 3.10+, Tk |
| Go console | `go run ./sdk/go/app` | Go 1.26+ |
| Node.js console | `node sdk/javascript/app.mjs` | Node.js 20+ |
| C / C++ console | Build `licentivo_c_app` / `licentivo_cpp_app` with CMake | C11 / C++17, libcurl |
| Rust console | `cargo run --manifest-path sdk/rust/Cargo.toml --example app` | Rust 1.77+, C11, libcurl |
| Ruby console | `ruby sdk/ruby/build_native.rb` then `ruby sdk/ruby/examples/app.rb` | Ruby 3.1+, C11, libcurl |

Only the chosen language's files are included. Python needs Tk (often
preinstalled on Windows/macOS). Native Windows builds need matching libcurl
development files; see the binding's README. Windows CMake defaults to a generator
available on that computer; GCC/MinGW users should add `-G "MinGW Makefiles"` to
the configure command and use a separate build directory from any Visual Studio
build. Stop if configuration or compilation fails. The ready-to-run Windows
application above is the recommended way to experience the SDK before compiling.

To integrate your own software, use [QUICKSTART.md](QUICKSTART.md). Copy your own
product ID and all key ID/public key pairs from Console > Products > Signing
public keys into initialization. The SDK defaults to `https://www.licentivo.com`.
Use `RunFeature` at the actual business entry point, and keep the SDK client alive
until application shutdown. Normal exit retains a node-locked device's activation.
