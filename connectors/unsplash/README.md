# Unsplash connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/unsplash/go@v0.3.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/unsplash`.

Public Unsplash photo, collection, user, and topic reads for Dex. The connector uses the public JSON API with an application Access Key in `Authorization: Client-ID`.

| Operation | Endpoint | Happy branch |
| --- | --- | --- |
| `searchPhotos` | `GET /search/photos` | `searched` with a possibly empty page |
| `getPhoto` | `GET /photos/{id}` | `found` with one photo |
| `listPhotos` | `GET /photos` | `listed` with an editorial page |
| `getRandomPhoto` | `GET /photos/random` | `found` with one random photo |
| `searchCollections` | `GET /search/collections` | `searched` with a collection page |
| `listCollections` | `GET /collections` | `listed` with a collection page |
| `getCollection` | `GET /collections/{id}` | `found` with one collection |
| `listCollectionPhotos` | `GET /collections/{id}/photos` | `listed` with a photo page |
| `getUser` | `GET /users/{username}` | `found` with one public profile |
| `listUserPhotos` | `GET /users/{username}/photos` | `listed` with a photo page |
| `searchUsers` | `GET /search/users` | `searched` with a profile page |
| `listTopics` | `GET /topics` | `listed` with a topic page |
| `getTopic` | `GET /topics/{id}` | `found` with one topic |
| `listTopicPhotos` | `GET /topics/{id}/photos` | `listed` with a photo page |

Each photo result includes Unsplash-hosted image URLs and attribution URLs. Applications must hotlink `regularUrl` and attribute the photographer and Unsplash; do not download and serve the image from your own host. A download-like action requires Unsplash's separate download tracking endpoint, which this connector does not expose.

The Python package is [`dex-connector-unsplash`](python/README.md), imported as
`dex_connectors.unsplash`. The TypeScript package is
[`@superdurable/connector-unsplash`](typescript/README.md), with a runnable
[photo search Flow](typescript/examples/photo-search).

## Authorization

Sign in at [Unsplash Your apps](https://unsplash.com/oauth/applications), choose **New Application**, accept the API guidelines, enter an application name and description, and copy the displayed **Access Key** string. Paste it into Dex Web Connections' secret `access_key` field. Blank prevents requests. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults. Override them only for a compatible gateway, a slower network, or larger photo pages. The key is never placed in a URL.

The application key cannot create or modify collections, photos, or profiles,
or access private resources. Unsplash requires a user's OAuth bearer token and
write scopes for those actions; adding them requires a separate authorization
method and connection migration.

## Failure behavior

| Condition | Route |
| --- | --- |
| Valid search or photo lookup | `searched` or `found` |
| Missing photo | `notFound` |
| Definite provider rejection | `providerRejected` |
| Malformed or oversized response | `invalidResponse` |
| Invalid input or missing credentials | `defect` |
| Transport failure, 408, 429, or 5xx | Retry with Dex's default one-hour budget |

Only the happy branch is wired in the [Go photo search example](go/examples/photo-search) and [TypeScript photo search example](typescript/examples/photo-search); optional branches fail each Flow.

## Mocks

The generated `unsplashmock` defaults return representative public resources.
The example's mock-backed real-Dex test checks photo search and collection
browsing through a Worker and Client.

## Sources and verification

- [Unsplash API documentation](https://unsplash.com/documentation) documents application registration, `Client-ID` authentication, public endpoints, inputs, response fields, and HTTP statuses.
- [Unsplash API guidelines](https://help.unsplash.com/en/articles/2511245-unsplash-api-guidelines) document image hotlinking, attribution, and download tracking.

Live Unsplash authorization and provider responses remain unverified. The Go mock-backed Flow test and TypeScript real-Dex stand-in integration exercise the public application paths. Go-recorded conformance scenarios replay in Python and TypeScript.
