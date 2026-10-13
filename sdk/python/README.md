# Licentivo Python SDK

Add software license activation and protected features to a Python application without implementing the licensing HTTP protocol yourself.

## Features

- Activates a customer license, derives device identity, stores a private device credential and restores it after restart.
- Verifies signed grants with pinned public keys and checks license/feature status before running your business callback.
- Schedules policy heartbeats, supports permitted offline grants and returns floating seats on close.
- Handles metered-feature reservation, commit/cancel and recovery of pending confirmations using a stable job ID.
- Provides explicit refresh/deactivation, offline activation file exchange and structured error details.

Your application supplies the activation interface, business function and durable business results. The SDK handles the authorization workflow; management API keys stay on your server.

## Installation

Use Python 3.10+ and a virtual environment. Install the pinned release from PyPI:

```bash
python -m pip install "licentivo-sdk==0.10.0"
```

For an offline release, install local sources with `python -m pip install ./sdk/python`,
or a downloaded wheel with `python -m pip install ./licentivo_sdk-0.10.0-py3-none-any.whl`.
The [website installation guide](https://www.licentivo.com/en/docs.html#sdk) shows current release availability and the common installer.
For the full starter, see QUICKSTART.md at the configured ZIP root and run `start.ps1` / `start.sh`.
Keep `product.json` in application resources. Read the customer's key from your
activation UI; after activation pass an empty string to restore the credential.

```python
from licentivo import Client
from pathlib import Path

with Client.open_with_license('product.json', customer_key, on_error=print) as client:
    client.run_feature('export', lambda: Path('report.txt').write_text('Report\n'))
    # In your app, keep this context open until the application exits.
```

For a configured metered feature, pass your business callback to the SDK:

```python
client.run_metered_feature('export', 1, persistent_job_id, export_report)
```

The SDK reserves, runs, confirms/cancels and stores a private journal. Retain your
job ID and durable business result. Repeating this call after a pending commit,
including after restart, retries only confirmation and never repeats business.
If a process crashes during the callback, inspect its result and call
`resolve_metered_feature(job_id, succeeded)`; the SDK does not guess or rerun it.
One workflow runs per client; different processes need different job IDs.
The lower-level reserve/commit/cancel methods remain available for custom flows.
The counter requires an online connection. `consume()` immediately charges
usage and is for operations that cannot subsequently fail.

`offline_request()` returns a dictionary to save as JSON;
`import_offline(request_dict, response_dict)` verifies an offline activation.
Closing a floating client releases its seat; device bindings are kept.

Accept an ordinary `lv_lic_...` key or a replacement computer's `lv_tmp_...`
code through your activation UI. After activation, the SDK saves a machine-scoped
credential beside `cache_path`. Later starts can use an empty `license_key`
with the same product, trusted keys and cache path. Keep the credential in the
user's application data directory; never ship it with the software.

With a valid device-bound offline cache, startup restores access immediately
and refreshes online in the background. Online-only and floating licenses wait
for the server. `feature_status('export')` explains a denial without a network
request. Call `close()` on exit. Python's synchronous HTTP transport may finish
an in-flight request after close; its timeout bounds that work.

Run `python desktop.py product.json` for an activation and report-export window.
It retains an export journal for commit recovery. An interrupted output still
requires your application to check whether the business operation completed.
