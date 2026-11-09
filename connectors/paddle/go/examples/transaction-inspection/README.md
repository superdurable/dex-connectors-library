# Paddle transaction inspection Flow

This runnable Flow calls `getTransaction` through the static `paddle` connection. It persists the selected transaction status in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/paddle/go`, run:

```bash
dexcli visualize ./examples/transaction-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/paddle-transaction-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override paddle=<artifact-directory>`. In Dex Web Connections select the `paddle` connection. Follow the [connection guide](../../../README.md) to create a server-side API key and enter it in secret `api_key`. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for Sandbox defaults; select the Live API URL for Live.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/paddle/go`:

```bash
GOWORK=off go run ./examples/transaction-inspection
```

Start `PaddleTransactionInspection` with a unique Flow ID and input `{"transactionId":"txn_01hv8wptq8987qeep44cyrewp9"}`. Replace the sample with a transaction ID returned by Paddle. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Real Paddle authorization and retrieval remain unverified.
