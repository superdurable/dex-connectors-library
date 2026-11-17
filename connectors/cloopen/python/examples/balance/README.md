# Cloopen main-account balance Flow

This runnable Python Flow uses the static `cloopen` connection to read the
main-account balance. Its `fetched` branch records Cloopen's exact decimal
string in a Dex Attribute and completes. Optional `providerRejected`,
`invalidResponse`, and `defect` branches are unwired, so the Flow fails when
one is selected.

In Dex Web **Connections**, create a `cloopen` connection. Open the
[Cloopen management console](https://console.yuntongxun.com) and its account
information page. Copy the 32-character main-account **ACCOUNT SID** and
**AUTH TOKEN** into the secret authorization fields. Leave `baseUrl`,
`timeout`, and `maxResponseBytes` blank for their displayed defaults unless an
approved proxy or measured response size requires an override. The API is a
read-only [AccountInfo GET](https://doc.yuntongxun.com/p/5a533b4b3b8496dd00dce034).
The Flow has no operation-specific configuration fields.

Set `DEX_FLOW_SERVICE_ADDRESS` and the Dex project configuration file for
local development. From `connectors/cloopen/python`, run:

```bash
uv sync --locked
uv run python -m examples.balance.worker
```

Start one lookup with a unique Flow ID:

```bash
uv run python -m examples.balance.start cloopen-balance-001
```

The Flow input is `{}`. Generate strict FDG 2.0 with:

```bash
dexcli visualize examples/balance/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/cloopen-balance-python-fdg --python .venv/bin/python
```

The integration test uses inert credentials and a fake provider against a real
Dex Worker and Client. It checks the UTC signature, 503 Retry, and safe
completion. No live Cloopen account has been authorized.
