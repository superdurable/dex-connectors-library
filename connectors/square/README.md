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

Each operation has optional `providerRejected`, `invalidResponse`, and `defect` branches; reads and payment actions also have optional `notFound` where applicable. Unwired optional branches fail the Flow. Reads and writes use async Execute durability and the default one-hour retry budget. All writes use the `safe` repeat tier. `CreatePayment` and `UpdatePayment` pass a stable Square idempotency key in the request body. `CancelPayment` and `CompletePayment` read the payment status after an unknown response before repeating the action.

`CreatePaymentInput.SourceID` accepts `CASH` or `EXTERNAL` only. This records a payment received outside Square without persisting a payment token in Dex Flow input. Amounts are positive integers in the currency's minor unit, with an uppercase ISO currency code.

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
- [https://developer.squareup.com/docs/payments-api/retrieve-payments](https://developer.squareup.com/docs/payments-api/retrieve-payments)

## Unverified live behavior

No real Square account or Dex Web connection was available in this dispatch. Live credential creation, access scope selection, store configuration, provider data shapes beyond the documented fields, and response behavior remain unverified. GitHub Actions runs real Dex integration coverage; a user-controlled connection is needed for a live provider check.
