# Shopify connector

Read-only Dex Query Steps for Shopify. The module is `github.com/superdurable/dex-connectors-library/connectors/shopify`. See the [runnable resource-read Flow](examples/resource-read/README.md).

## Operations

| Factory | Input | Output | Happy branch |
| --- | --- | --- | --- |
| `GetOrder` | `GetOrderInput` | `Order` | `found` |
| `ListOrders` | `ListOrdersInput` | `OrderPage` | `listed` |

Each operation has optional `notFound`, `providerRejected`, `invalidResponse`, and `defect` branches. Unwired optional branches fail the Flow. All reads use async Execute durability and the default one-hour retry budget.

Shopify calls REST Admin a legacy API and no longer permits creating new admin-created custom apps. This connector uses an access token from an existing app with order read access; new integrations need a separate supported token flow. [Shopify documents this limitation](https://shopify.dev/docs/apps/build/authentication-authorization/legacy/admin-custom-apps). By default, order reads cover only the last 60 days unless the app has additional access.

## Authentication and configuration

Follow the authorization guide in `connector.yaml` from [https://admin.shopify.com/](https://admin.shopify.com/). The connection resolves credentials for each operation. Secrets travel in HTTP headers and never enter results, receipts, failures, or URLs. Enter the required `shopSubdomain` from your store's myshopify.com domain; the connector appends `/admin/api/2026-10`. `timeout` bounds one HTTP request, and `maxResponseBytes` bounds JSON response size. The last two fields use a positive Go duration and a positive byte count, respectively; blank uses the displayed defaults.

`listOrders` sends `status=any` so open, closed, and cancelled orders can appear. Shopify's order history access still limits which older orders the app can read.

## Failure handling

| Provider response | Result |
| --- | --- |
| HTTP 200 with valid JSON | Happy branch |
| HTTP 404 | `notFound` |
| HTTP 401, 403, or other definite 4xx (except 408/429) | `providerRejected` |
| HTTP 408, 429, or 5xx; transport interruption | Retry |
| Malformed or oversized JSON | `invalidResponse` |
| Invalid local input or unavailable credentials | `defect` |

Provider error text is never copied into a Failure. Responses are bounded and redirects are not followed. The APIs are read-only, so there is no `uncertain` branch.

## Mocks

The generated `shopifymock` package returns one realistic default per operation from `connector.yaml`. The [mock-backed real Dex test](examples/resource-read/flow/workflow_mock_integration_test.go) executes the example Flow through a Worker and checks its mapped input and branch.

```go
mock := shopifymock.New(t, resourceread.ConnectionName)
flow := resourceread.NewFlow(mock.Connection())
```

## Provider documentation

- [https://shopify.dev/docs/api/admin-rest/latest/resources/order](https://shopify.dev/docs/api/admin-rest/latest/resources/order)
- [https://shopify.dev/docs/api/admin-rest](https://shopify.dev/docs/api/admin-rest)

## Unverified live behavior

No real Shopify account or Dex Web connection was available in this dispatch. Live credential creation, access scope selection, store configuration, provider data shapes beyond the documented fields, and response behavior remain unverified. GitHub Actions runs real Dex integration coverage; a user-controlled connection is needed for a live provider check.
