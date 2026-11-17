# Contentful connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/contentful/go@v0.3.0` after release.

Moved from `github.com/superdurable/dex-connectors-library/connectors/contentful`.

The Go connector exposes `getEntry`, `listEntries`, `getAsset`, `listAssets`, `getContentType`, `listContentTypes`, `getSpace`, and `listLocales`. The runnable [sample Flows](go/examples/sample-contentful/README.md) use `getEntry` and `getAsset` with the static `contentful` connection. Each operation uses its generated typed Step factory. Python and TypeScript retain their existing `getEntry` and `listEntries` scope.

The Python package is [`dex-connector-contentful`](python/README.md), imported as
`dex_connectors.contentful`.

The TypeScript package is [`@superdurable/connector-contentful`](typescript/README.md).
Its [sample application](typescript/examples/sample-contentful/README.md) runs
both `getEntry` and `listEntries` through the static `contentful` connection.

## Provider contract

The Content Delivery API is read-only and returns published content. The token travels in the Authorization header, never in a query parameter. Asset operations return metadata and file URLs, without downloading binary content. Collection operations return one bounded page with Contentful's `total`, `skip`, and `limit` fields; follow those fields to retrieve another page.

Official documentation used for the endpoint, authentication, and response contract:

- [https://www.contentful.com/developers/docs/references/content-delivery-api/overview/](https://www.contentful.com/developers/docs/references/content-delivery-api/overview/)
- [https://www.contentful.com/developers/docs/references/content-delivery-api/entries/get-an-entry/](https://www.contentful.com/developers/docs/references/content-delivery-api/entries/get-an-entry/)
- [https://www.contentful.com/developers/docs/references/content-delivery-api/entries/get-all-entries/](https://www.contentful.com/developers/docs/references/content-delivery-api/entries/get-all-entries/)
- [Get an asset](https://www.contentful.com/developers/docs/references/content-delivery-api/assets/get-an-asset/), [get all assets](https://www.contentful.com/developers/docs/references/content-delivery-api/assets/get-all-assets/)
- [Get a content type](https://www.contentful.com/developers/docs/references/content-delivery-api/content-types/get-a-content-type/), [get the content model](https://www.contentful.com/developers/docs/references/content-delivery-api/content-types/get-the-content-model-of-a-space/)
- [Get a space](https://www.contentful.com/developers/docs/references/content-delivery-api/spaces/get-a-space/), [get all locales](https://www.contentful.com/developers/docs/references/content-delivery-api/locales/get-all-locales-of-a-space/)

## Authorization and configuration

Start at [https://app.contentful.com](https://app.contentful.com) and follow the authorization guide shown in Dex Web Connections. The `delivery_token` field is secret. It is resolved for each call and sent only in the `Authorization` header. The connector never places it in a URL, Result, receipt, Failure, or log. Leave `timeout` blank for 15 seconds and `maxResponseBytes` blank for 2,097,152 bytes. A larger bounded value can be entered if a real response page exceeds that limit.

For operation IDs, open the selected space in Contentful. Find the space ID at Settings > General settings, and the environment ID under Settings > Environments; use `master` when that is your target environment. Find entry and asset IDs by selecting the published item under Content or Media, and content type IDs under Content model. IDs are non-secret alphanumeric strings; blank IDs are invalid. For collection Queries, set `limit` from 1 to 1000 and `skip` to a zero-based offset; blank or zero values use Contentful's default page settings.

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

The Go provider behavior is tested with local `httptest` servers. The TypeScript
package replays Go-recorded HTTP scenarios and runs both operations through a
real local Dex Worker and Client with a loopback provider stand-in. Live
Contentful authorization, permissions, and provider responses remain unverified.
