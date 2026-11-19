# Vika Python connector

`dex-connector-vika` exposes the `listSpaces` Query Step at `dex_connectors.vika`. See the [connector manifest](../connector.yaml) for provider authorization, configuration, and branch behavior.

The [runnable Flow](examples/space_list/) uses the static `vika` connection and stores only non-secret list spaces results. Its integration test runs a real Dex Worker/Client with a fake provider. Live provider authorization and API behavior remain unverified.

## Strict FDG 2.0 generation

From this package directory, run:

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/vika-python-fdg --python .venv/bin/python examples/space_list/flow.py
```
