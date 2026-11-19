# iP138 Python connector

`dex-connector-ip138` exposes the `getTokenUsage` Query Step at `dex_connectors.ip138`. It reads current-hour requests, hourly allowance, and purchased package quota without reflecting the token or raw provider message. Configure the secret token in Dex Web Connections using the [connector guide](../README.md).

The [runnable token usage Flow](examples/token_usage/) uses the static `ip138` connection. Its integration test runs a real Dex Worker and Client against a fake HTTP provider, including a 503 retry and persisted result. Live iP138 authorization and token quota remain unverified.

## Strict FDG 2.0 generation

From this package directory, run:

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/ip138-python-fdg --python .venv/bin/python examples/token_usage/flow.py
```
