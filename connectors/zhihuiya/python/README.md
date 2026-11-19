# Zhihuiya Patent Valuation Python connector

`dex-connector-zhihuiya` exposes the `getPatentValuation` Query Step through `dex_connectors.zhihuiya`. Configure the static `zhihuiya` connection using the [connector guide](../README.md).

The [runnable getPatentValuation Flow](examples/get_patent_valuation/) has a Worker entrypoint and real Dex integration test. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/zhihuiya-python-fdg --python .venv/bin/python examples/get_patent_valuation/flow.py
```
