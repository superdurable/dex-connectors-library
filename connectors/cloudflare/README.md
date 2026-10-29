# Cloudflare connector

The `cloudflare` Go module exposes typed Dex Steps for Cloudflare. Its [runnable lookup Flow](examples/cloudflare-lookup/README.md) uses the static connection name `cloudflare`.

## Operations

| Factory | Endpoint | Input | Output | Required branch | Optional branches |
| --- | --- | --- | --- | --- | --- |
| NewListZonesStep | GET /client/v4/zones | empty `ListZonesInput` | `ZonesPage.Result` and `Success` | `listed` | `providerRejected`, `invalidResponse`, `defect |
| NewPurgeCacheByURLsStep | POST /client/v4/zones/{zone_id}/purge_cache | `ZoneID` and 1 to 30 absolute HTTP(S) `URLs` | `CachePurge.Success` and purge request ID | `purged` | `notFound`, `providerRejected`, `invalidResponse`, `defect |

`purgeCacheByURLs` sends `{"files":[...]}`. Repeating a purge removes the same URLs, so a retry is safe. Cloudflare confirms acceptance with HTTP 200 and `success: true`; it does not guarantee that content was cached or removed. The list operation returns the first provider page.

## Authorization

Create a custom scoped token at [My Profile > API Tokens](https://dash.cloudflare.com/profile/api-tokens) with Zone Read and Cache Purge permissions on the intended zones. The connector sends `Authorization: Bearer <token>`.

`baseUrl`, `timeout`, and `maxResponseBytes` have manifest defaults. The connection form gives the provider URL, setup path, units, and blank behavior for every field. Secrets are resolved for each call and are never copied into Flow input, Results, Failures, receipts, or logs.

## Failure behavior

| Response | Outcome |
| --- | --- |
| HTTP 200 with a valid provider result | Required success branch |
| HTTP 200 with `success: false` | `providerRejected` |
| HTTP 404 for a named resource | `notFound` when declared, otherwise `providerRejected` |
| Other conclusive 4xx or redirects | `providerRejected` |
| HTTP 408, 409, 429, 5xx, or transport failure | Retry under the default one-hour policy |
| Malformed or oversized response | `invalidResponse` |
| Invalid local input or credentials | `defect` before sending |

HTTP redirects are not followed. Provider error bodies are never copied into failures. Unwired optional branches fail the Flow.

## Mocks

The generated `cloudflaremock` package installs these default answers:

- `listZones`: one active `meridian.example` zone.
- `purgeCacheByURLs`: a successful purge with ID `purge-meridian`.

The [mock-backed example test](examples/cloudflare-lookup/flow/workflow_mock_integration_test.go) runs the first default through a real Dex Worker and Client in CI.

## Official API references

- [https://developers.cloudflare.com/api/resources/zones/methods/list/](https://developers.cloudflare.com/api/resources/zones/methods/list/)
- [https://developers.cloudflare.com/api/resources/cache/methods/purge/](https://developers.cloudflare.com/api/resources/cache/methods/purge/)
- [https://developers.cloudflare.com/fundamentals/api/get-started/create-token/](https://developers.cloudflare.com/fundamentals/api/get-started/create-token/)

## Verification limits

The provider calls have not been exercised with live account credentials. Real Dex execution, release compatibility, and provider authorization remain unverified locally. Unlisted response fields and undocumented status details are not asserted.
