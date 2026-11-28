# Moxin Python connector

`dex-connector-moxin` exposes the `listModels` Query Step through `dex_connectors.moxin`. Configure the static `moxin` connection using the [connector guide](../README.md).

The [runnable listModels Flow](examples/list_models/) has a Worker entrypoint and real Dex integration test. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/moxin-python-fdg --python .venv/bin/python examples/list_models/flow.py
```

On HTTP 400 or 404, the model-list query selects `providerRejected` with a
validation failure. The response-size limit also applies to definitive rejections.
