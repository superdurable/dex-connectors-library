# Ucloud Python connector

`dex-connector-ucloud` exposes the `getRegion` Query Step through `dex_connectors.ucloud`. Configure the static `ucloud` connection using the [connector guide](../README.md).

The [runnable getRegion Flow](examples/get_region/) has a Worker entrypoint and real Dex integration test. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/ucloud-python-fdg --python .venv/bin/python examples/get_region/flow.py
```
