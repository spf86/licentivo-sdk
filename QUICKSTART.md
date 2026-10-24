# Integrate licensing into your own software

This guide starts inside your existing application. First protect one real
operation, then add the capabilities your product's policy actually uses.

## Fastest path: use the English AI integration prompt

1. Open [Console Quickstart](https://www.licentivo.com/en/dashboard.html#integration).
2. Create or select your product, create a policy, then issue a test license.
3. Choose your application's language and system. Copy the generated English
   prompt into an AI coding tool with your existing project open.
4. Run the integrated application. Return to Quickstart, use **Copy activation
   key** beside the prompt, and manually paste the key into your app's activation
   screen or interactive CLI. The prompt does not contain the full key.
5. Execute a real protected operation and restart the app to verify credential
   recovery. Review the [acceptance checklist](https://www.licentivo.com/en/docs.html#acceptance).

The key remains available in that browser tab for up to 24 hours, including after
refresh or navigating back, for the same account and workspace. Logging out or
clearing the temporary key removes it. Download the license text if you need to
keep it longer. The license list only shows a prefix, not the complete key.

For manual implementation, follow the steps below or the
[manual integration guide](https://www.licentivo.com/en/docs.html#integration-guide).

## 1. Install the SDK in your application

Select your language in the [SDK installation guide](https://www.licentivo.com/en/docs.html#sdk).
Install SDK 0.11.2 with your language's package manager. For Go, run:

```sh
go get github.com/spf86/licentivo-sdk@v0.11.2
```

Other languages use the pinned commands in [README.md](README.md#install-in-your-application).
Java uses a Maven/Gradle dependency; C/C++ use the automatic native package installer.
Do not download/extract SDK source or add a local path dependency.
Installing the common SDK does not select a product or include its public keys.

## 2. Copy your product identity and public verification keys into code

In Console > Products, create/select your software's product and copy its Product
ID. Click that product's **Signing public keys** and copy **every key ID and public
key pair**. Replace `YOUR_PRODUCT_ID`, `YOUR_KEY_ID` and `YOUR_PUBLIC_KEY` in your
initialization code. Console Quickstart includes this actual configuration in the
English integration prompt.

There is no product configuration file to download, preserve or place in a working
directory. The SDK defaults to `https://www.licentivo.com`; ordinary integration
does not require a server address, a management API key or a software version.
Different products have their own signing keys. Customers of one product share
the public verification keys; each customer has their own license and device
credential. Private signing keys remain on the server.

Enable your feature code, for example `export`, in the product's authorization
policy and issue a test license. For timed/trial licenses, select duration when
issuing the license; its clock begins at first activation. Version/maintenance
controls are optional; only those controls need the application's actual version.

## 3. Connect initialization, the real business entry point and shutdown

| Application location | Integration |
| --- | --- |
| Startup / activation screen | Call the language's direct product initialization below, supplying product ID, trusted keys and the customer's activation input. Keep the client alive. |
| Export button, shortcut or command | Pass the actual business function to `RunFeature` (language naming varies). All paths to that operation must use this gate. A denial skips the callback. |
| Application exit | Close/dispose the client. This stops background work and attempts to return floating seats; it retains an ordinary device binding. |

| Language | Startup API | Source showing initialization and a protected callback |
| --- | --- | --- |
| Go | `OpenProduct(ctx, productID, trustedKeys, customerKey, onError)` | `examples/go/quickstart/main.go` |
| Java | `Client.openProduct(productID, trustedKeys, customerKey, onError)` | `sdk/java/src/licentivo/Quickstart.java` |
| C | `ln_init_product(product_id, trusted_keys_json, customer_key)` then `ln_start` | `sdk/c/quickstart.c` |
| C++ | `Client::ForProduct(productID, trustedKeysJSON, customerKey)` then `Start` | `sdk/cpp/quickstart.cpp` |
| C# | `await LicentivoClient.OpenProduct(productId, trustedKeys, customerKey)` | `sdk/csharp/quickstart/Program.cs` |
| Python | `Client.open_product(product_id, trusted_keys, customer_key)` | `sdk/python/quickstart.py` |
| Node.js | `await Client.openProduct(productId, trustedKeys, customerKey)` | `sdk/javascript/quickstart.mjs` |
| Rust | `Client::open_product(product_id, trusted_keys_json, customer_key)` | `sdk/rust/examples/quickstart.rs` |
| Ruby | `Client.open_product(product_id, trusted_keys, customer_key)` | `sdk/ruby/examples/quickstart.rb` |

Replace the example's report writer with the actual function you want to protect.
GUI applications must keep initial network calls off their UI thread (or await
them, for asynchronous SDKs). The SDK performs requests, device identity, pinned
signature verification, private credential/cache storage and policy heartbeats.

## 4. Verify in your own application

1. Manually paste your test key into the app, activate and execute an allowed feature.
2. Close and reopen under the same user. Pass an empty key to restore the SDK's
   saved machine credential; customers do not need to retype their key.
3. Try a feature absent from the license. Its business callback must not run or
   produce an output file. Restore the correct feature name after this check.
4. Use a local fixture to test expired, revoked and released responses. A
   confirmed server denial must block the protected operation. Offline use is
   bounded by the already signed policy and lease expiry. Use dedicated test
   licenses for any live changes, and record which scenarios were actually tested.

## 5. Publish your integrated software and issue customer licenses

Build the SDK and product initialization into your software and distribute that
version. Issue a separate license to each customer; they activate inside your app.
Do not distribute development test keys, cached grants, machine credentials or
vendor management API keys. Updating a license controls the checks you have
actually connected to your business functions.

## Optional advanced settings

For a private deployment or a proxy, build the language's configuration object in
code, then create/start the client with the lower-level constructor. `base_url`
defaults to the official HTTPS origin, `cache_path` defaults to a private current
user location, and `app_version` defaults to empty. HTTP overrides are restricted
to loopback development. No JSON file is needed.

For usage limits, use `RunMeteredFeature` with a distinct, stable job ID and your
business callback. It reserves, runs, confirms/cancels and journals pending
settlement. Retain durable business results; after a crash during a callback,
inspect its real outcome before resolving the job. Repeating a known completed
job retries settlement without executing its business function again.

For floating seats, acquire a seat online on each process start, keep the client
alive, and close it on normal exit. A cached lease cannot authorize a new process
or extend an expired seat. Only call Deactivate for an explicit device transfer.

For an air-gapped device, export its offline request, process it on an online
machine, and import the matching signed response back on the original machine.
The policy must permit that offline mode. See [offline activation](https://www.licentivo.com/en/docs.html#offline).

When using version or maintenance restrictions, configure the real application
version; maintenance checks also require that version to be published in the
console. Refresh online after an application upgrade. Without these restrictions,
do not create a dummy software release just to activate.

Pin trusted product public keys in the application; never accept unknown keys
from an activation response. Keep device credentials and caches in a private
per-user location. For web apps, protect real operations on a trusted backend.
Management API keys belong only on your server for issuance and administration.

For all nine language APIs and exact parameter types, see the [runtime
reference](https://www.licentivo.com/en/docs.html#runtime). For common failures and
safe retry behavior, see [troubleshooting](https://www.licentivo.com/en/docs.html#errors).
