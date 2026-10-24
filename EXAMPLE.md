# Local SDK sample applications

For integration into your own application, start with [QUICKSTART.md](QUICKSTART.md)
or Console Quickstart's English integration prompt. This page describes the
optional sample sources in this repository.

## Configure a sample

Create a product, a policy and a test license in the console. Copy your product ID
and trusted key ID/public key pairs into the sample's BEGIN/END LICENTIVO PRODUCT
section. Do not put signing private keys or management API keys in a client.
Use the test license at the sample's interactive activation prompt.

Enable the feature actually used by the sample, such as `export`, and leave a
second feature such as `pro` disabled to verify denial. A trial license starts
its configured duration at first successful activation; restart or device release
does not renew it.

## Build and run from this checkout

| Sample | Command | Requirements |
| --- | --- | --- |
| Windows desktop (C#) | `dotnet run --project sdk/csharp/winforms` | .NET 8 SDK, Windows |
| Java desktop | `javac -encoding UTF-8 -d build sdk/java/src/licentivo/*.java`, then `java -cp build licentivo.DesktopDemo` | JDK 17 |
| Python desktop | `python sdk/python/desktop.py` | Python 3.10, Tk, cryptography |
| Go console | `go run ./examples/go/app` | Go 1.26 |
| Node.js console | `node sdk/javascript/app.mjs` | Node.js 20 |
| C / C++ console | Build `licentivo_c_app` / `licentivo_cpp_app` with CMake | C11 / C++17, libcurl |
| Rust console | `cargo run --manifest-path sdk/rust/Cargo.toml --example app` | Rust 1.77, C11, libcurl |
| Ruby console | `ruby sdk/ruby/build_native.rb`, then `ruby sdk/ruby/examples/app.rb` | Ruby 3.1, C11, libcurl |

Native Windows builds require a matching compiler architecture and libcurl.
Use the binding's README for toolchain settings. For MinGW CMake builds, select
an available generator, such as `-G "MinGW Makefiles"`, and a separate build
directory from any MSVC build. Stop after a failed configure or compilation.

Verify a real allowed action, a denied action and credential restoration after
restart. Keep one SDK client alive for the application lifetime. Ordinary close
keeps a node-locked binding; explicit logout/deactivation releases it. Floating
close attempts to return the seat, with lease expiry as the recovery boundary.
