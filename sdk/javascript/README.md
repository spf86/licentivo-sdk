# Licentivo Node.js SDK

Add software licensing to a Node.js or desktop application's main process. Use Node.js 20+; browser JavaScript cannot read the machine identity required by this SDK.

## Features

- Activates customer licenses, derives device identity, saves a private device credential and restores authorization after restart.
- Verifies signed grants against pinned keys and checks expiry and feature access before invoking your callback.
- Schedules policy heartbeats, supports permitted offline use and attempts floating-seat cleanup on close.
- Wraps metered operations with reserve, business execution, commit/cancel and a private pending-task journal.
- Provides local status decisions, explicit refresh/deactivation, offline activation file exchange and structured request errors.

The application supplies its activation screen, protected business function and durable business results. Keep vendor management keys on your backend.

## Installation

In your Node.js 20+ application project, install the pinned public package:

```bash
npm install @licentivo/sdk@0.10.0
```

Keep your package lockfile. The [installation guide](https://www.licentivo.com/en/docs.html#sdk) also provides the common installer and platform requirements. For an offline project, download and install the versioned archive:

```bash
npm install ./licentivo-sdk-0.10.0.tgz
```

Download that package from the [versioned release](https://www.licentivo.com/assets/sdk/releases/v0.10.0/licentivo-sdk-0.10.0.tgz), or install local sources with `npm install ./sdk/javascript`. Import `@licentivo/sdk` after either method. QUICKSTART.md at the configured ZIP root contains a runnable starter.

## First integration

Download a configured `sdk-demo.json` from the dashboard's Quick integration page.
That file contains a license key, product ID and trusted public keys, never a
vendor management API key.

```js
import {Client} from '@licentivo/sdk';
import {writeFile} from 'node:fs/promises';
const client = await Client.openWithLicense('product.json', customerKey, console.error);
try {
  await client.runFeature('export', () => writeFile('report.txt', 'Report\n'));
  // Keep client alive until your actual application exits.
} finally {
  await client.close();
}
```

For configured metered features, pass your business callback to the SDK:

```js
await client.runMeteredFeature('export', 1, persistentJobID, exportReport);
```

The SDK handles reserve, execution, commit/cancel and a private task journal.
Replaying a pending commit, including after restart, retries only confirmation.
If the process crashes during the callback, inspect your durable result before
`await client.resolveMeteredFeature(jobID, succeeded)`; it never reruns business.
Use different job IDs across processes and retain records while retries remain
possible. Lower-level reserve/commit/cancel remain for custom workflows.
`consume()` charges immediately and is for an
operation that cannot subsequently fail. Counters require a network connection.
Export/import
offline activation files with `offlineRequest()` and `importOffline()`.
Closing a floating client releases its seat; closing a device-bound client
stops heartbeats and preserves the installation binding.

Accept an ordinary `lv_lic_...` key or a replacement computer's `lv_tmp_...`
code through your activation UI. After activation, the SDK saves a machine-scoped
credential beside `cache_path`. Later starts can use an empty `license_key`
with the same product, trusted keys and cache path. Keep the credential in the
user's application data directory; never ship it with the software.

With a valid device-bound offline cache, startup restores access immediately
and refreshes online in the background. Online-only and floating licenses wait
for the server. `featureStatus('export')` explains a denial without contacting
the server. Run `node app.mjs product.json` for an interactive application example.
Its unfinished commit is held in memory; add durable job recovery in your app.

Direct connections use built-in fetch. For `proxy_url`, install the optional
`undici` dependency with a version compatible with your Node.js runtime.
