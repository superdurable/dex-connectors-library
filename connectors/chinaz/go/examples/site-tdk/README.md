# Chinaz site TDK Flow

This runnable Flow uses the static `chinaz` connection to read public TDK metadata for one domain. The `found` branch stores the typed result in a Dex Attribute and completes. Optional error branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/chinaz/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/site-tdk/flow/workflow.go --schema-version 2.0 --json --out ./build/site-tdk
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override chinaz=<artifact-directory>`. In Dex Web **Connections**, create the `chinaz` connection. Open [Chinaz Site TDK Information](https://api.chinaz.com/ApiDetails/SiteData), apply for or purchase the API, then find the APPKEY in User Center > My Data and enter it in protected `api_key`. Leave base URL, timeout, and response size blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/site-tdk
```

In Dex Web **Start Flow**, choose `ChinazSiteTDK`, enter a unique Flow ID, and supply a public domain without scheme or path:

```json
{"domain":"example.com"}
```

The mock-backed integration test uses a real Dex Worker and Client with synthetic site metadata and no provider credential.
