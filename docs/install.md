# Install Licentivo SDK 0.11.1

Run the package manager command inside your own application project.

| Language | Install |
| --- | --- |
| Go | `go get github.com/spf86/licentivo-sdk@v0.11.1` |
| Python | `python -m pip install "licentivo-sdk==0.11.1"` |
| Node.js | `npm install @licentivo/sdk@0.11.1` |
| C# | `dotnet add package Licentivo.SDK --version 0.11.1` |
| Java | Gradle dependency: `implementation 'com.licentivo:licentivo-sdk:0.11.1'`; enable Maven Central. The Java README also has Maven XML. |
| Rust | `cargo add licentivo@=0.11.1` |
| Ruby | `gem install licentivo-sdk --version 0.11.1` |

No source ZIP, manual extraction or local path override is needed. C/C++ use the
automatic, checksum-verified native package installer on the [SDK installation
page](https://www.licentivo.com/en/docs.html#sdk). Choose a compiler-compatible
native package and link `Licentivo::C` / `Licentivo::CPP` from CMake.

Copy Product ID from Console > Products and every key ID/public key pair from
that product's Signing public keys into initialization arguments. The official
HTTPS service is the SDK default. Read the customer's key from your activation
screen; an empty key restores this machine after its first successful activation.
Keep the SDK client alive until the application exits.

Go requires Go 1.26+, Java JDK 17+, C# .NET 8+, Python 3.10+, Node.js 20+, Rust
1.77+ and Ruby 3.1+. Native C/C++/Rust/Ruby builds need matching C11/libcurl tools;
on Windows the Rust native core uses GNU/MinGW and LICENTIVO_CURL_ROOT. Python
desktop examples use Tk. The Windows desktop example requires .NET desktop SDK.

QUICKSTART.md guides integration into your software. EXAMPLE.md describes the
separate small application, whose editable source is provided for learning.
