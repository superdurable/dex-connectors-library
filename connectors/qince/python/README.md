# Qince Python connector

`dex-connector-qince` exposes the `listDepartments` Query Step through `dex_connectors.qince`. Configure the static `qince` connection using the [connector guide](../README.md).

The [runnable listDepartments Flow](examples/list_departments/) has a Worker entrypoint and real Dex integration test. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/qince-python-fdg --python .venv/bin/python examples/list_departments/flow.py
```
