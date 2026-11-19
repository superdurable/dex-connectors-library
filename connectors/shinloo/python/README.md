# ShinLinkAI model IDs Python connector

`dex-connector-shinloo` exposes the provider Query at `dex_connectors.shinloo`. The result contains the provider model count and up to 50 IDs. Configure credentials in Dex Web Connections using the [connector guide](../README.md).

The [runnable Flow](examples/model_list/) uses the static `shinloo` connection. Its integration test runs a real Dex Worker and Client against a fake provider, including a 503 retry and persisted result. Live provider authorization and API behavior remain unverified.

## Strict FDG 2.0 generation

From this package directory, run:

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/shinloo-python-fdg --python .venv/bin/python examples/model_list/flow.py
```
