# Square connector

Read-only Dex Query Steps for Square. The module is `github.com/superdurable/dex-connectors-library/connectors/square`. See the [runnable resource-read Flow](examples/resource-read/README.md).

## Operations

| Factory | Input | Output | Happy branch |
| --- | --- | --- | --- |
| `ListPayments` | `ListPaymentsInput` | `PaymentPage` | `listed` |
| `GetPayment` | `GetPaymentInput` | `Payment` | `found` |

Each operation has optional `notFound`, `providerRejected`, `invalidResponse`, and `defect` branches. Unwired optional branches fail the Flow. All reads use async Execute durability and the default one-hour retry budget.

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

Provider error text is never copied into a Failure. Responses are bounded and redirects are not followed. The APIs are read-only, so there is no `uncertain` branch.

## Mocks

The generated `squaremock` package returns one realistic default per operation from `connector.yaml`. The [mock-backed real Dex test](examples/resource-read/flow/workflow_mock_integration_test.go) executes the example Flow through a Worker and checks its mapped input and branch.

```go
mock := squaremock.New(t, resourceread.ConnectionName)
flow := resourceread.NewFlow(mock.Connection())
```

## Provider documentation

- [https://developer.squareup.com/reference/square/payments-api/ListPayments](https://developer.squareup.com/reference/square/payments-api/ListPayments)
- [https://developer.squareup.com/reference/square/payments-api/GetPayment](https://developer.squareup.com/reference/square/payments-api/GetPayment)
- [https://developer.squareup.com/docs/payments-api/retrieve-payments](https://developer.squareup.com/docs/payments-api/retrieve-payments)

## Unverified live behavior

No real Square account or Dex Web connection was available in this dispatch. Live credential creation, access scope selection, store configuration, provider data shapes beyond the documented fields, and response behavior remain unverified. GitHub Actions runs real Dex integration coverage; a user-controlled connection is needed for a live provider check.
