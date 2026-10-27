# Licentivo Java SDK

[Website](https://www.licentivo.com/) · [Documentation](https://www.licentivo.com/en/docs.html#sdk) · [Source code](https://github.com/spf86/licentivo-sdk)

Add license activation and real feature permissions to your own application.
The SDK handles requests, machine identity, pinned signatures, private credentials,
cache and policy heartbeats. Your app supplies activation input and business code.

## Install into your project

Requirements: JDK 17+; JDK HTTP and Ed25519 APIs.

Install the public SDK package in your existing application project:

```gradle
implementation 'com.licentivo:licentivo-sdk:0.11.4'
```

Add the line above to your Gradle dependencies and enable `mavenCentral()`
in your repositories. For Maven, add this dependency to `pom.xml`:

```xml
<dependency>
  <groupId>com.licentivo</groupId>
  <artifactId>licentivo-sdk</artifactId>
  <version>0.11.4</version>
</dependency>
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

```java
import licentivo.Client;
import java.nio.file.*;
import java.util.Map;

var trustedKeys = Map.of("YOUR_KEY_ID", "YOUR_PUBLIC_KEY");
try (var client = Client.openProduct("YOUR_PRODUCT_ID", trustedKeys, customerKey,
        error -> System.err.println(error.getMessage()))) {
    client.runFeature("export", () -> Files.writeString(Path.of("report.csv"), "Your report\n"));
    // Real application: retain client until application shutdown.
}
```

Place the feature gate in the actual button/shortcut/command business handler.
Denied operations do not invoke the callback. After first activation, pass an
empty key to restore the saved credential for this product and user. Ordinary
close retains device registration; `deactivate` is for explicit device release.
Floating close attempts to return its seat, with server expiry as fallback.
Use `refreshOnline` to force an online check. Offline behavior follows the signed
policy and expiry; a confirmed server denial blocks protected features.

## Metering and advanced operations

Use `runMeteredFeature` for a configured usage counter, passing a distinct stable job ID,
quantity and your business callback. The SDK reserves, executes, commits/cancels
and records pending settlement. A known successful job retry settles without
rerunning business. Persist the actual business result; after a crash during a
callback, inspect its outcome and pass the truth to `resolveMeteredFeature`. The SDK does not
guess or replay an uncertain business action. Metering requires a network.

The lower-level reserve/commit/cancel and immediate-consume methods remain
available. Offline request/response files are a separate disconnected-computer
workflow. For private deployments, proxy settings or app versions, construct the
language's SDK configuration object directly in code and use its lower-level
constructor/start API. TLS and public-key pinning remain enabled.

Java methods are synchronous; run network work off the Swing event thread. Client implements AutoCloseable. Online errors can be Client.APIException; local, signature and business errors can be other exceptions.

Close with `close` at application shutdown. Do not distribute test activation
keys, machine credentials, cached grants or vendor management API keys.
See [QUICKSTART.md](https://www.licentivo.com/en/docs.html#quickstart) and the
[small desktop example](https://www.licentivo.com/en/docs.html#example).
MIT licensed; bundled dependencies retain their licenses (NOTICE).
