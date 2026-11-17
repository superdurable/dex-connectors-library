# Shopify resource-read example

This example registers three Flows on the static `shopify` connection. `ShopifyResourceRead` reads one order; `ShopifyOrderCount` counts orders; `ShopifyCreateOrder` creates an unpaid order with bounded retries. Optional error branches are unwired and fail the Flow.

## Generate strict FDG 2.0

From `connectors/shopify`:

```sh
mkdir -p build
dexcli visualize ./examples/resource-read/flow/workflow.go --schema-version 2.0 --json --out ./build/resource-read
```

The command should report `valid: true`. A local unpublished module can produce a release metadata warning.

## Configure and run

From the repository root, build a local release override, then start a Dex stack with the rendered graph:

```sh
mkdir -p /tmp/shopify-release
go run ./cmd/connectorctl release-artifact --manifest connectors/shopify/connector.yaml --module-path github.com/superdurable/dex-connectors-library/connectors/shopify/go --version v0.3.0 --tag connectors/shopify/go/v0.3.0 --source-sha "$(git rev-parse HEAD)" --output /tmp/shopify-release/connector-release.json --digest-output /tmp/shopify-release/connector-release.json.sha256
dexcli dev --flow-rendering-dir "$PWD/connectors/shopify/go/build" --connector-release-override shopify=/tmp/shopify-release
```

In Dex Web **Connections**, create the `shopify` connection with the manifest's authorization guide. Then start the Worker in another terminal:

```sh
cd connectors/shopify/go
go run ./examples/resource-read
```

The pull Worker connects outbound to Dex. In **Start Flow**, choose `ShopifyResourceRead`, a unique Flow ID, and input like:

```json
{"orderID":101}
```

The Flow completes with the provider value. A missing resource selects the unwired `notFound` branch and fails the Flow.

To count closed orders, start `ShopifyOrderCount` with a unique Flow ID and input `{"status":"closed"}`. Blank status counts every order status.

With an existing app token granted `write_orders`, start `ShopifyCreateOrder` with a unique Flow ID and input such as `{"lineItems":[{"title":"Sample item","quantity":1,"price":"29.00"}]}`. Shopify creates the order without collecting payment, sending a receipt, or claiming inventory.

The deterministic unit test checks input mapping and Flow identity. `workflow_mock_integration_test.go` runs the manifest defaults through a real Dex Worker in CI and can be run locally against `dexcli dev -open=false`.
