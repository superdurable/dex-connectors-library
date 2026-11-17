# ClickHouse table inspection Flow

This runnable Flow calls `listDatabases` and then `listTables` through the static `clickhouse` connection. It persists the requested database and returned table list in Attributes and exposes the table list through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/clickhouse/go`, run:

```bash
dexcli visualize ./examples/table-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/clickhouse-table-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev -open=false` with `--connector-release-override clickhouse=<artifact-directory>`. In Dex Web Connections select the `clickhouse` connection. Follow the [connection guide](../../../README.md) to enter `baseUrl`, `username`, and secret `password`; leave `timeout` and `maxResponseBytes` blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/clickhouse/go`:

```bash
GOWORK=off go run ./examples/table-inspection
```

Start `ClickHouseTableInspection` with a unique Flow ID and input `{"database":"analytics"}`. Replace the sample with a database returned by `SHOW DATABASES`. The Worker connects to Dex Server without an inbound listener.

CI runs the fixture-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Real ClickHouse authorization and query execution remain unverified.
