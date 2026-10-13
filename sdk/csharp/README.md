# Licentivo .NET SDK

Add software licensing to a .NET 8+ console, desktop or service application. The SDK handles the authorization protocol while your application provides its activation screen and protected business functions.

## Features

- Activate customer licenses, derive device identity, save private device credentials and restore authorization after restart.
- Verify signed grants against pinned public keys and check license expiry and feature permissions.
- Schedule policy heartbeats, use verified offline grants where the policy allows, and attempt floating-seat release on disposal.
- Reserve usage, run your callback, commit success or cancel failure, and journal pending confirmations across restarts.
- Read local status and feature decisions; refresh, deactivate or exchange offline activation files explicitly.
- Report structured errors with request IDs for online diagnostics.

Metered operations require a network connection. Retain a stable job ID and a durable business result; after a crash during the callback, inspect that result before resolving settlement. Retrying a pending commit does not rerun the business function.

## Installation

In the directory containing your application's `.csproj`:

```bash
dotnet add package Licentivo.SDK --version 0.10.0
```

NuGet restores the declared `BouncyCastle.Cryptography` dependency. For an offline SDK download or a project with private feeds, use the [website installer and local-source instructions](https://www.licentivo.com/en/docs.html#sdk).

## First integration

Download `product.json` from **Quick integration** in your Licentivo workspace. It contains the product ID, HTTPS address and pinned public keys. Read the customer's license key from your activation screen; subsequent launches pass an empty string to restore the saved device credential.

```csharp
using Licentivo;

await using var client = await LicentivoClient.OpenWithLicense(
    "product.json", customerKey, error => Console.Error.WriteLine(error.Message));

await client.RunFeature("export", () =>
    File.WriteAllTextAsync("report.txt", "Your report\n"));
// Keep client alive until your actual application exits.
```

For configured metered features:

```csharp
await client.RunMeteredFeature("export", 1, persistentJobId, ExportReport);
```

The SDK handles reserve, commit/cancel and the pending journal. The application supplies `ExportReport`, the persistent job ID and its actual business result. Explicitly deactivate only for a customer-requested device release. Ordinary disposal retains a device-bound registration and attempts to return a floating seat.

[Complete examples for all nine languages](https://github.com/spf86/licentivo-sdk/blob/main/sdk/README.md) · [Quick integration and runtime documentation](https://www.licentivo.com/en/docs.html#quickstart)

Do not distribute customer keys, device credentials, cached grants or vendor management API keys. Client code is MIT licensed; bundled dependencies retain their respective licenses.
