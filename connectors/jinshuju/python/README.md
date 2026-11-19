# Jinshuju Python connector

`dex-connector-jinshuju` exposes the `listFormsPage` Query Step at `dex_connectors.jinshuju`. It filters provider data to the Go-defined output model and never persists credentials or raw responses. Configure the static `jinshuju` connection using the [connector guide](../README.md).

The [runnable listFormsPage Flow](examples/forms_page/) includes a real Dex Worker and Client integration test with a fake provider. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/jinshuju-python-fdg --python .venv/bin/python examples/forms_page/flow.py
```
