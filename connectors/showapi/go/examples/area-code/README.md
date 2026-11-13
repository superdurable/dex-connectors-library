# ShowAPI weather region-code Flow

This runnable Flow uses the static `showapi` connection to resolve a place name to one or more weather area codes. The `found` branch stores the typed response in a Dex Attribute and completes. Optional error branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/showapi/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/area-code/flow/workflow.go --schema-version 2.0 --json --out ./build/area-code
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override showapi=<artifact-directory>`. In Dex Web **Connections**, create the `showapi` connection. Sign in at [ShowAPI](https://www.showapi.com), open **Console → My Apps → AppKey Management**, and enter the AppKey in the secret `app_key` field. Order the Weather Forecast API product first. Leave the base URL, timeout, and response size blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/area-code
```

In Dex Web **Start Flow**, choose `ShowAPIAreaCodes`, enter a unique Flow ID, and use:

```json
{"area":"丽江"}
```

The result contains only matching province, place name, and area code. The mock-backed integration test uses a real Dex Worker and Client with synthetic matches and no provider credential.
