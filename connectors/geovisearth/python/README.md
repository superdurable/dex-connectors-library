# geovisearth Python connector

The `dex-connector-geovisearth` package implements the read-only `GetCurrentWeather` Query. Read current weather for one Chinese area code or coordinate. Its [runnable Flow](examples/current_weather/flow.py) uses the static `geovisearth` connection and persists the public result. The [real Dex integration test](examples/current_weather/integration_test.py) verifies retry and persistence. The [conformance test](tests/test_conformance.py) replays Go-recorded provider requests and outcomes.

## Run with Dex Web

Create a `geovisearth` connection using the authorization guide in [connector.yaml](../connector.yaml). With `DEX_PROJECT_*` set, run:

```bash
uv run python -m examples.current_weather.worker
```

Start `GEOVISEarthCurrentWeatherFlow` with a unique Flow ID and input `location: WTX_CH101300201`. The example needs an authorized provider account for live requests.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/geovisearth-python-fdg --python .venv/bin/python examples/current_weather/flow.py
```

Live provider authorization and responses remain unverified.
