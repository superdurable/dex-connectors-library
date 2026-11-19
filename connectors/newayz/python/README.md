# Newayz Python connector

`dex-connector-newayz` exposes the `getCurrentWeather` Query Step through `dex_connectors.newayz`. Configure the static `newayz` connection using the [connector guide](../README.md).

The [runnable getCurrentWeather Flow](examples/get_current_weather/) has a Worker entrypoint and real Dex integration test. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/newayz-python-fdg --python .venv/bin/python examples/get_current_weather/flow.py
```
