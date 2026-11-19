# vivo Python connector

`dex-connector-vivo` exposes the `getOnlineVersion` Query Step at `dex_connectors.vivo`. See the [connector manifest](../connector.yaml) for provider authorization, configuration, and branch behavior.

The [runnable Flow](examples/get_online_version/) uses the static `vivo` connection and stores only non-secret get online version results. Its integration test runs a real Dex Worker/Client with a fake provider. Live provider authorization and API behavior remain unverified.

## Strict FDG 2.0 generation

From this package directory, run:

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/vivo-python-fdg --python .venv/bin/python examples/get_online_version/flow.py
```
