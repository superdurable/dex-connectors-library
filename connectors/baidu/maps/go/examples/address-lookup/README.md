# Baidu Maps address lookup Flow

This runnable Flow uses the static `baidu-maps` connection to resolve a
structured Chinese address. The `geocoded` branch stores the BD-09 location
and precision measures in a Dex Attribute and completes with them. Optional
branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/baidu/maps/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/address-lookup/flow/workflow.go --schema-version 2.0 --json --out ./build/address-lookup
```

The output must report `valid: true`. An unpublished module may require local
release metadata to resolve the connector contract.

## Run with Dex Web

Build this connector's release artifact and pass it to
`dexcli dev --connector-release-override baidu-maps=<artifact-directory>`.
In Dex Web **Connections**, create the `baidu-maps` connection with a
server-side AK from [Baidu Maps Application Management](https://lbs.baidu.com/apiconsole/key).
Leave the three defaulted settings blank.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/address-lookup
```

In Dex Web **Start Flow**, choose `BaiduMapsAddressLookup` and enter a unique
Flow ID with this input:

```json
{"address":"北京市海淀区上地十街10号","city":"北京市"}
```

The output contains `longitude`, `latitude`, `precise`, `confidence`,
`comprehension`, and `level`. The mock-backed integration test uses a real Dex
Worker and Client with no Baidu Maps credential.
