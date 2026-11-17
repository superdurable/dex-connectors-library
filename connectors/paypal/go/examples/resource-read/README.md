# PayPal resource-read example

The two Flows use the static `paypal` connection. `PayPalResourceRead` calls `paypal.NewGetOrderStep`; `PayPalPayoutBatchRead` calls `paypal.NewGetPayoutBatchStep`. Optional error branches are unwired and fail the Flow.

## Generate strict FDG 2.0

From `connectors/paypal`:

```sh
mkdir -p build
dexcli visualize ./examples/resource-read/flow/workflow.go --schema-version 2.0 --json --out ./build/resource-read
dexcli visualize ./examples/resource-read/flow/payout_batch.go --schema-version 2.0 --lint app --json --out ./build/payout-batch
```

The command should report `valid: true`. A local unpublished module can produce a release metadata warning.

## Configure and run

From the repository root, build a local release override, then start a Dex stack with the rendered graph:

```sh
mkdir -p /tmp/paypal-release
go run ./cmd/connectorctl release-artifact --manifest connectors/paypal/connector.yaml --module-path github.com/superdurable/dex-connectors-library/connectors/paypal/go --version v0.3.0 --tag connectors/paypal/go/v0.3.0 --source-sha "$(git rev-parse HEAD)" --output /tmp/paypal-release/connector-release.json --digest-output /tmp/paypal-release/connector-release.json.sha256
dexcli dev --flow-rendering-dir "$PWD/connectors/paypal/go/build" --connector-release-override paypal=/tmp/paypal-release
```

In Dex Web **Connections**, create the `paypal` connection with the manifest's authorization guide. Then start the Worker in another terminal:

```sh
cd connectors/paypal/go
go run ./examples/resource-read
```

The pull Worker connects outbound to Dex. In **Start Flow**, choose `PayPalResourceRead`, a unique Flow ID, and input like:

```json
{"orderID":"ORDER101"}
```

The Flow completes with the provider value. A missing resource selects the unwired `notFound` branch and fails the Flow.

For `PayPalPayoutBatchRead`, start with `{"batchID":"BATCH101"}`. The Flow returns the payout batch header and first item page.

The deterministic unit test checks input mapping and Flow identity. `workflow_mock_integration_test.go` runs the manifest default through a real Dex Worker in CI; it is not run during this host's heavy-work pause.
