# Go statement results Flow

This runnable Flow calls `getStatementResult` through the static `amazon-redshift` connection. It stores the selected `StatementResultPage` in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/amazon/redshift/go`, run:

```bash
dexcli visualize ./examples/statement-results/flow/workflow.go --schema-version 2.0 --json --out /tmp/amazon-redshift-statement-results-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override amazon-redshift=<artifact-directory>`. In Dex Web Connections select the `amazon-redshift` connection. Set `region` and secret `access_key_id` and `secret_access_key` as described in the [connector README](../../../README.md). Set `session_token` for temporary credentials. Leave `endpoint`, `timeout`, and `maxResponseBytes` blank for the documented behavior and displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/amazon/redshift/go`:

```bash
GOWORK=off go run ./examples/statement-results
```

Start `AmazonRedshiftStatementResults` with a unique Flow ID and input `{"statementId":"d9b6c0c9-0747-4bf4-b142-e8883122f766","nextToken":""}`. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`.
