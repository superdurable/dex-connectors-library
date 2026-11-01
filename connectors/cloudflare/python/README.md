# Cloudflare connector for Python

Install with `pip install dex-connector-cloudflare==0.1.0`. The package requires
`dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and implements the
manifest operations `listZones` (Query) and `purgeCacheByURLs` (Mutation) with
the same branches, defaults, and failure classification as the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `listZones` | `new_list_zones_step` | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| `purgeCacheByURLs` | `new_purge_cache_by_urls_step` | `purged` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

`listZones` reads the first page of `/zones` and requires `success: true` with
a `result` list. `purgeCacheByURLs` posts 1 through 30 absolute HTTP or HTTPS
URLs without user information to `/zones/<zoneId>/purge_cache`; repeating the
same purge has the same effect, so retries are safe. Acceptance does not
guarantee a cache miss.

Both operations retry on 408, 409, 429, 5xx, and transport failures. A
`success: false` envelope selects `providerRejected`; HTTP 404 selects
`notFound` for purges; other statuses select `providerRejected` with
authentication (401), authorization (403), not-found (404), or validation
(400, 422) kinds.

## Authorization

Create a Dex Web connection named `cloudflare` as described in the
[connector README](../README.md#authorization). The API token travels only in
the `Authorization: Bearer` header and is resolved for every attempt.

## Example

[`examples/cloudflare_lookup`](examples/cloudflare_lookup/flow.py) lists the
first zone page. Run the Worker with
`uv run python -m examples.cloudflare_lookup.worker` after `dexcli dev` has
loaded a project containing the `cloudflare` connection.

## Tests

`cloudflaremock.Mock` scripts both operations and defaults to the manifest
answers. Run `uv run pytest tests` for provider parity and manifest contract
checks.
