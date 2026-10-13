# Licentivo SDK: protect one operation

The Go, Java, C, C++, C#, Python, JavaScript, Rust and Ruby SDKs use the same signed-lease protocol. Customer applications activate with a license key or a one-time device-transfer code. Vendor management API keys belong on your server, never in the customer application.

**Start with QUICKSTART.md at the ZIP root** (in the server checkout: [QUICKSTART.md](../QUICKSTART.md)). It has environment checks, an actual file export, installation into an existing application, and a denied-feature check for all nine languages. With a toolchain already installed, budget about 15 minutes for that path. The [installation documentation](https://www.licentivo.com/en/docs.html#sdk) lists the verified package-manager commands for each language. The website also provides versioned packages, SHA-256 checksums and common/offline installers at https://www.licentivo.com/assets/sdk/releases/v0.10.0/.

## What the SDK does

Licentivo adds license activation, machine-bound credentials, expiry and feature access checks, pinned signature verification, policy heartbeats, permitted offline use, floating-seat cleanup and metered-feature settlement to an application. It also supports explicit device deactivation and offline activation request/response files.

Open a client with your product configuration and the customer's license key. The client activates or restores authorization and runs policy heartbeats. Pass a protected business operation to `RunFeature` or its language equivalent; the SDK checks permission before invoking it. For counters, use `RunMeteredFeature` with a stable job ID; the SDK handles reserve, commit/cancel and pending-confirmation recovery. Close the client when the application exits.

Your application provides the customer interface, the business function and durable business results. Vendor management API keys stay on the vendor's server. Public signing keys can be distributed with `product.json`; customer keys, activation credentials and caches remain private. SDK runtime and platform requirements and the actual installation commands are listed in the linked guide.

At the configured ZIP root, run `./start.ps1 -CheckOnly` then `./start.ps1 -TestLicense` on Windows, or `bash start.sh --check-only` then `bash start.sh --test-license` on Linux/macOS. Success creates `licensed-report.txt`. Run again without the test flag to restore the saved device credential. These starters keep node-locked registration on normal exit.

The SDK owns HTTP requests, identity, signature verification, encrypted device credentials, cache and heartbeat scheduling. Your application supplies customer activation input and its business callback. Use **RunFeature** for an ordinary entitlement or **RunMeteredFeature** for a feature with a configured counter; do not implement the protocol shown in advanced reference pages.

## Advanced: complete protocol demo

Open Quick integration in the console, enter the product name and choose a language. The console creates the product, policy, test license, public signing key and initial release. Download the configured ZIP and extract it. An existing product can use its own license key instead.

Run from the extracted root directory. The private `sdk-demo.json` contains the test license and public-key pins. Keep it out of source control and customer installations.

| Language | Requirements | Commands |
| --- | --- | --- |
| Go | Go 1.26+ | `go run ./examples/go/demo --config sdk-demo.json` |
| Java | JDK 17+ | `javac -encoding UTF-8 -d build sdk/java/src/licentivo/*.java`, then `java -cp build licentivo.Demo --config sdk-demo.json` |
| C | C11, CMake, libcurl | `cmake -S sdk -B build`, `cmake --build build`, then `build/licentivo_c_demo --config sdk-demo.json` |
| C++ | C++17; native dependencies above | Run `build/licentivo_cpp_demo --config sdk-demo.json` |
| C# | .NET 8+ | `dotnet run --project sdk/csharp/demo -- --config sdk-demo.json` |
| Python | Python 3.10+ | `python -m pip install -r sdk/python/requirements.txt`, then `python sdk/python/demo.py --config sdk-demo.json` |
| JavaScript | Node.js 20+ | `node sdk/javascript/demo.mjs --config sdk-demo.json` |
| Rust | Rust 1.77+, C11, libcurl | `cargo run --manifest-path sdk/rust/Cargo.toml --example app -- product.json` |
| Ruby | Ruby 3.1+, Fiddle, native core | `ruby sdk/ruby/examples/app.rb product.json` |

Windows PowerShell does not expand the Java wildcard: run `$sources = (Get-ChildItem sdk/java/src/licentivo/*.java).FullName`, then `javac -encoding UTF-8 -d build $sources`.

For C/C++ on Windows, use `cmake --build build --config Release`. Executables normally appear in `build/Release/` and have the `.exe` suffix; MinGW builds may place them directly in `build/`. The website provides commands for the selected operating system.

Linux needs a compiler, CMake, libcurl and OpenSSL development packages. On macOS, Homebrew can provide cmake, curl and openssl@3. Windows libraries and your compiler must target the same architecture. Distribute required DLLs and a trusted CA bundle. Certificate verification remains enabled.

The complete demo activates, validates, sends a heartbeat and releases its test device. Success means exit code 0; the Go demo ends with `Go: deactivated`.

## 2. Add licensing to your application

Configured downloads contain two files:

- `sdk-demo.json`: private test settings, including a test license.
- `product.json`: reusable product settings and public-key pins, with an empty license key.

Ship the required SDK files and `product.json`. Read the customer's own license key from your activation UI or private user settings and supply it to the startup method:

| Language | Start | Execute an unmetered feature | Close |
| --- | --- | --- | --- |
| Go | `OpenWithLicense(ctx, "product.json", key, onError)` | `RunFeature("export", exportReport)` | `Close()` |
| Java | `Client.openWithLicense("product.json", key, onError)` | `runFeature("export", this::exportReport)` | try-with-resources or `close()` |
| C | `ln_open_with_license("product.json", key)` | `ln_run_feature(client, "export", export_report, context)` | `ln_destroy(client)` |
| C++ | `licentivo::Client client("product.json", key); client.Start();` | `client.RunFeature("export", exportReport)` | Object destruction |
| C# | `await LicentivoClient.OpenWithLicense("product.json", key, onError)` | `await RunFeature("export", ExportReport)` | await using or `DisposeAsync()` |
| Python | `with Client.open_with_license("product.json", key, on_error) as client:` | `run_feature("export", export_report)` | Exit the with block |
| Rust | `Client::open_with_license("product.json", key)?` | `client.run_feature("export", export_report)?` | Drop or `close()` |
| Ruby | `Licentivo::Client.open_with_license("product.json", key)` | `client.run_feature("export") { export_report }` | Block ensure or `close` |
| JavaScript | `await Client.openWithLicense("product.json", key, onError)` | `await runFeature("export", exportReport)` | `await close()` |

Startup loads settings, activates or restores a valid cache, and schedules heartbeats. Keep the client alive for the application lifetime. Check the feature before every protected operation and block that operation when the check fails. Replace `export` with a feature from your authorization policy.

After activation with an ordinary `lv_lic_` key or temporary `lv_tmp_` code, the SDK saves a machine-bound `lv_dev_` credential. Subsequent starts supply an empty key to restore it; your application does not need to retain the original key. Keep the input until activation succeeds so an interrupted request can be retried. Website examples use `LICENTIVO_LICENSE_KEY` only as a substitute for an input field.

With a valid node-locked offline lease, startup restores authorization immediately and refreshes it online in the background, including when the timer is disabled. Online-only policies and floating licenses wait for a successful server response. Local feature checks do not contact the server or wait for background requests. Keep one client alive for the application lifetime.

Omit `cache_path` or leave it empty to select a private per-user directory scoped to this origin and product. `timeout_seconds` sets a 1-120 second request timeout, default 10. Transient requests can make two attempts. `proxy_url` selects an outbound proxy; Java accepts an HTTP proxy URL, and Node requires the optional `undici` peer dependency. TLS verification stays enabled.

Set `cache_path` to a writable per-user application-data location. Never distribute the test configuration, caches, device credentials or management API keys. Do not automatically trust a public key obtained from an unauthenticated response. Existing Open and Start entry points remain available.

For node-locked licenses, normal shutdown keeps the device binding. Call Deactivate only when the customer wants to release the device. Floating sessions try to return the seat on shutdown; after a crash or network failure, the server reclaims it when the session lease expires.

## Customer device transfers

Customers sign in to `/portal.html` using the license-associated email, select the old device, and confirm Unbind and transfer. The portal displays a one-time code valid for at most 15 minutes and can optionally email it. The email login link itself never releases a device.

Accept the `lv_tmp_` code in the ordinary activation field. After successful signature verification, the SDK stores a product- and machine-bound credential at `cache_path + ".credential.json"`. Heartbeats, validation and future starts use that credential. Expiry of the temporary code does not invalidate an already activated device.

The new device uses the original license. First activation, expiry, maintenance rights, features, consumption and customer ownership do not reset. A failed redemption does not consume the code. A retry with the same request identity recovers the original result. An unused expired code can be reissued from the same released-device record; a used code cannot be issued again from that old record.

Keep the temporary code until successful redemption. The SDK's retry record preserves an interrupted request. Afterwards, an empty license key can restore the credential using the same cache path.

A completely offline replacement machine can export a request with the temporary code, obtain a portal-approved response before the code expires, and import that response. The policy must permit offline node-locked activation. Demos offer `--transfer-test` and `--transfer-import`. Floating sessions still return seats on exit.

Remote release cannot immediately delete an old machine's offline cache. The next online request is denied. A permanently offline cache cannot learn a remote revocation until the machine reconnects.

## Feature consumption

CheckFeature and RunFeature verify entitlement, not remaining usage. For a policy allowing 500 exports per UTC month, use the metered callback instead. The SDK reserves before calling it, confirms after success and cancels after business failure:

| Language | Execute a metered feature |
| --- | --- |
| Go | `RunMeteredFeature(ctx, "export", 1, jobID, exportReport)` |
| Java | `runMeteredFeature("export", 1, jobID, this::exportReport)` |
| C | `ln_run_metered_feature(client, "export", 1, job_id, export_report, context)` |
| C++ | `RunMeteredFeature("export", 1, jobID, exportReport)` |
| C# | `await RunMeteredFeature("export", 1, jobID, ExportReport)` |
| Python | `run_metered_feature("export", 1, job_id, export_report)` |
| Rust | `run_metered_feature("export", 1, job_id, export_report)?` |
| Ruby | `run_metered_feature("export", 1, job_id) { export_report }` |
| Node.js | `await runMeteredFeature("export", 1, jobID, exportReport)` |

Use a stable, persisted job ID. The SDK stores the feature, quantity, reservation ID and outcome in a private task journal next to its cache. A commit retry, including after a restart, never reruns a callback known to have completed. Reusing the ID with another feature or quantity is rejected. A failed/canceled job needs a new ID for a new action. One workflow may run per client; different processes must use different job IDs. Retain completed records while their IDs can be retried.

If the process dies during the business callback, the journal says `business_running`, so the SDK refuses to guess whether the action completed. Inspect your durable business result, then call ResolveMeteredFeature / resolveMeteredFeature / resolve_metered_feature / ln_resolve_metered_feature with that outcome; this method only confirms or cancels and never executes your business. Remote consumption cannot be atomic with a local export. If the reservation has expired, retain the completed result and reconcile it; do not rerun it.

### Advanced: control the reservation yourself

The existing lower-level methods remain available when your workflow needs them:

1. Create and persist one stable job ID for this business action.
2. Reserve one unit. Only a `pending` response permits new work. A `committed` replay means the action already finished; never execute it again.
3. Execute the business action and persist its result.
4. Commit after success, or cancel if the action failed before completion.

| Language | Reserve | Confirm | Cancel |
| --- | --- | --- | --- |
| Go | `Reserve(ctx, "export", 1, jobID)` | `Commit(ctx, hold.ID, jobID)` | `Cancel(ctx, hold.ID, jobID)` |
| Java | `reserve("export", 1, jobID)` | `commit(id, jobID)` | `cancel(id, jobID)` |
| C | `ln_reserve(client, "export", 1, job_id)` | `ln_commit(client, id, job_id)` | `ln_cancel(client, id, job_id)` |
| C++ | `Reserve("export", 1, jobID)` | `Commit(id, jobID)` | `Cancel(id, jobID)` |
| C# | `await Reserve("export", 1, jobID)` | `await Commit(id, jobID)` | `await Cancel(id, jobID)` |
| Python | `reserve("export", 1, job_id)` | `commit(id, job_id)` | `cancel(id, job_id)` |
| Rust | `reserve("export", 1, job_id)?` | `commit(id, job_id)?` | `cancel(id, job_id)?` |
| Ruby | `reserve("export", 1, job_id)` | `commit(id, job_id)` | `cancel(id, job_id)` |
| Node.js | `await reserve("export", 1, jobID)` | `await commit(id, jobID)` | `await cancel(id, jobID)` |

Receipts contain `reservation_id`, `operation_id`, `status`, `expires_at` and `consumption`. A pending hold normally lasts 15 minutes and ends earlier at a UTC reset boundary. Direct API callers can set `reservation_seconds` to 30-3600. Finish before the deadline. A pre-approved pending hold retains its reserved units if allowance settings change before commit.

| Consumption field | Meaning |
| --- | --- |
| `used` | Confirmed uses in the period |
| `reserved` | Units held by all unexpired pending jobs |
| `limit` | Basic functional allowance, excluding permitted overage |
| `remaining` | `max(0, limit - used - reserved)`; excludes permitted overage |
| `quantity` | Units belonging to this job |
| `overage` | Confirmed uses above the basic allowance; does not collect money from your customer |
| `period` / `reset_at` | UTC month/day or lifetime and its next reset; lifetime has a null reset date |

A timed-out commit is uncertain: retry Commit with the same job ID and hold ID. Do not cancel after the business result exists, and do not repeat finished work. If a hold expires after completed work, retain the job for reconciliation. The SDK cannot make your local business transaction atomic with the remote server; production applications must persist their job/result state and recover unfinished jobs on startup.

The simpler Consume methods remain available for actions that are already final. They charge immediately and are not automatically refunded if later work fails:

| Language | Consume one unit |
| --- | --- |
| Go | `client.Consume(ctx, "export", 1, jobID)` |
| Java | `client.consume("export", 1, jobID)` |
| C | `ln_consume(client, "export", 1, job_id)`; release returned strings with `ln_free_string` |
| C++ | `client.Consume("export", 1, jobID)` |
| C# | `await client.Consume("export", 1, jobID)` |
| Python | `client.consume("export", 1, job_id)` |
| JavaScript | `await client.consume("export", 1, jobID)` |

Keep one stable job ID per business operation and reuse it for retries. Different operations need different IDs. Daily and monthly periods reset at UTC boundaries; lifetime consumption does not reset. Reserve, Commit, Cancel and Consume require an online server. Offline license checks do not provide offline consumption. Completed commits emit `license.consume`; provisional holds do not.

Policy overage allows extra functional uses and records their count. It does not collect money from your customer. Vendors handle their own customer charges. These functional counts are separate from platform device and API billing. Consume does not add to the four billable runtime API operations.

## Completely offline first activation

Do not call Open on a machine that cannot connect. Create an unstarted Client, New instance or `ln_init_file` instance, then:

1. Call OfflineRequest with `activate` and save the request file on the target machine.
2. Carry the file to an online machine. Upload it in the console's Offline activation page or the associated customer's portal and download the signed response.
3. Carry the response back and call ImportOffline. The SDK verifies the request identity, signature, machine, product and version before saving the lease.
4. CheckFeature locally within the signed offline allowance.

Go accepts JSON bytes; Java/C/C++ use JSON text; C# imports JSON bytes; Python and JavaScript use parsed objects. Demos support `--offline-export` and `--offline-import`. C strings must be freed with `ln_free_string`.

Requests are valid for 30 days. License and maintenance periods begin when the server first approves activation, not when the response reaches the target machine. Floating licenses require online seat acquisition and do not support first activation through this file workflow.

## Heartbeat, offline cache and machine identity

Set heartbeat interval and offline allowance independently in the authorization policy. Interval 0 disables the timer; other intervals are 600-86400 seconds. Scheduling adds 0-10% random delay and never shortens the interval below 600 seconds. Startup still refreshes a valid cache in the background when the timer is disabled. Permanent offline permission does not extend a timed license beyond its expiry. Successful online validation refreshes current rules; an offline cache keeps its signed rules.

For an online-only policy, the current signature includes the maximum scheduling delay and 30 seconds for a normal request. Startup still waits for the server, and a failed check stops protected operations. Floating policies require a seat lease longer than this interval. If your configured HTTP timeout exceeds that grace, an unresponsive server can cause a temporary denial while the request is still pending. Limited offline caches expire at the configured allowance; choose an allowance long enough to cover the normal check interval if uninterrupted access is required.

Transport failures, timeouts, 429 and temporary 5xx responses permit fallback to an otherwise valid cache, including an HTML error from a reverse proxy. Explicit server denial invalidates the cache. A clock rollback exceeding five minutes requires an online check. A floating lease belongs to the current process session, so restarting cannot indefinitely retain an old seat.

The SDK derives product-specific identity from the local Windows, Linux or macOS system and available hardware data; it never uploads the raw identity. A configured device_id does not replace local identity. Policies can require hardware data, reject detected virtual machines or migrate a binding after an OS reinstall when the hardware matches.

Ordinary copying of configuration and caches to another machine is rejected. Full VM clones, spoofed hardware/system identities or modified application binaries are outside this guarantee. Test strict hardware policies on the actual target environment, particularly where hardware data cannot be read.

## Local packages and verification

`scripts/build-sdks.ps1` and `bash scripts/build-sdks.sh` create source ZIPs, a Python wheel, npm tgz, Java JAR, NuGet package and a native installation directory with checksums under `.local/packages`.

- Python: `pip install ./licentivo_sdk-0.10.0-py3-none-any.whl`.
- Node.js: `npm install ./licentivo-sdk-0.10.0.tgz`, then `import {Client} from '@licentivo/sdk'`.
- Java: add the JAR to the classpath; Maven metadata is in `sdk/java/pom.xml`.
- .NET: add the package directory as a NuGet source and reference `Licentivo.SDK`.
- C/C++: add the installation directory to CMAKE_PREFIX_PATH, call `find_package(Licentivo 0.10 REQUIRED)`, and link `Licentivo::C` or `Licentivo::CPP`. The Windows native package targets MinGW x64 and needs compatible compiler/libcurl binaries.
- Go: the Go ZIP is an independent `github.com/spf86/licentivo-sdk` module with its own verifier and no server imports. After publication, install `go get github.com/spf86/licentivo-sdk@v0.10.0`. An offline ZIP can use `replace github.com/spf86/licentivo-sdk => /path/to/extracted-go-sdk`.

These are local packages, not automatic public registry publications. Publishing requires the appropriate repository, account and package names.

## Authorization status and notifications

Status methods are local reads. They expose `state`, `allowed`, `code`, `request_id`, `retryable` and `lease_valid_until`. The last field is the signed cache deadline, not necessarily the final license expiry date. `allowed` describes local license validity, not remaining metered units.

| State | Meaning |
| --- | --- |
| `active` | Last online verification succeeded and local authorization is valid |
| `offline_valid` | Signed local authorization is valid; no online confirmation has succeeded since this start |
| `verification_required` | No usable signed authorization; stop protected operations and activate/refresh |
| `expired` / `revoked` / `released` | Server explicitly denied the license or device |
| `quota_exhausted` | A metered request exceeded its allowance; other entitled features can remain usable |

Go/C#/C++ use Status and OnStatusChanged (C# has a StatusChanged event); Java/Node use status; Python uses status and on_status_changed; C uses ln_status and ln_on_status_changed. Every language also provides FeatureStatus / featureStatus / feature_status / ln_feature_status for the named entitlement; C's returned JSON string is freed with ln_free_string. These local views do not check a metered allowance. Marshal callbacks to your UI thread; C callbacks must not destroy the client. Typed HTTP errors contain status, code, request ID, retryability and Retry-After seconds, using each language's naming convention. Transport exceptions can remain native exceptions; the status view provides NETWORK_UNAVAILABLE.

## Customer application examples

These examples accept an activation key, display status, write an actual protected `licensed-report.txt`, and keep the client alive. Use the policy's export feature; select metered export only when export has a quota rule. Leave the key empty to restore the saved credential.

| Language | Command from the ZIP root |
| --- | --- |
| Go | `go run ./examples/go/app product.json` |
| Java | Compile the sources as above, then `java -cp build licentivo.DesktopDemo` |
| C | Build with CMake, then `build/licentivo_c_app product.json` |
| C++ | `build/licentivo_cpp_app product.json` |
| C# | On Windows, `dotnet run --project sdk/csharp/winforms` |
| Python | `python sdk/python/desktop.py`; requires Tk support |
| Node.js | `node sdk/javascript/app.mjs product.json` |

Java Swing, Python Tk and C# WinForms have activation forms. The other examples have terminal menus. They show commit retries without repeating the report. The Python example includes a durable export journal. Other examples retain pending jobs only for the current session: add durable job recovery before using their metered flow in production.

The 48 shared signature vectors include session, maintenance and offline-request context. `scripts/test-sdks.ps1 -Integration` or `bash scripts/test-sdks.sh --integration` runs against a dedicated `_test` MySQL database, covering requests, cache recovery, seats, consumption and offline files. SMTP and real payments are not required.

On Windows, add `-Desktop` to test the actual Java Swing, Python Tk and C# WinForms controls and event loops. The file selector receives a temporary test path; authorization still uses the real test API and MySQL database. The test activates once, restores with an empty key, cancels a failed export, commits one successful export, retries without charging again and releases the device. Python must include Tk support.

CI configurations target Linux x64/ARM64 and macOS Intel/ARM64. Windows x64 has local execution evidence. A workflow configuration alone does not prove that another platform has passed.
