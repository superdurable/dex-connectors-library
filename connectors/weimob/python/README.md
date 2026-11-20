# Weimob Python connector

`dex-connector-weimob` exposes the `getStoreBasic` Query Step. Configure the static `weimob` connection with the [provider guide](../README.md).

The [runnable getStoreBasic Flow](examples/get_store_basic/) includes a Worker entrypoint and real Dex retry integration test with synthetic provider responses. Live Weimob authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/weimob-python-fdg --python .venv/bin/python examples/get_store_basic/flow.py
```
