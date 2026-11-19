# Qihoo360 Python connector

`dex-connector-qihoo360` exposes the `generateText` Query Step through `dex_connectors.qihoo360`. Configure the static `qihoo360` connection using the [connector guide](../README.md).

The [runnable generateText Flow](examples/generate_text/) has a Worker entrypoint and real Dex integration test. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/qihoo360-python-fdg --python .venv/bin/python examples/generate_text/flow.py
```
