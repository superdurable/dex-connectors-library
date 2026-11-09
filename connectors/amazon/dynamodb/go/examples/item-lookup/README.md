# Go item lookup Flow

This runnable Flow calls `getItem` through the static `amazon-dynamodb` connection. It stores the selected `ItemResult` in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/amazon/dynamodb/go`, run:

```bash
dexcli visualize ./examples/item-lookup/flow/workflow.go --schema-version 2.0 --json --out /tmp/amazon-dynamodb-item-lookup-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override amazon-dynamodb=<artifact-directory>`. In Dex Web Connections select the `amazon-dynamodb` connection. Set `region` and secret `access_key_id` and `secret_access_key` as described in the [connector README](../../../README.md). Set `session_token` for temporary credentials. Leave `endpoint`, `timeout`, and `maxResponseBytes` blank for the documented behavior and displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/amazon/dynamodb/go`:

```bash
GOWORK=off go run ./examples/item-lookup
```

Start `AmazonDynamoDBItemLookup` with a unique Flow ID and input `{"tableName":"Examples","key":{"pk":{"S":"example"}},"consistentRead":false}`. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`.
