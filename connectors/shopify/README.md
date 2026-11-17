# Shopify connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/shopify/go@v0.3.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/shopify`.

Dex Query and Mutation Steps for Shopify orders and products. The module is `github.com/superdurable/dex-connectors-library/connectors/shopify/go`. See the [runnable order and product Flows](go/examples/resource-read/README.md).

The Python package is [`dex-connector-shopify`](python/README.md), imported as
`dex_connectors.shopify`.
The TypeScript package is [`@superdurable/connector-shopify`](typescript/README.md),
with a [runnable order read Flow](typescript/examples/order-read/README.md).

## Operations

| Factory | Input | Output | Happy branch |
| --- | --- | --- | --- |
| `GetOrder` | `GetOrderInput` | `Order` | `found` |
| `ListOrders` | `ListOrdersInput` | `OrderPage` | `listed` |
| `CountOrders` | `CountOrdersInput` | `OrderCount` | `counted` |
| `CreateOrder` | `CreateOrderInput` | `Order` | `created` |
| `UpdateOrder` | `UpdateOrderInput` | `Order` | `updated` |
| `DeleteOrder` | `DeleteOrderInput` | `DeletedOrder` | `deleted` |
| `GetProduct` | `GetProductInput` | `Product` | `found` |
| `ListProducts` | `ListProductsInput` | `ProductPage` | `listed` |
| `CreateProduct` | `CreateProductInput` | `Product` | `created` |
| `UpdateProduct` | `UpdateProductInput` | `Product` | `updated` |
| `DeleteProduct` | `DeleteProductInput` | `DeletedProduct` | `deleted` |

Reads and repeat-safe writes use async Execute durability. `CreateOrder` and `CreateProduct` are limited to three sends after an ambiguous outcome and select `uncertain` when they cannot confirm creation. Each operation's non-happy branches are optional; unwired optional branches fail the Flow. The default one-hour retry budget applies.

Shopify calls REST Admin a legacy API and no longer permits creating new admin-created custom apps. This connector uses an access token from an existing app with `read_orders` for reads or `write_orders` for writes; new integrations need a separate supported token flow. Existing read-only connections continue to serve read operations. [Shopify documents this limitation](https://shopify.dev/docs/apps/build/authentication-authorization/legacy/admin-custom-apps). By default, order access covers only the last 60 days unless the app has additional access.

Product reads require `read_products` or `write_products`; product writes require `write_products`. Grant these in the existing app's **Configuration > Admin API access scopes** before using product Steps. Changing a provider app's scopes can require refreshing its access token; the connector credential schema stays the same.

## Authentication and configuration

Follow the authorization guide in `connector.yaml` from [https://admin.shopify.com/](https://admin.shopify.com/). The connection resolves credentials for each operation. Secrets travel in HTTP headers and never enter results, receipts, failures, or URLs. Enter the required `shopSubdomain` from your store's myshopify.com domain; the connector appends `/admin/api/2026-10`. `timeout` bounds one HTTP request, and `maxResponseBytes` bounds JSON response size. The last two fields use a positive Go duration and a positive byte count, respectively; blank uses the displayed defaults.

`listOrders` and `countOrders` send `status=any` when blank, so open, closed, and cancelled orders can appear. `ListOrdersInput` supports the documented order status, financial status, fulfillment status, date, ID, and attribution filters. A page returns `NextPageInfo` from Shopify's `Link` header; pass it as `PageInfo` for the next page with only `Limit`, because Shopify forbids other filters on cursor requests. Cursor values are temporary. Shopify's order history access still limits which older orders the app can read. `CountOrdersInput` accepts an optional status filter.

`CreateOrder` accepts titled line items with positive quantities and decimal prices. It disables customer receipts and inventory claims, and does not collect payment. `UpdateOrder` replaces only note and tags; use blank strings to clear them. `DeleteOrder` removes an order by numeric ID. Shopify does not document an idempotency key for these REST creates, so an ambiguous create is bounded by the mutation send cap and may become `uncertain`.

`ListProducts` returns one bounded page and a temporary `NextPageInfo` cursor. Pass it back with only `Limit` for the next page; filters belong on the first request. `CreateProduct` supports a title, description HTML, vendor, product type, tags, and status. `UpdateProduct` changes only pointer fields supplied by the caller, so a pointer to blank text clears an optional field. Product summaries intentionally omit images and variants; Shopify's REST product write endpoints are legacy and limited compared with the current GraphQL product model.

## Failure handling

| Provider response | Result |
| --- | --- |
| HTTP 200 with valid JSON | Happy branch |
| HTTP 404 | `notFound` |
| HTTP 401, 403, or other definite 4xx (except 408/429) | `providerRejected` |
| HTTP 408, 429, or 5xx; transport interruption | Retry |
| Malformed or oversized JSON | `invalidResponse` |
| Invalid local input or unavailable credentials | `defect` |

Provider error text is never copied into a Failure. Responses are bounded and redirects are not followed. Only limited-tier creation has an `uncertain` branch.

## Mocks

The generated `shopifymock` package returns one realistic default per operation from `connector.yaml`. The [mock-backed real Dex test](go/examples/resource-read/flow/workflow_mock_integration_test.go) executes both example Flows through a Worker and checks their mapped inputs and branches.

```go
mock := shopifymock.New(t, resourceread.ConnectionName)
flow := resourceread.NewFlow(mock.Connection())
```

## Provider documentation

- [https://shopify.dev/docs/api/admin-rest/latest/resources/order](https://shopify.dev/docs/api/admin-rest/latest/resources/order)
- [https://shopify.dev/docs/api/admin-rest/latest/resources/product](https://shopify.dev/docs/api/admin-rest/latest/resources/product)
- [https://shopify.dev/docs/api/admin-rest/usage/pagination](https://shopify.dev/docs/api/admin-rest/usage/pagination)
- [https://shopify.dev/docs/api/admin-rest/usage/access-scopes](https://shopify.dev/docs/api/admin-rest/usage/access-scopes)
- [https://shopify.dev/docs/api/admin-rest](https://shopify.dev/docs/api/admin-rest)

## Unverified live behavior

No real Shopify account or Dex Web connection was available in this dispatch. Live credential creation, access scope selection, store configuration, provider data shapes beyond the documented fields, and response behavior remain unverified. GitHub Actions runs real Dex integration coverage; a user-controlled connection is needed for a live provider check.
