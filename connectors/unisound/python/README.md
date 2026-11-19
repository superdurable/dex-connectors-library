# Unisound Python connector

`dex-connector-unisound` exposes the `listSystemVoices` Query Step at `dex_connectors.unisound`. See the [connector manifest](../connector.yaml) for provider authorization, configuration, and branch behavior.

The [runnable Flow](examples/list_system_voices/) uses the static `unisound` connection and stores only non-secret list system voices results. Its integration test runs a real Dex Worker/Client with a fake provider. Live provider authorization and API behavior remain unverified.

## Strict FDG 2.0 generation

From this package directory, run:

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/unisound-python-fdg --python .venv/bin/python examples/list_system_voices/flow.py
```
