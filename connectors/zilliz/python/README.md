# Zilliz Cloud Python connector

`dex-connector-zilliz` exposes the `listClusters` Query Step through `dex_connectors.zilliz`. Configure the static `zilliz` connection using the [connector guide](../README.md).

The [runnable listClusters Flow](examples/list_clusters/) has a Worker entrypoint and real Dex integration test. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/zilliz-python-fdg --python .venv/bin/python examples/list_clusters/flow.py
```
