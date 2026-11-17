# Chuanglan 253 SMS balance Flow

This runnable Python Flow uses the static `chuanglan` connection to query the
remaining domestic SMS message count. Its `fetched` branch records the count in
a Dex Attribute and completes. Optional `providerRejected`, `invalidResponse`,
and `defect` branches are unwired, so the Flow fails when one is selected.

In Dex Web **Connections**, create a `chuanglan` connection. Open the
[Chuanglan SMS console](https://zz.253.com), then **产品概览 > API 接口信息**.
Copy the domestic SMS API account and API password into the two secret fields.
Use the account-assigned HTTPS node origin for `baseUrl` if it differs from the
displayed default; leave it blank otherwise. Leave `timeout` and
`maxResponseBytes` blank for their displayed defaults. If an IP whitelist is
enabled, add the Worker's outbound IP in the console.

Set `DEX_FLOW_SERVICE_ADDRESS` and the Dex project configuration file for
local development. From `connectors/chuanglan/python`, run:

```bash
uv sync --locked
uv run python -m examples.balance.worker
```

Start one lookup with a unique Flow ID:

```bash
uv run python -m examples.balance.start chuanglan-balance-001
```

The Flow input is `{}`. Generate strict FDG 2.0 with:

```bash
dexcli visualize examples/balance/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/chuanglan-balance-python-fdg --python .venv/bin/python
```

The integration test uses an inert account and a fake provider against a real
Dex Worker and Client. It verifies a 503 Retry followed by a successful count;
no live Chuanglan account has been authorized.
