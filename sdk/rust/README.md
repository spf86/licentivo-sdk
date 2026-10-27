# Licentivo Rust SDK

[Website](https://www.licentivo.com/) · [Documentation](https://www.licentivo.com/en/docs.html#sdk) · [Source code](https://github.com/spf86/licentivo-sdk)

Add license activation and real feature permissions to your own application.
The SDK handles requests, machine identity, pinned signatures, private credentials,
cache and policy heartbeats. Your app supplies activation input and business code.

## Install into your project

Requirements: Rust 1.77+, C11 compiler and libcurl development files (OpenSSL development files on Linux).

Install the public SDK package in your existing application project:

```sh
cargo add licentivo@=0.11.5
```

No SDK source download, extraction or local path dependency is required.

## Initialize from product settings in code

In Console > Products, copy your Product ID. Click that product's **Signing public
keys** and copy every key ID/public key pair into trustedKeys / trusted_keys below.
Replace the three YOUR_ placeholders. Quick integration can generate the public
code constants for your own product. No configuration file is needed. The SDK
defaults to `https://www.licentivo.com`; a software version is optional unless
your policy uses version/maintenance controls. Each product has its own signing
keys; customers of the same product share its public verification pins.

Read customerKey / customer_key from your activation form. Accept a customer's
ordinary license key or temporary transfer code; never use a vendor management key.

```rust
use licentivo::{Client, Error};

fn protected_export(customer_key: &str) -> Result<(), Error> {
    let trusted_keys = r#"{"YOUR_KEY_ID":"YOUR_PUBLIC_KEY"}"#;
    let mut client = Client::open_product("YOUR_PRODUCT_ID", trusted_keys, customer_key)?;
    client.run_feature("export", || {
        std::fs::write("report.csv", "Your report\n")?;
        Ok(())
    })?;
    Ok(()) // Real application: retain client until shutdown; Drop handles cleanup.
}
```

Place the feature gate in the actual button/shortcut/command business handler.
Denied operations do not invoke the callback. After first activation, pass an
empty key to restore the saved credential for this product and user. Ordinary
close retains device registration; `deactivate` is for explicit device release.
Floating close attempts to return its seat, with server expiry as fallback.
Use `refresh_online` to force an online check. Offline behavior follows the signed
policy and expiry; a confirmed server denial blocks protected features.

## Metering and advanced operations

Use `run_metered_feature` for a configured usage counter, passing a distinct stable job ID,
quantity and your business callback. The SDK reserves, executes, commits/cancels
and records pending settlement. A known successful job retry settles without
rerunning business. Persist the actual business result; after a crash during a
callback, inspect its outcome and pass the truth to `resolve_metered_feature`. The SDK does not
guess or replay an uncertain business action. Metering requires a network.

The lower-level reserve/commit/cancel and immediate-consume methods remain
available. Offline request/response files are a separate disconnected-computer
workflow. For private deployments, proxy settings or app versions, construct the
language's SDK configuration object directly in code and use its lower-level
constructor/start API. TLS and public-key pinning remain enabled.

The binding uses the shared C core. Client is Send, not Sync; move ownership to a worker for synchronous network work. There are no separate activate/start/check_feature/timer/listener APIs. status and feature_status return local JSON. Error exposes code, http_status, request_id, retryable and retry_after_seconds. A confirmed job replay is Execution::AlreadyCompleted.

Close with `close` at application shutdown. Do not distribute test activation
keys, machine credentials, cached grants or vendor management API keys.
See [QUICKSTART.md](https://www.licentivo.com/en/docs.html#quickstart) and the
[small desktop example](https://www.licentivo.com/en/docs.html#example).
MIT licensed; bundled dependencies retain their licenses (NOTICE).

For advanced in-code JSON settings, use `Client::open_config(config_json)`. It starts automatically and accepts base_url, cache_path and app_version without a file.
