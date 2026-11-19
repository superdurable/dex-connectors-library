# Webflow connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/webflow/go@v0.3.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/webflow`.

This Dex connector exposes `listCollections`, `getCollection`, `listCollectionItems`, `getCollectionItem`, `listLiveCollectionItems`, and `getLiveCollectionItem`. The runnable [sample Flow](go/examples/sample-webflow/flow/workflow.go) uses `listCollections` with the static `webflow` connection. Each operation uses the generated typed Step factory.

The Python package is [`dex-connector-webflow`](python/README.md), imported as
`dex_connectors.webflow`.

The TypeScript package is [`@superdurable/connector-webflow`](typescript/README.md) with a [runnable Dex Flow](typescript/examples/list-items/flow.ts) using the same static `webflow` connection.

## Provider contract

All operations use `CMS:read` scope. A site token is scoped to one site. `siteId`, `collectionId`, and `itemId` are 24-character hexadecimal IDs. Collection details include the field schema; item reads select staged or published content.

Official documentation used for the endpoint, authentication, and response contract:

- [https://developers.webflow.com/data/reference/cms/collection-items/staged-items/list-items](https://developers.webflow.com/data/reference/cms/collection-items/staged-items/list-items)
- [https://developers.webflow.com/data/reference/cms/collection-items/staged-items/get-item](https://developers.webflow.com/data/reference/cms/collection-items/staged-items/get-item)
- [https://developers.webflow.com/data/reference/cms/collections/list](https://developers.webflow.com/data/reference/cms/collections/list)
- [https://developers.webflow.com/data/reference/cms/collections/get](https://developers.webflow.com/data/reference/cms/collections/get)
- [https://developers.webflow.com/data/reference/cms/collection-items/live-items/list-items-live](https://developers.webflow.com/data/reference/cms/collection-items/live-items/list-items-live)
- [https://developers.webflow.com/data/reference/authentication/site-token](https://developers.webflow.com/data/reference/authentication/site-token)

## Authorization and configuration

Start at [https://webflow.com/dashboard](https://webflow.com/dashboard) and follow the authorization guide shown in Dex Web Connections. The `site_token` field is secret. It is resolved for each call and sent only in the `Authorization` header. The connector never places it in a URL, Result, receipt, Failure, or log. Leave `timeout` blank for 15 seconds and `maxResponseBytes` blank for 2,097,152 bytes. A larger bounded value can be entered if a real response page exceeds that limit.

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

The generated `webflowmock` package provides a default response for each operation. The [real-Dex mock test](go/examples/sample-webflow/flow/workflow_mock_integration_test.go) constructs and runs the first operation this way:

```go
mock := webflowmock.New(t, ConnectionName)
flow := NewFlow(mock.Connection())
```

The mock defaults represent one site collection and related items.

## Verification limits

The provider HTTP behavior is tested with local HTTP scenarios, and the sample's mapping has a deterministic unit test. Real provider authorization, permissions, and response variants remain unverified without a site token.
