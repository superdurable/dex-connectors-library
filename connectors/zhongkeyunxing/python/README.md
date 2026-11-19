# Zhongke Yunxing Model Catalog Python connector

`dex-connector-zhongkeyunxing` exposes the `listModels` Query Step through `dex_connectors.zhongkeyunxing`. Configure the static `zhongkeyunxing` connection using the [connector guide](../README.md).

The [runnable listModels Flow](examples/list_models/) has a Worker entrypoint and real Dex integration test. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/zhongkeyunxing-python-fdg --python .venv/bin/python examples/list_models/flow.py
```
