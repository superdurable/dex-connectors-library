# Infini AI Python connector

`dex-connector-infiniai` exposes the `listModels` Query Step at `dex_connectors.infiniai`. It reads model IDs, types, and token limits from GenStudio without reflecting the API key or unrelated provider fields. Configure the `sk-` prefixed API key in Dex Web Connections using the [connector guide](../README.md).

The [runnable model list Flow](examples/model_list/) uses the static `infiniai` connection. Its integration test runs a real Dex Worker and Client against a fake provider, including a 503 retry and persisted result. Live GenStudio authorization and model listing remain unverified.

## Strict FDG 2.0 generation

From this package directory, run:

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/infiniai-python-fdg --python .venv/bin/python examples/model_list/flow.py
```
