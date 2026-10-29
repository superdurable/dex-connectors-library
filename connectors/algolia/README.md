# Algolia connector

Algolia index search and repeat-safe record replacement for Dex. The connector uses the public Search REST API with an application ID and an API key in headers.

| Operation | Endpoint | Happy branch |
| --- | --- | --- |
| `search` | `POST /1/indexes/{indexName}/query` | `searched` with record IDs and optional names |
| `saveObject` | `PUT /1/indexes/{indexName}/{objectID}` | `saved` with a queued task ID |

`saveObject` replaces the complete record with the supplied absolute attributes. The explicit `objectID` makes retrying safe for that record; omitted old attributes are removed. The HTTP 200 response means an indexing task was queued, not that the record is searchable yet. `search` returns only `objectID` and optional `name` from each hit to avoid leaking arbitrary record fields into Flow state.

## Authorization

Sign in to the [Algolia API Keys page](https://www.algolia.com/account/api-keys/all) under **Settings > Team and Access > API Keys**. Copy your uppercase alphanumeric **Application ID** into `application_id`. Open [Restricted API Keys](https://www.algolia.com/account/api-keys/restricted), choose **Create API Key**, grant `search` and `addObject` ACLs for the intended index, and paste its secret key into `api_key`. Blank fields prevent requests. The key travels only in `x-algolia-api-key`; the application ID travels in `x-algolia-application-id` and the public host.

The displayed `baseUrl` default is a host template: the connector changes `https://algolia.net/1` to `https://<application_id>.algolia.net/1` before a public call. Override it only for a compatible gateway. Leave `timeout` and `maxResponseBytes` blank for their displayed defaults, or override for a slower network or larger search response.

## Failure behavior

| Condition | Route |
| --- | --- |
| Valid search or queued replacement | `searched` or `saved` |
| Definite provider rejection | `providerRejected` |
| Malformed or oversized response | `invalidResponse` |
| Invalid input or missing credentials | `defect` |
| Transport failure, 408, 409, 429, or 5xx | Retry with Dex's default one-hour budget |

Only the happy branch is wired in the [index search example](examples/index-search). Optional branches fail its Flow. There is no `uncertain` branch because full replacement by `objectID` is repeat-safe.

## Mocks

The generated `algoliamock` defaults return one Meridian notebook hit from `search` and a queued indexing task for the same `product-1` ID from `saveObject`. The example's mock-backed real-Dex test checks that its search reaches the generated mock.

## Sources and verification

- [Search an index](https://www.algolia.com/doc/rest-api/search/search-single-index) documents POST, the headers, request `params`, and HTTP 200 response.
- [Add or replace a record](https://www.algolia.com/doc/rest-api/search/add-or-update-object) documents PUT, `objectID`, the `addObject` ACL, task ID, and queued indexing.
- [Create an API key](https://www.algolia.com/doc/libraries/scala/v2/methods/search/add-api-key) documents ACLs; [Algolia's dashboard guide](https://www.algolia.com/blog/algolia/zero-to-storefront-minutes) locates Application ID and API Keys.

Live Algolia authorization, indexing, and search responses remain unverified. CI runs the mock-backed real-Dex test.
