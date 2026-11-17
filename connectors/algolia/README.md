# Algolia connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/algolia/go@v0.4.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/algolia`.

Algolia index search, record management, and index listing for Dex. The connector uses the public Search REST API with an application ID and an API key in headers.

| Operation | Endpoint | Happy branch |
| --- | --- | --- |
| `search` | `POST /1/indexes/{indexName}/query` | `searched` with record IDs and optional names |
| `getObject` | `GET /1/indexes/{indexName}/{objectID}` | `found` with the record ID and optional name |
| `saveObject` | `PUT /1/indexes/{indexName}/{objectID}` | `saved` with a queued task ID |
| `partialUpdateObject` | `POST /1/indexes/{indexName}/{objectID}/partial` | `updated` with a queued task ID |
| `deleteObject` | `DELETE /1/indexes/{indexName}/{objectID}` | `deleted` with a queued task ID |
| `listIndices` | `GET /1/indexes` | `listed` with index names and record counts |

`saveObject` replaces the complete record with the supplied absolute attributes. `partialUpdateObject` sets only supplied string attributes and requires an existing record; it does not expose increment actions. The explicit `objectID` makes updates and deletion repeat-safe for that record. HTTP 200 on a write means an indexing task was queued, not that it has completed. `search` and `getObject` return only `objectID` and optional `name` to keep arbitrary record fields out of Flow state. For `search`, an omitted or blank `query` matches all records, and an omitted `hitsPerPage` uses Algolia's default. For `listIndices`, `page` is zero-based and `hitsPerPage` may be 1 to 1000; zero uses Algolia's default.

The Python package is [`dex-connector-algolia`](python/README.md), imported as
`dex_connectors.algolia`.

The TypeScript package is [`@superdurable/connector-algolia`](typescript/README.md), with [runnable search and replacement Flows](typescript/examples/index-search).

## Authorization

Sign in to the [Algolia API Keys page](https://www.algolia.com/account/api-keys/all) under **Settings > Team and Access > API Keys**. Copy your uppercase alphanumeric **Application ID** into `application_id`. Open [Restricted API Keys](https://www.algolia.com/account/api-keys/restricted), choose **Create API Key**, grant `search`, `addObject`, `deleteObject`, and `listIndexes` ACLs for the operations and indices you use, and paste its secret key into `api_key`. Blank fields prevent requests. The key travels only in `x-algolia-api-key`; the application ID travels in `x-algolia-application-id` and the public host.

The displayed `baseUrl` default is a host template: the connector changes `https://algolia.net/1` to `https://<application_id>.algolia.net/1` before a public call. Override it only for a compatible gateway. Leave `timeout` and `maxResponseBytes` blank for their displayed defaults, or override for a slower network or larger search response.

## Failure behavior

| Condition | Route |
| --- | --- |
| Valid read, listing, or queued write | `searched`, `found`, `listed`, `saved`, `updated`, or `deleted` |
| Record absent on `getObject` | `missing` |
| Definite provider rejection | `providerRejected` |
| Malformed or oversized response | `invalidResponse` |
| Invalid input or missing credentials | `defect` |
| Transport failure, 408, 409, 429, or 5xx | Retry with Dex's default one-hour budget |

Only happy branches are wired in the [index search example](go/examples/index-search). Optional branches fail its Flow. There is no `uncertain` branch because writes use an absolute value or deletion by stable `objectID`.

## Mocks

The generated `algoliamock` defaults return one Meridian notebook hit from `search` and a queued indexing task for the same `product-1` ID from `saveObject`. The example's mock-backed real-Dex test checks that its search reaches the generated mock.

## Sources and verification

- [Search an index](https://www.algolia.com/doc/rest-api/search/search-single-index) documents POST, the headers, request `params`, and HTTP 200 response.
- [Add or replace a record](https://www.algolia.com/doc/rest-api/search/add-or-update-object) documents PUT, `objectID`, the `addObject` ACL, task ID, and queued indexing.
- [Retrieve a record](https://www.algolia.com/doc/rest-api/search/get-object), [add or update attributes](https://www.algolia.com/doc/rest-api/search/partial-update-object), and [delete a record](https://www.algolia.com/doc/rest-api/search/delete-object) document the record endpoints.
- [List indices](https://www.algolia.com/doc/rest-api/search/list-indices) documents the paginated index endpoint.
- [Create an API key](https://www.algolia.com/doc/libraries/scala/v2/methods/search/add-api-key) documents ACLs; [Algolia's dashboard guide](https://www.algolia.com/blog/algolia/zero-to-storefront-minutes) locates Application ID and API Keys.

Live Algolia authorization, indexing, and search responses remain unverified. CI runs the mock-backed real-Dex test.
