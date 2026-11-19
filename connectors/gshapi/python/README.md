# GSH API Python connector

`dex-connector-gshapi` exposes the `listModels` Query Step at `dex_connectors.gshapi`. See the [GSH API developer center](https://api.gshpay.com/docs.html) for the documented `GET /v1/models` endpoint and API Key setup.

The [runnable Flow](examples/model_list/) uses the static `gshapi` connection and stores only model IDs. Its integration test exercises a real Dex Worker/Client with a fake provider. Live GSH API authorization and model availability remain unverified.

## Strict FDG 2.0 generation

From this package directory, run:

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/gshapi-python-fdg --python .venv/bin/python examples/model_list/flow.py
```
