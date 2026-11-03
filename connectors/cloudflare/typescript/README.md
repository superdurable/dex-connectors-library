# Cloudflare TypeScript connector

`@superdurable/connector-cloudflare` supplies `listZones` and
`purgeCacheByURLs` to Dex TypeScript Flows. Its Step factories, model validators,
connection contracts, and mocks are generated from the connector manifest and
Go model types. Run `go run ./cmd/connectorctl generate --lang typescript
connectors/cloudflare/connector.yaml` after changing either source.

## Install and connect

```sh
npm install @superdurable/connector-cloudflare @superdurable/connectors-sdk @superdurable/dex
```

Use Node.js 22 or 24 and Dex TypeScript SDK 1.28.0 or newer. Create a scoped
Cloudflare token from [My Profile > API Tokens](https://dash.cloudflare.com/profile/api-tokens)
with Zone Read and Cache Purge permissions for the intended zones. Store the
secret as `api_token` in a private Dex Web Connection. The token never belongs
in Flow input, Attributes, Results, Streams, or logs.

At Worker startup, load the project with `loadFromEnvironment()` from
`@superdurable/connectors-sdk/projectconfig` and use the static
`ConnectionName` with `newProjectConnection`. The [runnable lookup example](examples/cloudflare-lookup/)
contains both Worker and starter. The client resolves current credentials for
each request; `baseUrl`, `timeoutMs`, and `maxResponseBytes` use manifest
defaults when omitted.

## Operations

| Step factory | Request | Happy branch |
| --- | --- | --- |
| `newListZonesStep` | `GET /client/v4/zones` | `listed` |
| `newPurgeCacheByURLsStep` | `POST /client/v4/zones/{zoneId}/purge_cache` | `purged` |

All other declared branches are optional; an unwired optional branch fails
the Flow. `purgeCacheByURLs` accepts one zone and 1–30 absolute HTTP or HTTPS
URLs. It sends `{"files":[...]}` and derives a stable idempotency identity from
the Dex Call ID; Cloudflare receives no idempotency header because repeating a
purge has the same cache removal effect. HTTP 408, 409, 429, 5xx, and transport
failures retry. Invalid input or credentials select `defect`; malformed or
oversized results select `invalidResponse`. Provider error bodies are excluded
from failures.

## Verify

```sh
npm ci
npm run check
```

With `dexcli dev` running on a port in 21200–21299 and
`DEX_FLOW_SERVICE_ADDRESS` set, run `npm run test:integration`. This runs the
checked-in Flow through a real Dex Worker and Client against a local Cloudflare
HTTP fixture, including an unwired authorization failure. Shared Go recordings
cover both operations' requests, outputs, branches, retries, and mutation
identity. Live Cloudflare authorization and API behavior remain unverified.
