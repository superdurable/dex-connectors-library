# Tripo Python connector

`dex-connector-vast-tripo` exposes the `getAccountBalance` Query Step at `dex_connectors.vast_tripo`. See the [connector manifest](../connector.yaml) for provider authorization, configuration, and branch behavior.

The [runnable Flow](examples/account_balance/) uses the static `tripo` connection and stores only non-secret get account balance results. Its integration test runs a real Dex Worker/Client with a fake provider. Live provider authorization and API behavior remain unverified.

## Strict FDG 2.0 generation

From this package directory, run:

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/tripo-python-fdg --python .venv/bin/python examples/account_balance/flow.py
```
