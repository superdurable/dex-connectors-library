# Braintree transaction lookup Flow

This runnable Flow reads one Braintree transaction with `getTransaction` through the static `braintree` connection and persists its profile. Configure `publicKey` and secret `privateKey` in Dex Web Connections using the [connection guide](../../../README.md). Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/braintree/go`, run:

```bash
dexcli visualize ./examples/transaction-read/flow/workflow.go --schema-version 2.0 --json --out /tmp/braintree-transaction-read-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override braintree=<artifact-directory>`. In Dex Web Connections select the `braintree` connection. Follow the [connection guide](../../../README.md) to enter `publicKey` and secret `privateKey`; leave `baseUrl`, `apiVersion`, `timeout`, and `maxResponseBytes` blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/braintree/go`:

```bash
GOWORK=off go run ./examples/transaction-read
```

Start `BraintreeTransactionRead` with a unique Flow ID and input `{"transactionId":"txn-1"}`. Replace the sample IDs with GraphQL transaction ID from your Braintree account. The Worker connects to Dex Server without an inbound listener.

CI runs the fixture-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Real Braintree authorization and lookup remain unverified.
