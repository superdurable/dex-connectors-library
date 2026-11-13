# 51Tracking shipment status counts Flow

This runnable Flow uses the static `fiftyonetracking` connection to read aggregate parcel status counts. The `counted` branch stores the typed result in a Dex Attribute and completes. Optional error branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/fiftyonetracking/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/shipment-status-counts/flow/workflow.go --schema-version 2.0 --json --out ./build/shipment-status-counts
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override fiftyonetracking=<artifact-directory>`. In Dex Web **Connections**, create `fiftyonetracking`. Sign in to the [51Tracking console](https://www.51tracking.com/), open **Settings > Get API Key**, name and generate a key, then enter it in protected `api_key` without a prefix. The [official key guide](https://docs.51tracking.com/article/12) shows the steps. Leave base URL, timeout, and response size blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/shipment-status-counts
```

In Dex Web **Start Flow**, choose `FiftyOneTrackingShipmentStatusCounts`, enter a unique Flow ID, and use empty input:

```json
{}
```

The mock-backed integration test uses a real Dex Worker and Client with synthetic counts and no provider credential.
