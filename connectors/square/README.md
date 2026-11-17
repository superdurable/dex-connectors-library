# Square connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/square/go@v0.3.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/square`.

Typed Dex Query and Mutation Steps for Square. The module is `github.com/superdurable/dex-connectors-library/connectors/square/go`. See the [runnable resource-read Flow](go/examples/resource-read/README.md).

The TypeScript package is [`@superdurable/connector-square`](typescript/README.md).

The Python package is [`dex-connector-square`](python/README.md), imported as
`dex_connectors.square`.

## Operations

| Factory | Input | Output | Happy branch |
| --- | --- | --- | --- |
| `ListPayments` | `ListPaymentsInput` | `PaymentPage` | `listed` |
| `GetPayment` | `GetPaymentInput` | `Payment` | `found` |
| `CreatePayment` | `CreatePaymentInput` | `Payment` | `created` |
| `UpdatePayment` | `UpdatePaymentInput` | `Payment` | `updated` |
| `CancelPayment` | `CancelPaymentInput` | `Payment` | `canceled` |
| `CompletePayment` | `CompletePaymentInput` | `Payment` | `completed` |
| `ListRefunds` | `ListRefundsInput` | `RefundPage` | `listed` |
| `GetRefund` | `GetRefundInput` | `Refund` | `found` |
| `RefundPayment` | `RefundPaymentInput` | `Refund` | `refunded` |
| `ListCustomers` | `ListCustomersInput` | `CustomerPage` | `listed` |
| `SearchCustomers` | `SearchCustomersInput` | `CustomerPage` | `listed` |
| `GetCustomer` | `GetCustomerInput` | `Customer` | `found` |
| `CreateCustomer` | `CreateCustomerInput` | `Customer` | `created` |
| `UpdateCustomer` | `UpdateCustomerInput` | `Customer` | `updated` |
| `DeleteCustomer` | `DeleteCustomerInput` | `DeletedCustomer` | `deleted` |
| `SearchOrders` | `SearchOrdersInput` | `OrderPage` | `listed` |
| `GetOrder` | `GetOrderInput` | `Order` | `found` |
| `CreateOrder` | `CreateOrderInput` | `Order` | `created` |
| `UpdateOrder` | `UpdateOrderInput` | `Order` | `updated` |
| `ListCatalogObjects` | `ListCatalogObjectsInput` | `CatalogPage` | `listed` |
| `SearchCatalogObjects` | `SearchCatalogObjectsInput` | `CatalogPage` | `listed` |
| `GetCatalogObject` | `GetCatalogObjectInput` | `CatalogObject` | `found` |
| `UpsertCatalogObject` | `UpsertCatalogObjectInput` | `CatalogObject` | `upserted` |
| `DeleteCatalogObject` | `DeleteCatalogObjectInput` | `DeletedCatalogObjects` | `deleted` |
| `RetrieveInventoryCount` | `RetrieveInventoryCountInput` | `InventoryCounts` | `found` |
| `SetInventoryPhysicalCount` | `SetInventoryPhysicalCountInput` | `InventoryCounts` | `reconciled` |
| `ListInvoices` | `ListInvoicesInput` | `InvoicePage` | `listed` |
| `GetInvoice` | `GetInvoiceInput` | `Invoice` | `found` |
| `CreateInvoice` | `CreateInvoiceInput` | `Invoice` | `created` |
| `UpdateInvoice` | `UpdateInvoiceInput` | `Invoice` | `updated` |
| `PublishInvoice` | `PublishInvoiceInput` | `Invoice` | `published` |
| `CancelInvoice` | `CancelInvoiceInput` | `Invoice` | `canceled` |
| `DeleteInvoice` | `DeleteInvoiceInput` | `DeletedInvoice` | `deleted` |
| `ListLocations` | `ListLocationsInput` | `LocationList` | `listed` |
| `GetLocation` | `GetLocationInput` | `Location` | `found` |

Each operation has optional `providerRejected`, `invalidResponse`, and `defect` branches; reads and payment actions also have optional `notFound` where applicable. Unwired optional branches fail the Flow. Reads and writes use async Execute durability and the default one-hour retry budget. All writes use the `safe` repeat tier. `CreatePayment`, `UpdatePayment`, and `RefundPayment` pass a stable Square idempotency key in the request body. `CancelPayment` and `CompletePayment` read the payment status after an unknown response before repeating the action.

`CreatePaymentInput.SourceID` accepts `CASH` or `EXTERNAL` only. This records a payment received outside Square without persisting a payment token in Dex Flow input. Amounts are positive integers in the currency's minor unit, with an uppercase ISO currency code.

Customer operations require `CUSTOMERS_READ` or `CUSTOMERS_WRITE` on the token as appropriate. `SearchCustomers` matches exact email and/or external reference, returning one page. `CreateCustomer` supplies a stable provider idempotency key. `UpdateCustomer` applies nonblank fields as absolute values; blank fields mean no change. `DeleteCustomer` takes a Square customer ID and reports it on success.

Order operations require `ORDERS_READ` or `ORDERS_WRITE`. `SearchOrders` returns one page for a seller location. `CreateOrder` creates one custom-priced line item from a location ID, item name, positive decimal quantity, and minor-unit price. `UpdateOrder` changes the external reference of an open order using its current `version`. Both writes use Square's idempotency key in the request body.

Catalog operations require `ITEMS_READ` or `ITEMS_WRITE`. List and text search return one page of `ITEM` objects. `GetCatalogObject` returns the complete provider object. `UpsertCatalogObject` creates or **fully replaces** an `ITEM`; its `Object` must include every field and child variation to retain. Retrieve the current object before an update, change the desired fields, and pass the complete object with its version. Square clears any omitted fields during replacement. The connector derives the provider idempotency key. `DeleteCatalogObject` deletes the item and dependent variations; the output lists every deleted object ID.

Inventory operations require `INVENTORY_READ` or `INVENTORY_WRITE`. `RetrieveInventoryCount` reads one `ITEM_VARIATION` and optionally restricts results to one location; use `Cursor` for another page. `SetInventoryPhysicalCount` reconciles the observed `IN_STOCK` quantity at one location using Square's batch-change endpoint. Supply a nonnegative decimal `Quantity` string and the actual observation time as RFC3339 `OccurredAt`; retries retain that time and the same UUID idempotency key. Find the variation ID under Square Dashboard > Items & services > Item library and the location ID under Account & Settings > Business > Locations. Square's inventory change affects stock, so verify the location and observed quantity before starting the Flow.

Invoice reads require `INVOICES_READ`; writes require `INVOICES_WRITE` and `ORDERS_WRITE`. `ListInvoices` pages by location; `GetInvoice` returns the complete provider invoice. `CreateInvoice` creates a draft email invoice for an open order and a customer with an email address, with one `BALANCE` payment due on `DueDate` (`YYYY-MM-DD`). `UpdateInvoice` changes its description by current version; blank clears the description. `PublishInvoice` starts Square delivery and payment processing for a draft invoice, using the current version and a stable provider idempotency key. `CancelInvoice` stops collection on a published invoice; `DeleteInvoice` permanently removes a draft invoice and cancels its order. Read the current version with `GetInvoice` before an action. Find the location and order in Square Dashboard > Orders, the customer in Customer Directory, and the draft invoice in Invoices. Square may charge a card on file when publishing an existing invoice configured for automatic payment; inspect the invoice before publishing. The connector's create path uses email delivery and no automatic card charge.

Location reads require `MERCHANT_PROFILE_READ`. `ListLocations` returns seller locations, including inactive ones, so a Flow can discover IDs before querying orders, invoices, or inventory. `GetLocation` accepts a Square location ID or `main` for the primary location. Neither operation changes location configuration.

## Authentication and configuration

Follow the authorization guide in `connector.yaml` from [https://developer.squareup.com/apps](https://developer.squareup.com/apps). The connection resolves credentials for each operation. Secrets travel in HTTP headers and never enter results, receipts, failures, or URLs. The `baseUrl` field uses the provider API URL, `timeout` bounds one HTTP request, and `maxResponseBytes` bounds JSON response size. The last two fields use a positive Go duration and a positive byte count, respectively; blank uses the displayed defaults.

## Failure handling

| Provider response | Result |
| --- | --- |
| HTTP 200 with valid JSON | Happy branch |
| HTTP 404 | `notFound` |
| HTTP 401, 403, or other definite 4xx (except 408/429) | `providerRejected` |
| HTTP 408, 429, or 5xx; transport interruption | Retry |
| Malformed or oversized JSON | `invalidResponse` |
| Invalid local input or unavailable credentials | `defect` |

Provider error text is never copied into a Failure. Responses are bounded and redirects are not followed. Safe write retries do not select `uncertain`.

## Mocks

The generated `squaremock` package returns one realistic default per operation from `connector.yaml`. The [mock-backed real Dex test](go/examples/resource-read/flow/workflow_mock_integration_test.go) executes the example Flow through a Worker and checks its mapped input and branch.

```go
mock := squaremock.New(t, resourceread.ConnectionName)
flow := resourceread.NewFlow(mock.Connection())
```

## Provider documentation

- [https://developer.squareup.com/reference/square/payments-api/ListPayments](https://developer.squareup.com/reference/square/payments-api/ListPayments)
- [https://developer.squareup.com/reference/square/payments-api/GetPayment](https://developer.squareup.com/reference/square/payments-api/GetPayment)
- [https://developer.squareup.com/reference/square/payments-api/create-payment](https://developer.squareup.com/reference/square/payments-api/create-payment)
- [https://developer.squareup.com/reference/square/payments-api/update-payment](https://developer.squareup.com/reference/square/payments-api/update-payment)
- [https://developer.squareup.com/reference/square/payments-api/cancel-payment](https://developer.squareup.com/reference/square/payments-api/cancel-payment)
- [https://developer.squareup.com/reference/square/payments-api/complete-payment](https://developer.squareup.com/reference/square/payments-api/complete-payment)
- [https://developer.squareup.com/reference/square/refunds-api/list-payment-refunds](https://developer.squareup.com/reference/square/refunds-api/list-payment-refunds)
- [https://developer.squareup.com/reference/square/refunds-api/get-payment-refund](https://developer.squareup.com/reference/square/refunds-api/get-payment-refund)
- [https://developer.squareup.com/reference/square/refunds-api/refund-payment](https://developer.squareup.com/reference/square/refunds-api/refund-payment)
- [https://developer.squareup.com/reference/square/customers-api](https://developer.squareup.com/reference/square/customers-api)
- [https://developer.squareup.com/reference/square/orders](https://developer.squareup.com/reference/square/orders)
- [https://developer.squareup.com/reference/square/catalog](https://developer.squareup.com/reference/square/catalog)
- [https://developer.squareup.com/reference/square/inventory-api/retrieve-inventory-count](https://developer.squareup.com/reference/square/inventory-api/retrieve-inventory-count)
- [https://developer.squareup.com/reference/square/inventory-api/batch-change-inventory](https://developer.squareup.com/reference/square/inventory-api/batch-change-inventory)
- [https://developer.squareup.com/reference/square/invoices-api](https://developer.squareup.com/reference/square/invoices-api)
- [https://developer.squareup.com/docs/invoices-api/create-publish-invoices](https://developer.squareup.com/docs/invoices-api/create-publish-invoices)
- [https://developer.squareup.com/reference/square/locations-api/list-locations](https://developer.squareup.com/reference/square/locations-api/list-locations)
- [https://developer.squareup.com/reference/square/locations-api/retrieve-location](https://developer.squareup.com/reference/square/locations-api/retrieve-location)
- [https://developer.squareup.com/docs/payments-api/retrieve-payments](https://developer.squareup.com/docs/payments-api/retrieve-payments)

## Unverified live behavior

No real Square account or Dex Web connection was available in this dispatch. Live credential creation, access scope selection, store configuration, provider data shapes beyond the documented fields, and response behavior remain unverified. GitHub Actions runs real Dex integration coverage; a user-controlled connection is needed for a live provider check.
