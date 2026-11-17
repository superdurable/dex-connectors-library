# Cloudflare connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/cloudflare/go@v0.3.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/cloudflare`.

The `cloudflare` Go module exposes typed Dex Steps for Cloudflare. Its [runnable lookup Flow](go/examples/cloudflare-lookup/README.md) uses the static connection name `cloudflare`.

## Operations

| Factory | Endpoint | Input | Output | Required branch | Optional branches |
| --- | --- | --- | --- | --- | --- |
| NewListZonesStep | GET /client/v4/zones | empty `ListZonesInput` | `ZonesPage.Result` and `Success` | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| NewPurgeCacheByURLsStep | POST /client/v4/zones/{zone_id}/purge_cache | `ZoneID` and 1 to 30 absolute HTTP(S) `URLs` | `CachePurge.Success` and purge request ID | `purged` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| NewPurgeEntireCacheStep (Go) | POST /client/v4/zones/{zone_id}/purge_cache | `ZoneID` | `CachePurge` | `purged` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| NewListDNSRecordsStep (Go) | GET /client/v4/zones/{zone_id}/dns_records | `ZoneID`, optional page and page size | `DNSRecordsPage` | `listed` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| NewGetDNSRecordStep (Go) | GET /client/v4/zones/{zone_id}/dns_records/{record_id} | zone and record IDs | `DNSRecord` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| NewUpdateDNSRecordStep (Go) | PUT /client/v4/zones/{zone_id}/dns_records/{record_id} | zone and record IDs, type, name, content, TTL, proxy flag | `DNSRecord` | `updated` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| NewDeleteDNSRecordStep (Go) | DELETE /client/v4/zones/{zone_id}/dns_records/{record_id} | zone and record IDs | `DeletedDNSRecord` | `deleted` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

`purgeCacheByURLs` sends `{"files":[...]}`. `purgeEntireCache` sends `{"purge_everything":true}`. Repeating either purge removes the same cache entries, so retries are safe. Cloudflare confirms acceptance with HTTP 200 and `success: true`; it does not guarantee that content was cached or removed. `updateDNSRecord` overwrites an A, AAAA, CNAME, or TXT record with an absolute value; `deleteDNSRecord` removes one record. Both are repeat safe. DNS creation is omitted because Cloudflare documents no idempotency key for POST and an ambiguous create can leave duplicate records.

The Python package is [`dex-connector-cloudflare`](python/README.md), imported as
`dex_connectors.cloudflare`. The [TypeScript package](typescript/README.md) is
`@superdurable/connector-cloudflare`; its runnable example lists the first zone page.

## Authorization

Create a custom scoped token at [My Profile > API Tokens](https://dash.cloudflare.com/profile/api-tokens) with Zone Read, Cache Purge, and DNS Read/Edit permissions on the intended zones. The connector sends `Authorization: Bearer <token>`.

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
- `purgeEntireCache`: a zone-wide purge acceptance.
- `listDNSRecords`, `getDNSRecord`, `updateDNSRecord`, and `deleteDNSRecord`: sample Meridian DNS records.

The [mock-backed example test](go/examples/cloudflare-lookup/flow/workflow_mock_integration_test.go) runs the first default through a real Dex Worker and Client in CI.

## Official API references

- [https://developers.cloudflare.com/api/resources/zones/methods/list/](https://developers.cloudflare.com/api/resources/zones/methods/list/)
- [https://developers.cloudflare.com/api/resources/cache/methods/purge/](https://developers.cloudflare.com/api/resources/cache/methods/purge/)
- [DNS record list](https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/list/)
- [DNS record get](https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/get/)
- [DNS record update](https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/update/)
- [DNS record delete](https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/delete/)
- [https://developers.cloudflare.com/fundamentals/api/get-started/create-token/](https://developers.cloudflare.com/fundamentals/api/get-started/create-token/)

## Verification limits

The provider calls have not been exercised with live account credentials. Provider authorization and live responses remain unverified. Unlisted response fields and undocumented status details are not asserted.
