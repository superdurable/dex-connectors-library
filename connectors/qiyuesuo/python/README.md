# Qiyuesuo Python connector

`dex-connector-qiyuesuo` exposes the `listContracts` Query Step through `dex_connectors.qiyuesuo`. Configure the static `qiyuesuo` connection using the [connector guide](../README.md).

The [runnable listContracts Flow](examples/list_contracts/) has a Worker entrypoint and real Dex integration test. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/qiyuesuo-python-fdg --python .venv/bin/python examples/list_contracts/flow.py
```
