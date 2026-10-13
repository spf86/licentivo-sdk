# Licentivo Java SDK

Add software license activation and protected features to a Java 17+ application. The SDK uses the JDK HTTP client and cryptographic APIs; no customer-side management key is required.

## Features and workflow

- Activate a customer license, derive device identity, store a private device credential and restore it after restart.
- Verify grants with pinned signing keys and check expiry and feature access before invoking your business callback.
- Schedule policy heartbeats, use verified offline grants when permitted, and attempt to return floating seats on close.
- Reserve metered usage, execute the callback, commit success or cancel failure, and persist pending confirmations.
- Read local status, refresh or deactivate explicitly, and exchange offline activation files.

The application provides the activation screen, business function, stable operation ID and durable business result. If a process crashes during a metered callback, inspect its actual result before resolving settlement. A pending commit retries confirmation without rerunning the business function. Metering requires a network connection.

## Installation

Add this dependency to your application's `pom.xml` for Maven Central:

```xml
<dependency>
  <groupId>com.licentivo</groupId>
  <artifactId>licentivo-sdk</artifactId>
  <version>0.10.0</version>
</dependency>
```

Or configure Gradle:

```groovy
repositories { mavenCentral() }
dependencies { implementation 'com.licentivo:licentivo-sdk:0.10.0' }
```

The [live installation guide](https://www.licentivo.com/en/docs.html#sdk) confirms registry availability and provides the website/local JAR alternative. Keep version `0.10.0` pinned in your application.

## First integration

Download `product.json` from **Quick integration** in your Licentivo workspace. It contains the product UUID, HTTPS server and pinned public keys. Read the customer's key from your activation interface; pass an empty string on later starts to restore the saved device credential.

```java
import licentivo.Client;
import java.nio.file.Files;
import java.nio.file.Path;

try (var client = Client.openWithLicense("product.json", customerKey,
        error -> System.err.println(error.getMessage()))) {
    client.runFeature("export", () ->
        Files.writeString(Path.of("report.txt"), "Your report\n"));
    // Keep client alive until your actual application exits.
}
```

For a configured metered feature, call `client.runMeteredFeature("export", 1, persistentJobId, () -> exportReport())`. Closing a device-bound client stops heartbeats and retains the binding; explicitly deactivate for a customer-requested device release.

[Working examples](https://github.com/spf86/licentivo-sdk/tree/main/sdk/java/src/licentivo) · [All nine languages](https://www.licentivo.com/en/docs.html#sdk)

Do not distribute customer keys, cached grants, device credentials or vendor management API keys. Client code is MIT licensed.
