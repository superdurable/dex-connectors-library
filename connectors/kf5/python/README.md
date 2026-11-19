# KF5 Python connector

`dex-connector-kf5` exposes the `getTicket` Query Step at `dex_connectors.kf5`. It filters provider data to the Go-defined output model and never persists credentials or raw responses. Configure the static `kf5` connection using the [connector guide](../README.md).

The [runnable getTicket Flow](examples/get_ticket/) includes a real Dex Worker and Client integration test with a fake provider. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/kf5-python-fdg --python .venv/bin/python examples/get_ticket/flow.py
```
