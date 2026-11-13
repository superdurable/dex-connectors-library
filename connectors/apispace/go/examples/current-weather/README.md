# APISpace current weather Flow

This runnable Flow uses the static `apispace` connection to read current weather for one city ID. The `found` branch stores the typed observation in a Dex Attribute and completes. Optional error branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/apispace/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/current-weather/flow/workflow.go --schema-version 2.0 --json --out ./build/current-weather
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override apispace=<artifact-directory>`. In Dex Web **Connections**, create the `apispace` connection. Subscribe to [APISpace Weather Forecast Query](https://www.apispace.com/eolink/api/456456/apiDocument), then open Console > Access Control and enter the API key in protected `api_token`. Leave base URL, timeout, and response size blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/current-weather
```

In Dex Web **Start Flow**, choose `APISpaceCurrentWeather`, enter a unique Flow ID, and use a city ID from the product's Domestic Cities attachment:

```json
{"areaCode":"101010100"}
```

The mock-backed integration test uses a real Dex Worker and Client with synthetic Beijing weather and no provider credential.
