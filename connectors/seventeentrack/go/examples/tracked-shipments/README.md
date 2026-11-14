# 17TRACK shipment status list Flow

This runnable Flow uses the static `seventeentrack` connection to list a page of already registered shipment statuses. The `listed` branch stores the typed result in a Dex Attribute and completes. Optional error branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/seventeentrack/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/tracked-shipments/flow/workflow.go --schema-version 2.0 --json --out ./build/tracked-shipments
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override seventeentrack=<artifact-directory>`. In Dex Web **Connections**, create `seventeentrack`. Sign in to the [17TRACK Tracking API console](https://api.17track.net/zh-cn/), open **Settings**, find your API key, and enter it in the protected `api_key` field without a prefix. The [official guide](https://api.17track.net/zh-cn/doc) also describes webhook setup for ongoing tracking updates. Leave base URL, timeout, and response size blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/tracked-shipments
```

In Dex Web **Start Flow**, choose `SeventeenTrackShipmentList`, enter a unique Flow ID, and use a one-based page number. Zero selects page 1:

```json
{"pageNumber":1}
```

The mock-backed integration test uses a real Dex Worker and Client with a synthetic shipment and no provider credential.
