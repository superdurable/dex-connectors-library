# Huoban Python connector

`dex-connector-huoban` exposes the `listSpaces` Query Step at `dex_connectors.huoban`. It returns authorized workspace IDs and names without cover images, member data, or the API key. Configure the secret API key in Dex Web Connections using the [connector guide](../README.md).

The [runnable workspace list Flow](examples/space_list/) uses the static `huoban` connection. Its integration test runs a real Dex Worker and Client against a fake HTTP provider, including a 503 retry and persisted result. Live Huoban authorization and workspace listing remain unverified.

## Strict FDG 2.0 generation

From this package directory, run:

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/huoban-python-fdg --python .venv/bin/python examples/space_list/flow.py
```
