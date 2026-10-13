# Licentivo Rust SDK

Start with the repository's `QUICKSTART.md`. The complete application in
`examples/quickstart.rs` activates/restores a device and actually exports a file.
It also supports `LICENTIVO_METERED=1` and a stable `LICENTIVO_OPERATION_ID`.

Prerequisites: Rust 1.77+, a C11 compiler and libcurl development files. Linux also
needs OpenSSL development files. Windows currently uses the GNU/MinGW Rust target
and a matching libcurl package; set `LICENTIVO_CURL_ROOT` to its directory and add
its `bin` directory to the application process's PATH. MSVC and physical macOS
acceptance are pending. The build compiles the bundled licensing core; applications
do not need to implement HTTP, signatures, device identity, encryption or timers.

Install the pinned release from crates.io in your existing Cargo project:

```bash
cargo add licentivo@=0.10.0
```

The [common website installer](https://www.licentivo.com/en/docs.html#sdk) is also available with `rust`; it verifies the downloaded package and adds a Cargo path dependency. Offline source installation is supported:

```toml
[dependencies]
licentivo = { path = "SDK_ROOT/sdk/rust" }
```

```rust
use licentivo::{Client, Error};

fn run(customer_key: &str) -> Result<(), Error> {
    let mut client = Client::open_with_license("product.json", customer_key)?;
    client.run_feature("export", || {
        std::fs::write("report.txt", "Your business result\n")?;
        Ok(())
    })?;
    Ok(()) // In a real app, retain client until shutdown.
}
```

`run_metered_feature("export", 1, job_id, callback)` journals reserve, business
execution and confirmation. A successful retry returns `Execution::AlreadyCompleted`
and skips the callback; load its result from your application's job record. If the
application crashes during the callback, determine the real business outcome and
call `resolve_metered_feature(job_id, succeeded)`. Use one distinct stable ID per
job and serialize calls on the client. Client is Send, so ownership can move to a
worker thread; it is not Sync. Policy heartbeats run in the core.

`status()` and `feature_status()` return local JSON decisions. `Error` exposes
`code`, `http_status`, `request_id`, `retryable` and `retry_after_seconds`. There
are explicit online refresh, deactivate, offline exchange and low-level metering
methods for advanced integrations. `close()` consumes the client; Drop also stops
heartbeats and releases floating seats. Pinned signatures and TLS remain enabled.

Do not ship test keys, `sdk-demo.json`, device credentials, caches or management API
keys. The SDK and vendored dependencies' licenses are included in the package.
