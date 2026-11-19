# Juhe Python connector

`dex-connector-juhe` exposes the `getCurrentWeather` Query Step at `dex_connectors.juhe`. It filters provider data to the Go-defined output model and never persists credentials or raw responses. Configure the static `juhe` connection using the [connector guide](../README.md).

The [runnable getCurrentWeather Flow](examples/current_weather/) includes a real Dex Worker and Client integration test with a fake provider. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/juhe-python-fdg --python .venv/bin/python examples/current_weather/flow.py
```
