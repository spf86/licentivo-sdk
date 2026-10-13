# Licentivo Ruby SDK

Add software licensing to a Ruby application with a block-based interface and a bundled native licensing core.

## Features

- Activates or restores a license with a private machine-scoped device credential.
- Verifies pinned signatures, checks expiry and feature access, and protects your business block.
- Runs policy heartbeats, supports permitted offline use and floating-seat cleanup.
- Handles metered-feature reserve/commit/cancel and journals pending settlement across restarts.
- Supports refresh, explicit device deactivation, offline file activation and structured error diagnostics.

Your application provides the activation interface, the actual business block and durable business results. The SDK manages licensing; customer credentials and vendor management keys do not belong in distributed packages.

## Installation and runtime requirements

Start with the repository's `QUICKSTART.md`. The application in
`examples/quickstart.rb` activates/restores a device and actually exports a file.
It also supports `LICENTIVO_METERED=1` with a stable `LICENTIVO_OPERATION_ID`.

Prerequisites: Ruby 3.1+ with Fiddle, a C11 compiler and libcurl development files.
Linux also needs OpenSSL development files. Installing a source gem requires make
(RubyInstaller Devkit/MSYS2 on Windows). A matching precompiled platform gem
includes the core and installs without a C compiler; Windows also includes the
matching libcurl DLL and its license. The platform gem still requires Fiddle.
The source launcher builds the bundled
core automatically. For a source gem, RubyGems builds it during installation.
Windows uses MinGW and a matching libcurl package; set `LICENTIVO_CURL_ROOT` and add
its `bin` directory to the process's PATH. Physical macOS acceptance is pending.

Install the pinned RubyGems release, or add it to your application's Gemfile:

```bash
gem install licentivo-sdk --version 0.10.0
```

```ruby
gem 'licentivo-sdk', '= 0.10.0'
```

The [common website installer](https://www.licentivo.com/en/docs.html#sdk) with `ruby` also selects and installs a verified matching platform gem. It prints the per-terminal GEM_PATH command; platform gems avoid a local C build. A complete local source bundle can also be used:

```ruby
# Gemfile
gem 'licentivo-sdk', path: 'SDK_ROOT/sdk/ruby'
```

For a path gem, run `ruby SDK_ROOT/sdk/ruby/build_native.rb` before starting your
application. A built `.gem` runs this compilation automatically during `gem install`.
The SDK does not download executable code when you require it.

```ruby
require 'licentivo'

Licentivo::Client.open_with_license('product.json', customer_key) do |client|
  client.run_feature('export') { File.write('report.txt', "Your result\n") }
  # A real application keeps client alive through its main loop.
end
```

`run_metered_feature('export', 1, job_id) { your_operation }` handles reserve,
business execution, commit and cancellation. It returns an `Execution` with
`executed` and `value`; `replay?` means the business block was skipped and you
should load its existing result using the job ID. Exceptions are re-raised after
the core handles cancellation. If the process dies during the block, check the
real business outcome and call `resolve_metered_feature(job_id, true_or_false)`.

Use one distinct stable ID per job; separate processes must not execute the same
ID. Do not close or start another workflow from inside a business block. Status
reads are available there. The client serializes native access; policy heartbeats
run inside the core without entering Ruby. Poll `status` or `feature_status` for
your UI and marshal updates to its thread.

`Licentivo::Error` exposes `code`, `http_status`, `request_id`, `retryable` and
`retry_after_seconds`. Online refresh, explicit deactivate, offline exchange and
low-level metering methods are also provided. Use `close` or the owning block's
ensure cleanup on shutdown; normal exits retain node-locked device registration.

Do not ship test keys, `sdk-demo.json`, credentials, caches or management API keys.
TLS and pinned signatures stay enabled. Licenses accompany the vendored core.
