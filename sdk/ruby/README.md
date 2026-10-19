# Licentivo Ruby SDK

Add license activation and real feature permissions to your own application.
The SDK handles requests, machine identity, pinned signatures, private credentials,
cache and policy heartbeats. Your app supplies activation input and business code.

## Install into your project

Requirements: Ruby 3.1+, Fiddle, C11 compiler and libcurl development files. Source gems also need make.

Install the public SDK package in your existing application project:

```sh
gem install licentivo-sdk --version 0.11.0
```

For Bundler, use `gem "licentivo-sdk", "= 0.11.0"` in your Gemfile.

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

```ruby
require 'licentivo'

trusted_keys = {'YOUR_KEY_ID' => 'YOUR_PUBLIC_KEY'}
Licentivo::Client.open_product('YOUR_PRODUCT_ID', trusted_keys, customer_key) do |client|
  client.run_feature('export') { File.write('report.csv', "Your report\n") }
  # Real application: keep the owning block alive until shutdown.
end
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

The binding uses the shared C core through Fiddle. Calls are synchronous. open_product returns an owning client when no block is provided; with a block it closes in ensure. No Ruby callback is registered on the native heartbeat thread. Poll local status for UI updates. A metered Execution has value, executed and replay?.

Close with `close` at application shutdown. Do not distribute test activation
keys, machine credentials, cached grants or vendor management API keys.
See [QUICKSTART.md](https://www.licentivo.com/en/docs.html#quickstart) and the
[small desktop example](https://www.licentivo.com/en/docs.html#example).
MIT licensed; bundled dependencies retain their licenses (NOTICE).

For advanced in-code Hash settings, use `Client.open_config(config) { |client| ... }`. It starts and closes with the block, without a file.
