# Seniverse current weather Flow

This runnable Flow uses the static `seniverse` connection to read one city's current weather. The `fetched` branch stores the result in a Dex Attribute and completes with it. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/seniverse/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/current-weather/flow/workflow.go --schema-version 2.0 --json --out ./build/current-weather
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override seniverse=<artifact-directory>`. In Dex Web **Connections**, create the `seniverse` connection. From the [Seniverse console](https://www.seniverse.com/login), open 产品管理, select a Weather API product with 天气实况 access, then copy its 私钥 into the secret private_key field. Leave base URL, timeout, response size, language, and unit blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/current-weather
```

In Dex Web **Start Flow**, choose `SeniverseCurrentWeather` and enter a unique Flow ID:

```json
{"location":"北京"}
```

The output includes `weatherText`, `weatherCode`, `temperature`, `temperatureUnit`, and `lastUpdate`. The mock-backed integration test uses a real Dex Worker and Client with synthetic sample data and no provider credential.
