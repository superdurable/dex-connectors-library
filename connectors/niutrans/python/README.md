# Niutrans Python connector

`dex-connector-niutrans` exposes the `translateText` Query Step through `dex_connectors.niutrans`. Configure the static `niutrans` connection using the [connector guide](../README.md).

The [runnable translateText Flow](examples/translate_text/) has a Worker entrypoint and real Dex integration test. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/niutrans-python-fdg --python .venv/bin/python examples/translate_text/flow.py
```
