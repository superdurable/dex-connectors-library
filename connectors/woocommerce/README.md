# WooCommerce connector

Read-only Dex Query Steps for WooCommerce. The module is `github.com/superdurable/dex-connectors-library/connectors/woocommerce`. See the [runnable resource-read Flow](examples/resource-read/README.md).

## Operations

| Factory | Input | Output | Happy branch |
| --- | --- | --- | --- |
| `ListOrders` | `ListOrdersInput` | `OrderPage` | `listed` |
| `GetOrder` | `GetOrderInput` | `Order` | `found` |

Each operation has optional `notFound`, `providerRejected`, `invalidResponse`, and `defect` branches. Unwired optional branches fail the Flow. All reads use async Execute durability and the default one-hour retry budget.

## Authentication and configuration

Follow the authorization guide in `connector.yaml` from [https://woocommerce.com/document/woocommerce-rest-api/](https://woocommerce.com/document/woocommerce-rest-api/). The connection resolves credentials for each operation. Secrets travel in HTTP headers and never enter results, receipts, failures, or URLs. The `baseUrl` field uses the provider API URL, `timeout` bounds one HTTP request, and `maxResponseBytes` bounds JSON response size. The last two fields use a positive Go duration and a positive byte count, respectively; blank uses the displayed defaults.

For a live store, replace the `baseUrl` example host with your own HTTPS store before running.

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

The generated `woocommercemock` package returns one realistic default per operation from `connector.yaml`. The [mock-backed real Dex test](examples/resource-read/flow/workflow_mock_integration_test.go) executes the example Flow through a Worker and checks its mapped input and branch.

```go
mock := woocommercemock.New(t, resourceread.ConnectionName)
flow := resourceread.NewFlow(mock.Connection())
```

## Provider documentation

- [https://woocommerce.github.io/woocommerce-rest-api-docs/](https://woocommerce.github.io/woocommerce-rest-api-docs/)

## Unverified live behavior

No real WooCommerce account or Dex Web connection was available in this dispatch. Live credential creation, access scope selection, store configuration, provider data shapes beyond the documented fields, and response behavior remain unverified. GitHub Actions runs real Dex integration coverage; a user-controlled connection is needed for a live provider check.
