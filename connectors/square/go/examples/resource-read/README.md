# Square resource-read example

This Flow uses the static `square` connection. By default, `square.NewListPaymentsStep` reads payments. With `recordCashPayment: true`, `square.NewCreatePaymentStep` records a cash payment. With `refundPaymentId`, `square.NewRefundPaymentStep` refunds a completed payment. With `createCustomer: true`, `square.NewCreateCustomerStep` creates a customer. With `orderLocationId`, `square.NewCreateOrderStep` creates an order. With `catalogItemName`, `square.NewUpsertCatalogObjectStep` creates a catalog item. With `inventoryVariationId`, `square.NewSetInventoryPhysicalCountStep` reconciles stock. With `invoiceOrderId`, `square.NewCreateInvoiceStep` creates a draft invoice. With `listLocations: true`, `square.NewListLocationsStep` discovers seller location IDs. Start input is typed as `Request`; the result is `Outcome`. Optional error branches are unwired and fail the Flow.

## Generate strict FDG 2.0

From `connectors/square`:

```sh
mkdir -p build
dexcli visualize ./examples/resource-read/flow/workflow.go --schema-version 2.0 --json --out ./build/resource-read
```

The command should report `valid: true`. A local unpublished module can produce a release metadata warning.

## Configure and run

From the repository root, build a local release override, then start a Dex stack with the rendered graph:

```sh
mkdir -p /tmp/square-release
go run ./cmd/connectorctl release-artifact --manifest connectors/square/connector.yaml --module-path github.com/superdurable/dex-connectors-library/connectors/square/go --version v0.3.0 --tag connectors/square/go/v0.3.0 --source-sha "$(git rev-parse HEAD)" --output /tmp/square-release/connector-release.json --digest-output /tmp/square-release/connector-release.json.sha256
dexcli dev -open=false --flow-rendering-dir "$PWD/connectors/square/go/build" --connector-release-override square=/tmp/square-release
```

In Dex Web **Connections**, create the `square` connection with the manifest's authorization guide. Then start the Worker in another terminal:

```sh
cd connectors/square/go
go run ./examples/resource-read
```

The pull Worker connects outbound to Dex. In **Start Flow**, choose `SquareResourceRead`, a unique Flow ID, and input like:

```json
{"cursor":""}
```

The Flow completes with the provider value. A missing resource selects the unwired `notFound` branch and fails the Flow.

To record a cash payment, enable `PAYMENTS_WRITE` in the Square application and start with `{"recordCashPayment":true,"amountMoney":{"amount":2900,"currency":"USD"}}`. The amount is in minor currency units. The connector sets `source_id` to `CASH`; no card token is stored in the Flow.

To refund a completed payment, start with `{"refundPaymentId":"pay_101","amountMoney":{"amount":1000,"currency":"USD"}}`. Use the payment's Square ID and a positive amount in the currency's minor unit. The connector derives the required refund idempotency key from the Dex call identity.

To create a customer, enable `CUSTOMERS_WRITE` and start with `{"createCustomer":true,"customerGivenName":"Ada","customerEmailAddress":"ada@example.com"}`. Square requires at least one public name or email field; the connector derives a stable idempotency key.

To create an order, enable `ORDERS_WRITE`, find the seller location ID in Square Dashboard > Account & Settings > Business > Locations, and start with `{"orderLocationId":"loc_101","orderLineItemName":"Coffee","orderQuantity":"2","amountMoney":{"amount":500,"currency":"USD"}}`. The price is in the currency's minor unit. The connector derives a stable idempotency key.

To create a catalog item, enable `ITEMS_WRITE` and start with `{"catalogItemName":"Coffee"}`. The example builds a complete ITEM object with one variable-priced variation and derives a stable idempotency key. For updates outside this example, pass a complete object to `UpsertCatalogObject`; omitted fields and child variations are cleared by Square.

To reconcile stock, enable `INVENTORY_WRITE` and start with `{"inventoryVariationId":"variation_101","inventoryLocationId":"loc_101","inventoryQuantity":"5","inventoryOccurredAt":"2026-11-17T12:00:00Z"}`. Find the variation ID in Square Dashboard > Items & services > Item library and the location ID in Account & Settings > Business > Locations. Quantity is a nonnegative decimal string; occurredAt is the actual RFC3339 observation time. The connector derives the required UUID idempotency key. Use a current timestamp from the physical count, rather than the example value.

To create a draft invoice, enable `INVOICES_WRITE` and `ORDERS_WRITE`, find an open order and its location in Square Dashboard > Orders, and find a customer with an email address in Customer Directory. Start with `{"invoiceOrderId":"order_101","invoiceLocationId":"loc_101","invoiceCustomerId":"customer_101","invoiceDueDate":"2030-01-24"}`. The due date is `YYYY-MM-DD`. The example leaves the invoice in `DRAFT`; a separate `PublishInvoice` Step is required to send it. The connector derives a stable idempotency key. Use actual IDs and a valid future due date.

To discover location IDs, enable `MERCHANT_PROFILE_READ` and start with `{"listLocations":true}`. The result includes active and inactive Square locations. Select an active ID for new orders, invoices, and inventory changes.

The deterministic unit test checks input mapping and Flow identity. `workflow_mock_integration_test.go` runs read, cash write, refund, customer create, order create, catalog create, inventory reconciliation, invoice creation, and location discovery paths through a real Dex Worker using manifest mocks.
