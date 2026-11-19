# VariFlight Python connector

`dex-connector-variflight` exposes the `getFlightStatus` Query Step at `dex_connectors.variflight`. See the [connector manifest](../connector.yaml) for provider authorization, configuration, and branch behavior.

The [runnable Flow](examples/flight_status/) uses the static `variflight` connection and stores only non-secret get flight status results. Its integration test runs a real Dex Worker/Client with a fake provider. Live provider authorization and API behavior remain unverified.

## Strict FDG 2.0 generation

From this package directory, run:

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/variflight-python-fdg --python .venv/bin/python examples/flight_status/flow.py
```
