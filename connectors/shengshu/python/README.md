# Vidu task status Python connector

`dex-connector-shengshu` exposes the provider Query at `dex_connectors.shengshu`. The result contains task state, credits, and creation IDs without signed media URLs. Configure credentials in Dex Web Connections using the [connector guide](../README.md).

The [runnable Flow](examples/task_status/) uses the static `shengshu` connection. Its integration test runs a real Dex Worker and Client against a fake provider, including a 503 retry and persisted result. Live provider authorization and API behavior remain unverified.

## Strict FDG 2.0 generation

From this package directory, run:

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/shengshu-python-fdg --python .venv/bin/python examples/task_status/flow.py
```
