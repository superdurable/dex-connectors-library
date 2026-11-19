# TextIn Python connector

`dex-connector-textin` exposes the `getAccountBalance` Query Step at `dex_connectors.textin`. It filters provider data to the Go-defined output model and never persists credentials or raw responses. Configure the static `textin` connection using the [connector guide](../README.md).

The [runnable getAccountBalance Flow](examples/get_account_balance/) includes a real Dex Worker and Client integration test with a fake provider. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/textin-python-fdg --python .venv/bin/python examples/get_account_balance/flow.py
```
