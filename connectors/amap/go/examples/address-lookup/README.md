# Amap address lookup Flow

This runnable Flow uses the static `amap` connection to resolve a structured
Chinese address. The `geocoded` branch stores the provider result in a Dex
Attribute and completes with the locations. Optional branches are unwired and
fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/amap/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/address-lookup/flow/workflow.go --schema-version 2.0 --json --out ./build/address-lookup
```

The output must report `valid: true`. An unpublished module may require its
local release metadata to resolve the connector contract.

## Run with Dex Web

Build the connector release artifact from this source and pass it to
`dexcli dev --connector-release-override amap=<artifact-directory>`.
In Dex Web **Connections**, create the `amap` connection with a Web Service Key
from [Amap Application Management](https://console.amap.com/dev/key/app).
Leave the base URL, timeout, and response-size fields blank for their defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/address-lookup
```

In Dex Web **Start Flow**, choose `AmapAddressLookup` and enter a unique Flow ID:

```json
{"address":"北京市朝阳区阜通东大街6号","city":"北京"}
```

The output contains the matching `locations`. A successful empty array means
the provider found no candidate. The mock-backed integration test uses a real
Dex Worker and Client with no Amap credential.
