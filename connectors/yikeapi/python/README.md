# Yikeapi Python connector

`dex-connector-yikeapi` exposes the `lookupIPLocation` Query Step through `dex_connectors.yikeapi`. Configure the static `yikeapi` connection using the [connector guide](../README.md).

The [runnable lookupIPLocation Flow](examples/lookup_ip_location/) has a Worker entrypoint and real Dex integration test. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/yikeapi-python-fdg --python .venv/bin/python examples/lookup_ip_location/flow.py
```
