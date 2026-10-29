# Unsplash connector

Read-only Unsplash photo operations for Dex. The connector uses the public JSON API with an application Access Key in `Authorization: Client-ID`.

| Operation | Endpoint | Happy branch |
| --- | --- | --- |
| `searchPhotos` | `GET /search/photos` | `searched` with a possibly empty page |
| `getPhoto` | `GET /photos/{id}` | `found` with one photo |

Each photo result includes Unsplash-hosted image URLs and attribution URLs. Applications must hotlink `regularUrl` and attribute the photographer and Unsplash; do not download and serve the image from your own host. A download-like action requires Unsplash's separate download tracking endpoint, which this connector does not expose.

## Authorization

Sign in at [Unsplash Your apps](https://unsplash.com/oauth/applications), choose **New Application**, accept the API guidelines, enter an application name and description, and copy the displayed **Access Key** string. Paste it into Dex Web Connections' secret `access_key` field. Blank prevents requests. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults. Override them only for a compatible gateway, a slower network, or larger photo pages. The key is never placed in a URL.

## Failure behavior

| Condition | Route |
| --- | --- |
| Valid search or photo lookup | `searched` or `found` |
| Missing photo | `notFound` |
| Definite provider rejection | `providerRejected` |
| Malformed or oversized response | `invalidResponse` |
| Invalid input or missing credentials | `defect` |
| Transport failure, 408, 429, or 5xx | Retry with Dex's default one-hour budget |

Only the happy branch is wired in the [photo search example](examples/photo-search); optional branches fail its Flow.

## Mocks

The generated `unsplashmock` defaults return one office photo for `searchPhotos` and that same photo by ID for `getPhoto`. The example's mock-backed real-Dex test checks that a search reaches the generated mock with its request input.

## Sources and verification

- [Unsplash API documentation](https://unsplash.com/documentation) documents application registration, `Client-ID` authentication, both endpoints, inputs, response fields, and HTTP statuses.
- [Unsplash API guidelines](https://help.unsplash.com/en/articles/2511245-unsplash-api-guidelines) document image hotlinking, attribution, and download tracking.

Live Unsplash authorization and provider responses remain unverified. CI runs the mock-backed real-Dex test.
