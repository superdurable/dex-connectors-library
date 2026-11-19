# 365 API Python connector

`dex-connector-365api` exposes the `listModels` Query Step at `dex_connectors._365api`. See the [365 API developer center](https://www.hby365.studio/developers) for the documented `GET /v1/models` endpoint and API Key setup.

The [runnable Flow](examples/model_list/) uses the static `hby365` connection and stores only model IDs. Its integration test exercises a real Dex Worker/Client with a fake provider. Live 365 API authorization and model availability remain unverified.

## Strict FDG 2.0 generation

From this package directory, run:

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/365api-python-fdg --python .venv/bin/python examples/model_list/flow.py
```
