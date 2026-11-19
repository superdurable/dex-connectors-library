# StepFun model records Python connector

`dex-connector-stepfun` exposes the provider Query at `dex_connectors.stepfun`. The result contains model IDs, creation times, and owners. Configure credentials in Dex Web Connections using the [connector guide](../README.md).

The [runnable Flow](examples/model_list/) uses the static `stepfun` connection. Its integration test runs a real Dex Worker and Client against a fake provider, including a 503 retry and persisted result. Live provider authorization and API behavior remain unverified.

## Strict FDG 2.0 generation

From this package directory, run:

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/stepfun-python-fdg --python .venv/bin/python examples/model_list/flow.py
```
