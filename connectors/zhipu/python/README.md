# Zhipu Python connector

`dex-connector-zhipu` exposes the `generateText` Query Step through `dex_connectors.zhipu`. Configure the static `zhipu` connection using the [connector guide](../README.md).

The [runnable generateText Flow](examples/generate_text/) has a Worker entrypoint and real Dex integration test. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/zhipu-python-fdg --python .venv/bin/python examples/generate_text/flow.py
```
