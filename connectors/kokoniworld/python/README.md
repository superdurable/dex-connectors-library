# Kokoni World Python connector

`dex-connector-kokoniworld` exposes the `listModels` Query Step at `dex_connectors.kokoniworld`. It filters provider data to the Go-defined output model and never persists credentials or raw responses. Configure the static `kokoniworld` connection using the [connector guide](../README.md).

The [runnable listModels Flow](examples/list_models/) includes a real Dex Worker and Client integration test with a fake provider. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/kokoniworld-python-fdg --python .venv/bin/python examples/list_models/flow.py
```
