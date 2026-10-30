# Contentful connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/contentful/go@v0.2.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/contentful`.

This Dex connector exposes `getEntry`, `listEntries`. The runnable [sample Flow](go/examples/sample-contentful/flow/workflow.go) uses `getEntry` with the static `contentful` connection. Each operation uses the generated typed Step factory.

## Provider contract

The Content Delivery API is read-only and returns published entries. The token travels in the Authorization header, never in a query parameter.

Official documentation used for the endpoint, authentication, and response contract:

- [https://www.contentful.com/developers/docs/references/content-delivery-api/overview/](https://www.contentful.com/developers/docs/references/content-delivery-api/overview/)
- [https://www.contentful.com/developers/docs/references/content-delivery-api/entries/get-an-entry/](https://www.contentful.com/developers/docs/references/content-delivery-api/entries/get-an-entry/)
- [https://www.contentful.com/developers/docs/references/content-delivery-api/entries/get-all-entries/](https://www.contentful.com/developers/docs/references/content-delivery-api/entries/get-all-entries/)

## Authorization and configuration

Start at [https://app.contentful.com](https://app.contentful.com) and follow the authorization guide shown in Dex Web Connections. The `delivery_token` field is secret. It is resolved for each call and sent only in the `Authorization` header. The connector never places it in a URL, Result, receipt, Failure, or log. Leave `timeout` blank for 15 seconds and `maxResponseBytes` blank for 2,097,152 bytes. A larger bounded value can be entered if a real response page exceeds that limit.

## Failure handling

| Provider outcome | Dex outcome |
| --- | --- |
| Documented success response with valid JSON shape | Happy-path branch |
| 404 for a single-resource read | Optional `notFound` branch |
| 400, 401, 403, or another definite 4xx | Optional `providerRejected` branch |
| 408, 409, 429, 5xx, or transport interruption | Retry until the Step budget expires |
| Malformed, incomplete, or oversized success response | Optional `invalidResponse` branch |
| Invalid input or credential | Optional `defect` branch |

Provider error bodies are never copied into Failures. A Flow fails when an optional branch is selected without wiring. The default retry policy is exponential from one second to five minutes, with unlimited attempts within one hour.

## Mocks

The generated `contentfulmock` package provides a default response for each operation. The [real-Dex mock test](go/examples/sample-contentful/flow/workflow_mock_integration_test.go) constructs and runs the first operation this way:

```go
mock := contentfulmock.New(t, ConnectionName)
flow := NewFlow(mock.Connection())
```

The mock defaults represent the same Meridian example resource across both operations.

## Verification limits

The provider HTTP behavior is tested with local `httptest` servers, and the sample's mapping has a deterministic unit test. No live provider account or Dex stack was used during this batch. Real provider authorization, permissions, response variants, and the mock-backed Dex integration remain unverified locally; CI covers the Dex integration.
