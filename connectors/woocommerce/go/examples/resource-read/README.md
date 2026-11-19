# WooCommerce resource-read example

The worker registers a resource-read Flow and an order-update Flow on the static `woocommerce` connection. The read Flow uses `woocommerce.NewListOrdersStep`; the update Flow uses `woocommerce.NewUpdateOrderStep` with `OrderUpdateRequest{OrderID, Status}`. The mock-backed real Dex test starts both Flows and checks the provider inputs and happy branches. Optional error branches are unwired and fail the Flow.

## Generate strict FDG 2.0

From `connectors/woocommerce`:

```sh
mkdir -p build
dexcli visualize ./examples/resource-read/flow/workflow.go --schema-version 2.0 --json --out ./build/resource-read
```

The command should report `valid: true`. A local unpublished module can produce a release metadata warning.

## Configure and run

From the repository root, build a local release override, then start a Dex stack with the rendered graph:

```sh
mkdir -p /tmp/woocommerce-release
go run ./cmd/connectorctl release-artifact --manifest connectors/woocommerce/connector.yaml --module-path github.com/superdurable/dex-connectors-library/connectors/woocommerce/go --version v0.3.0 --tag connectors/woocommerce/go/v0.3.0 --source-sha "$(git rev-parse HEAD)" --output /tmp/woocommerce-release/connector-release.json --digest-output /tmp/woocommerce-release/connector-release.json.sha256
dexcli dev --flow-rendering-dir "$PWD/connectors/woocommerce/go/build" --connector-release-override woocommerce=/tmp/woocommerce-release
```

In Dex Web **Connections**, create the `woocommerce` connection with the manifest's authorization guide. Then start the Worker in another terminal:

```sh
cd connectors/woocommerce/go
go run ./examples/resource-read
```

The pull Worker connects outbound to Dex. In **Start Flow**, choose `WooCommerceResourceRead`, a unique Flow ID, and input like:

```json
{"page":1}
```

The Flow completes with the provider value. A missing resource selects the unwired `notFound` branch and fails the Flow.

The deterministic unit test checks input mapping and Flow identity. `workflow_mock_integration_test.go` runs the manifest default through a real Dex Worker in CI; it is not run during this host's heavy-work pause.
