# ZoomEye Python connector

`dex-connector-knownsec-zoomeye` exposes the `getAccountQuota` Query Step at `dex_connectors.knownsec_zoomeye`. It filters provider data to the Go-defined output model and never persists credentials or raw responses. Configure the static `zoomeye` connection using the [connector guide](../README.md).

The [runnable getAccountQuota Flow](examples/account_quota/) includes a real Dex Worker and Client integration test with a fake provider. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/knownsec_zoomeye-python-fdg --python .venv/bin/python examples/account_quota/flow.py
```
