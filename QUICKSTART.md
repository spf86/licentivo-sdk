# First integration: protect one operation

Goal: activate your own product, export one file only when allowed, and move that gate into your application. Budget about 15 minutes **after your language toolchain is installed**. This is an integration path, not a guarantee for unfamiliar builds or missing native dependencies.

The SDK performs HTTP requests, device identity, pinned-signature checks, private credential/cache storage and policy heartbeats. You do not implement the JSON protocol. SDK startup restores a prior activation when the supplied key is empty. SDK shutdown returns floating seats and keeps node-locked device registration.

The nine supported languages are Go, Java, C, C++, C#, Python, Node.js, Rust and Ruby.

## 1. Get your configured source ZIP (about 2 minutes)

In Console > Development and integration > Quick integration, enter a product name, select node-locked licensing, create the product, choose your language and download the **configured** ZIP. The default example has the `export` entitlement. If using an existing product, enable the same feature code in its policy and license. Start with an unmetered feature; add a meter after the basic gate works.

Extract into a new directory. `product.json`, `sdk-demo.json`, `start.ps1`, `start.sh`, `SDK-LANGUAGE` and `sdk/` must remain together. Go also has `go.mod`, client sources and `internal/` at the root. `product.json` is public product settings; `sdk-demo.json` contains your private test key. Generic downloads require product settings from your console before running.

## 2. Check and run (about 3 minutes)

| Language | Required development environment |
| --- | --- |
| Rust | Rust 1.77+, a C11 compiler and libcurl development files; OpenSSL off Windows. Windows: GNU/MinGW target, modern SEH compiler and matching libcurl. |
| Ruby | Ruby 3.1+ with Fiddle, a C11 compiler and libcurl development files; OpenSSL off Windows. Installing a source gem also requires make. |
| Go | Go 1.26+ |
| Python | Python 3.10+, venv support and pip; the launcher creates `.venv` and installs the local SDK there |
| Node.js | Node.js 20+; no npm installation is needed to run the source example |
| Java | JDK 17+, including `javac` |
| C# | .NET SDK 8+; NuGet must be reachable or its dependencies cached |
| C / C++ | C11 / C++17 compiler, CMake 3.18+, libcurl development files; Linux also needs OpenSSL development files. On Windows use matching compiler/architecture, required curl DLLs and trusted CA certificates. |

Run at the extracted root. Windows PowerShell:

```powershell
./start.ps1 -CheckOnly
./start.ps1 -TestLicense
```

Linux / macOS:

```bash
bash start.sh --check-only
bash start.sh --test-license
```

The check finds your product file and tools; it does not contact the license server or verify TLS. The run builds/installs only the selected language and supplies the test key to the child process without changing `product.json`. It keeps TLS verification enabled. If PowerShell policy blocks a downloaded script, inspect it and use `Unblock-File ./start.ps1` rather than changing machine-wide policy. A trusted deployment certificate is needed by SDKs; accepting a browser certificate warning does not make the SDK trust that certificate.

Success: exit code 0, with these lines and a new `licensed-report.txt`:

```text
License OK: export is available
Export completed: licensed-report.txt
```

Run again **without** `-TestLicense` / `--test-license`. The SDK restores the private device credential. Unlike the complete protocol demo, this starter does not unbind a node-locked device when it exits. A Windows MinGW installation without CMake can supply `-CurlRoot C:/path/to/curl`; the launcher builds the same C core and C++ wrapper directly. Rust and Ruby on Windows also require `-CurlRoot C:/path/to/curl` (or `LICENTIVO_CURL_ROOT`). Rust uses the GNU target; the current Rust toolchain needs a modern SEH MinGW compiler. See the language README before building. Select a custom Python using `-PythonExe` / `--python`.

## 3. Add the SDK to your existing application (about 4 minutes)

Use the [installation documentation](https://www.licentivo.com/en/docs.html#sdk) to add the pinned SDK version with your language's package manager. The common website installer also supports all nine languages and verifies SHA-256 checksums; it can install downloaded packages offline. Go and Rust project dependencies are added automatically; Java and C/C++ provide build references.

For an offline configured source bundle, use the extracted directory as `SDK_ROOT` below; replace it with its actual absolute path. Copy **only `product.json`** into your application's resources. This local route remains supported.

| Language | Add the local SDK | Starter file to read/copy |
| --- | --- | --- |
| Rust | Add `licentivo = { path = "SDK_ROOT/sdk/rust" }` under `[dependencies]` in your application Cargo.toml. Cargo builds the bundled core. | `sdk/rust/examples/quickstart.rs` |
| Ruby | Add `gem "licentivo-sdk", path: "SDK_ROOT/sdk/ruby"` to Gemfile; build the local path gem with `ruby SDK_ROOT/sdk/ruby/build_native.rb`. Import with `require "licentivo"`. | `sdk/ruby/examples/quickstart.rb` |
| Go | In your existing module: `go mod edit -require=github.com/spf86/licentivo-sdk@v0.10.0`, then `go mod edit -replace=github.com/spf86/licentivo-sdk=SDK_ROOT`. Keep the extracted root as that dependency; import `licentivo "github.com/spf86/licentivo-sdk"`. | `sdk/go/quickstart/main.go` (repository export: `examples/go/quickstart/main.go`) |
| Python | In your application's venv: `python -m pip install "SDK_ROOT/sdk/python"`; `from licentivo import Client`. | `sdk/python/quickstart.py` |
| Node.js | In your app: `npm install "SDK_ROOT/sdk/javascript"`; `import {Client} from '@licentivo/sdk'`. A desktop app uses its Node main process. | `sdk/javascript/quickstart.mjs` (change the source import to the package import) |
| Java | Add `SDK_ROOT/sdk/java/src/licentivo` to your existing Java sources, or build the local JAR. Import `licentivo.Client` in your own class. | `sdk/java/src/licentivo/Quickstart.java` (adapt its package/class name to your app) |
| C# | `dotnet add MyApp.csproj reference "SDK_ROOT/sdk/csharp/Licentivo.csproj"`; `using Licentivo;`. | `sdk/csharp/quickstart/Program.cs` |
| C / C++ | Add the extracted SDK with `add_subdirectory("SDK_ROOT/sdk" licentivo-build)`, then link your target with `Licentivo::C` or `Licentivo::CPP`. Set `LICENTIVO_BUILD_EXAMPLES` and `LICENTIVO_BUILD_TESTS` OFF before adding it. | `sdk/c/quickstart.c` / `sdk/cpp/quickstart.cpp` |

There are three places to change:

1. **Application startup or activation input:** create one client using `product.json` and the customer's key. Later starts pass an empty key. Resolve the product file from your app's installed resources; do not depend on an end user's current terminal directory. In a GUI, first-time network activation belongs on a worker/task, not the UI event loop.
2. **Business entry point:** put your existing export implementation inside `RunFeature` (language names below). The SDK checks entitlement before calling it. Use the same gate for button, shortcut, menu and command-line paths. Keep one client alive; do not activate anew for every export.
3. **Application shutdown:** close/dispose/destroy the client. Do not call Deactivate on every node-locked shutdown; Deactivate is for intentional unbinding/transfer.

| Language | Non-metered gate around your existing business operation |
| --- | --- |
| Rust | `client.run_feature("export", || export_report())?` where the callback returns `Result<T, licentivo::Error>` |
| Ruby | `client.run_feature("export") { export_report }`; raise on business failure |
| Go | `err := client.RunFeature("export", func() error { return exportReport() })` |
| Python | `client.run_feature("export", export_report)` |
| Node.js | `await client.runFeature('export', () => exportReport())` |
| Java | `client.runFeature("export", () -> exportReport())` |
| C# | `await client.RunFeature("export", () => ExportReport())` where ExportReport returns Task |
| C | `ln_run_feature(client, "export", export_report, context)`; callback returns 1 on success, 0 on failure |
| C++ | `client.RunFeature("export", [&] { exportReport(); })` |

Callbacks contain your business logic. They do not build authorization requests, calculate device IDs, parse signatures, store keys or schedule heartbeats. FeatureStatus/Status are local reads; use them to display a denied reason. Hide or disable the button if useful, and retain the SDK gate in the business handler.

## 4. Prove both allowed and denied paths (about 3 minutes)

Keep the successful file; run a denied feature into a different output path:

```powershell
./start.ps1 -Feature feature_not_granted -Output denied-report.txt
Test-Path ./denied-report.txt  # False in a fresh directory
```

```bash
bash start.sh --feature feature_not_granted --output denied-report.txt
test ! -e denied-report.txt
```

The denied run must exit nonzero and not create a file. In your application repeat the test through its actual business handler. Restore the feature code to `export`. At this point startup, successful business execution, denial and empty-key restart are integrated.

## 5. When you need feature counts

Enable an `export` meter in the product policy and a **fresh or updated license with that meter**. A boolean entitlement alone does not impose a usage count. Replace RunFeature with RunMeteredFeature; the SDK handles reserve, commit after success, and cancel after failure.

| Language | Metered business gate |
| --- | --- |
| Rust | `client.run_metered_feature("export", 1, &job_id, || export_report())?` |
| Ruby | `client.run_metered_feature("export", 1, job_id) { export_report }` |
| Go | `client.RunMeteredFeature(ctx, "export", 1, jobID, func() error { return exportReport() })` |
| Python | `client.run_metered_feature("export", 1, job_id, export_report)` |
| Node.js | `await client.runMeteredFeature('export', 1, jobID, () => exportReport())` |
| Java | `client.runMeteredFeature("export", 1, jobID, () -> exportReport())` |
| C# | `await client.RunMeteredFeature("export", 1, jobID, () => ExportReport())` |
| C | `ln_run_metered_feature(client, "export", 1, job_id, export_report, context)` |
| C++ | `client.RunMeteredFeature("export", 1, jobID, [&] { exportReport(); })` |

Use a stable, unique job ID (1-80 non-whitespace ASCII characters) for a business operation. Retain it with your business job; reuse it only to retry that same job with the same feature and quantity. To test the starter use `-Metered -OperationID demo-job-001` or `--metered --operation-id demo-job-001`. Test a failed callback and verify the reserved units are released.

The SDK writes a private task journal next to its cache before the callback, then marks a successful result as `commit_pending`. When confirmation fails, repeating the same metered call only retries commit; it never calls the business callback again. Completed IDs skip repeat execution. A failed callback attempts cancellation, and a later retry of its ID only retries cancellation. A new attempt at business requires a new job ID.

**Crash during business is different from a failed confirmation.** A `business_running` record cannot tell whether your database transaction or file output completed. Inspect your own durable business result, then call ResolveMeteredFeature / resolve_metered_feature / resolveMeteredFeature / ln_resolve_metered_feature with the job ID and known success flag. That method only settles the reservation, never invokes business. Do not delete the journal to force a rerun. If the hold already expired or the license is unavailable, surface the error for reconciliation. The SDK cannot make an external business transaction and a remote usage charge one atomic transaction.

Keep one client for the app and serialize metered workflows per client. Multiple processes must not run the same business job ID concurrently. Arbitrary user business results are not stored or replayed by the SDK. Retain task files for as long as job IDs may be retried; they are private runtime state and must not ship in customer installers.

Rust metered calls return `Execution<T>`; a retry that skips business returns `AlreadyCompleted`. Ruby returns `Execution`, with `replay?` and `value`. Load an existing business result from your own job record when a retry skips the callback. Both include a console application at `sdk/rust/examples/app.rs` / `sdk/ruby/examples/app.rb` with status, export, metered export, refresh and explicit device transfer.

## Troubleshooting the first integration

| Symptom | Check |
| --- | --- |
| `product.json` not found | Run at the extracted root; your actual app needs a resource path, not a terminal-dependent relative path. |
| Enter a license key / invalid configuration | First run uses the test flag or your customer's input. Empty-key restart requires the same product, origin, OS user and private credential location. |
| TLS/certificate error | Use the configured HTTPS origin, with a certificate trusted by the SDK runtime. Browser approval alone is insufficient; retain certificate verification. |
| FEATURE_NOT_ENTITLED | The exact feature code must appear in that customer's signed license. Test with `export`, not its translated display label. |
| Meter missing / FEATURE_QUOTA_EXCEEDED | Use a license with that meter and a remaining allowance. RunFeature checks permission; RunMeteredFeature enforces the count. |
| ACTIVATION_LIMIT_REACHED / floating seat occupied | Check actual activations/sessions in the console. Closing returns floating seats; transfer intentionally unbinds node-locked devices. |
| LICENSE_REVOKED / LICENSE_EXPIRED | Correct the license in the console or obtain a valid key; do not continue the protected operation. |
| Confirmation pending / outcome unknown | Retain the same job ID and journal; retry settlement or inspect the durable business outcome. Never repeat a successful export to retry charging. |
| Import/build dependency failure | Use the installed language version and correct architecture; see the environment table. Include the support code and request ID, never your license key, private credential or management API key. |

After this path, `sdk/README.md` is the reference for offline imports, transfer, floating seats, version/maintenance rules, status callbacks and low-level SDK methods. HTTP request/response documentation is a protocol reference for custom clients, not a prerequisite for these nine SDKs. macOS code exists but Mac/ARM physical acceptance is pending; there is no dedicated Android/iOS SDK yet.

## License validity

Set validity days when issuing each timed or trial license, not in its authorization policy. Supply `duration_days` from 1 to 36500; perpetual licenses omit it. Counting starts at the first successful activation. Applying a policy preserves the license term, expiry and renewals. Existing SDK 0.10.0 clients enforce the signed expiry without code or configuration changes.

[Console and management API documentation](https://www.licentivo.com/en/docs.html#management)
