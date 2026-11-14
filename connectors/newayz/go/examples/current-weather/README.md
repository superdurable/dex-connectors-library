# WAYZ current weather Flow

This runnable Flow uses the static `newayz` connection to read current weather for one city code. The `fetched` branch stores the result in a Dex Attribute and completes with it. Optional failure branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/newayz/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/current-weather/flow/workflow.go --schema-version 2.0 --json --out ./build/current-weather
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact and pass it to `dexcli dev --connector-release-override newayz=<artifact-directory>`. In Dex Web **Connections**, create the `newayz` connection. Register at [LoTBoard](https://lotboard.newayz.com/), apply for an application with Weather API access, wait for approval, and copy its AccessKey into the secret `access_key` field. Leave timeout and response size blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/current-weather
```

In Dex Web **Start Flow**, choose `NewayzCurrentWeather` and enter a unique Flow ID:

```json
{"cityCode":"420100"}
```

The output includes `city`, `weather`, `temperatureCelsius`, and `humidityPercent`. The mock-backed integration test uses a real Dex Worker and Client with synthetic sample data and no provider credential.
