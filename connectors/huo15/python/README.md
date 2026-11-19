# Huo15 Python connector

`dex-connector-huo15` exposes the `listModels` Query Step at `dex_connectors.huo15`. It returns up to 50 model IDs available to the Juxingyi API key, without exposing metadata or the key. Configure the secret `fsk-` prefixed key in Dex Web Connections using the [connector guide](../README.md).

The [runnable model list Flow](examples/model_list/) uses the static `huo15` connection. Its integration test runs a real Dex Worker and Client against a fake HTTP provider, including a 503 retry and persisted result. Live Juxingyi authorization and model availability remain unverified.

## Strict FDG 2.0 generation

From this package directory, run:

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/huo15-python-fdg --python .venv/bin/python examples/model_list/flow.py
```
