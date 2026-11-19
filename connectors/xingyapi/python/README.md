# Xingyapi Python connector

`dex-connector-xingyapi` exposes the `getInstanceOnlineStatus` Query Step through `dex_connectors.xingyapi`. Configure the static `xingyapi` connection using the [connector guide](../README.md).

The [runnable getInstanceOnlineStatus Flow](examples/get_instance_online_status/) has a Worker entrypoint and real Dex integration test. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/xingyapi-python-fdg --python .venv/bin/python examples/get_instance_online_status/flow.py
```
