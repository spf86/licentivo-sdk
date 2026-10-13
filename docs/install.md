# Install and integrate

Version **0.10.0** supports the verified registry installation commands in the [live installation guide](https://www.licentivo.com/en/docs.html#sdk), plus the website and offline installation methods below. Start with [the common installation commands](../README.md#common-installation) or the [Chinese guide](quickstart.zh-CN.md). Obtain your `product.json` from Licentivo Quick integration before activating a customer's license.

## Installer options

| Purpose | PowerShell | Bash |
| --- | --- | --- |
| Select language | `-Language python` | `python` |
| Pin a version | `-Version 0.10.0` | `--version 0.10.0` |
| Choose local SDK folder | `-Destination .licentivo-sdk` | `--destination .licentivo-sdk` |
| C# / Rust project | `-Project ./MyApp.csproj` or `-Project ./Cargo.toml` | `--project ./MyApp.csproj` or `--project ./Cargo.toml` |
| Choose Python | `-PythonExe ./.venv/Scripts/python.exe` | `--python ./.venv/bin/python` |
| Download without installing | `-DownloadOnly` | `--download-only` |
| Use downloaded assets offline | `-PackageDirectory ./release-files` | `--package-dir ./release-files` |
| Alternate HTTPS release root | `-ReleaseBaseURL https://HOST/assets/sdk/releases` | `--base-url https://HOST/assets/sdk/releases` |
| Windows C/C++ ABI | `-Toolchain mingw` or `msvc` | `--toolchain mingw` or `msvc` |

The offline directory must include the required package and `SHA256SUMS`. A checksum mismatch stops installation. Downloaded files stay in a version-specific directory. No account password, management API key or GitHub token is needed to install this public release.

## Go

```bash
go mod init example.com/my-application    # only if your project has no go.mod
bash install-licentivo.sh go --version 0.10.0
```

Import `licentivo "github.com/spf86/licentivo-sdk"`. The website installer adds the version-pinned requirement and a local `replace` pointing at the verified standalone module. Retain the SDK source directory with your project, and update by rerunning the installer with a new version. The public Go module is installed with `go get github.com/spf86/licentivo-sdk@v0.10.0`; it contains only clients and examples.

## Python

```bash
python -m venv .venv
# Activate your environment or use its Python executable directly.
bash install-licentivo.sh python --version 0.10.0 --python .venv/bin/python
```

Import `from licentivo import Client`. The package manager installs the declared `cryptography` dependency. Avoid replacing system Python packages; a virtual environment keeps application dependencies separate.

## Rust

Run the installer inside your existing Cargo application; it uses `cargo add --path` and retains other dependencies. Use `--project path/to/Cargo.toml` when the manifest is elsewhere. The configured source ZIP also contains `sdk/rust/Cargo.toml`, the complete starter, a console application and the bundled C core. Its manual equivalent is:

```toml
[dependencies]
licentivo = { path = "SDK_ROOT/sdk/rust" }
```

Rust requires a C11 compiler and libcurl development files; OpenSSL off Windows.
On Windows use the GNU Rust target with modern SEH MinGW and set
`LICENTIVO_CURL_ROOT` to matching curl headers/libraries. Cargo compiles the core.
Run `cargo run --manifest-path sdk/rust/Cargo.toml --example app -- product.json`
for status, export, metered export, refresh and device transfer. To install from
crates.io, run `cargo add licentivo@=0.10.0` in your application project.

## Ruby

For a path dependency, add `gem 'licentivo-sdk', path: 'SDK_ROOT/sdk/ruby'` to your
Gemfile, run `ruby SDK_ROOT/sdk/ruby/build_native.rb` once, then `require 'licentivo'`.
The source launcher performs that native build for you. The console demonstration
is `ruby sdk/ruby/examples/app.rb product.json`.

The release installer uses the checksum manifest to select `licentivo-sdk-0.10.0-PLATFORM.gem` for your installed
Ruby platform and installs inside `.licentivo-sdk/ruby/v0.10.0/gems`. Add that folder
to `GEM_PATH`; it does not alter global gem settings. This package contains the
compiled core and Windows libcurl DLL/license, so installation needs no compiler.
It prints the exact per-terminal `GEM_PATH` command. Keep the installed gem directory with your application. If no matching platform package appears in the manifest, the installer selects the universal source gem instead of requesting a nonexistent package. Linux still requires compatible libcurl/OpenSSL runtime libraries. The universal
source `.gem` requires Ruby development headers, C11, libcurl development files
and make (RubyInstaller Devkit/MSYS2 on Windows). The live guide lists RubyGems
availability and the exact versioned command. Ruby blocks encapsulate the actual business operation and raise on failure.

## Node.js

```bash
bash install-licentivo.sh javascript --version 0.10.0
```

Import `import {Client} from '@licentivo/sdk'`. Keep the generated package lockfile. The SDK runs in Node.js or a desktop application's Node main process; it is not a web-browser SDK. Install `undici` separately when selecting an explicit outbound proxy.

## C# / .NET

Run the common installer in your project, selecting `csharp` and the `.csproj` if needed. It downloads `Licentivo.SDK.0.10.0.nupkg`, adds its local package source to the project's `NuGet.Config`, adds the version-pinned package reference and restores dependencies. Existing feeds and NuGet settings are retained; inherited user/parent sources continue to apply. It does not change global NuGet sources. If your project has intentionally cleared all sources, retain a feed containing `BouncyCastle.Cryptography`.

Manual equivalent, after downloading the package into `.licentivo-sdk/csharp/v0.10.0`:

```xml
<!-- Your application's NuGet.Config: preserve other sources and settings. -->
<configuration>
  <packageSources>
    <add key="licentivo-release-0.10.0" value=".licentivo-sdk/csharp/v0.10.0" />
    <add key="nuget.org" value="https://api.nuget.org/v3/index.json" />
  </packageSources>
</configuration>
```

```bash
dotnet add MyApp.csproj package Licentivo.SDK --version 0.10.0 --no-restore
dotnet restore MyApp.csproj
```

For application projects with private feeds, add the local SDK source to your existing project NuGet configuration instead of dropping the private feeds.

## Java

The installer downloads `licentivo-sdk-0.10.0.jar`. For Gradle:

```groovy
dependencies {
    implementation(files('.licentivo-sdk/java/v0.10.0/licentivo-sdk-0.10.0.jar'))
}
```

For Maven, install the Release JAR into your local Maven repository, then declare the dependency:

```bash
mvn install:install-file -Dfile=.licentivo-sdk/java/v0.10.0/licentivo-sdk-0.10.0.jar -DgroupId=com.licentivo -DartifactId=licentivo-sdk -Dversion=0.10.0 -Dpackaging=jar -DgeneratePom=true
```

```xml
<dependency>
  <groupId>com.licentivo</groupId>
  <artifactId>licentivo-sdk</artifactId>
  <version>0.10.0</version>
</dependency>
```

When installing from the website, retain the downloaded JAR or its locally installed package. For Maven Central availability and the Maven/Gradle dependency declarations, see the live guide or [Java README](../sdk/java/README.md).

## C and C++

The installer selects the Linux x64 or Windows x64 MinGW native package when available. For Windows MSVC, select `-Toolchain msvc`; when that binary is absent it extracts portable sources. Other platforms use the source package. Always match your application compiler and ABI. For a precompiled package, configure with `-DCMAKE_PREFIX_PATH=ABSOLUTE_PROJECT_PATH/.licentivo-sdk/cpp/v0.10.0/native` and use:

```cmake
find_package(Licentivo 0.10 REQUIRED)
target_link_libraries(your_application PRIVATE Licentivo::CPP) # Licentivo::C for C
```

For portable sources, download with `-DownloadOnly` or `--download-only` from a release without a matching native package, or directly extract `licentivo-cpp-0.10.0.zip`. Build/install it:

```bash
cmake -S .licentivo-sdk/cpp/v0.10.0/source/sdk -B .licentivo-sdk/native-build -DCMAKE_INSTALL_PREFIX="$PWD/.licentivo-sdk/native"
cmake --build .licentivo-sdk/native-build --config Release
cmake --install .licentivo-sdk/native-build --config Release
```

Then `find_package(Licentivo 0.10 REQUIRED)` and link `Licentivo::C` or `Licentivo::CPP`; point `CMAKE_PREFIX_PATH` at the install prefix. Release native binary packages are optional conveniences for matching compiler targets. Windows MinGW and MSVC artifacts cannot be interchanged. libcurl and required trusted CA certificates must be available; certificate verification remains enabled.

## Update

Choose an explicit newer release and rerun the relevant installer or language command. Keep your product settings and private credential/cache directories. Package updates do not replace signing pins, activate a license or transfer a device. Review release notes before upgrading. [Runtime integration and fields](https://www.licentivo.com/en/docs.html).
