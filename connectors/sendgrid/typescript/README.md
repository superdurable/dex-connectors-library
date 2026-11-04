# SendGrid TypeScript connector

`@superdurable/connector-sendgrid` supplies `getStats` and `listTemplates` Queries for Dex TypeScript Flows. Operation Step factories, models, connection bindings, and mocks come from [`connector.yaml`](../connector.yaml) and the Go operation types. The hand-written client follows the Go provider request and failure classifications. Use Node.js 22 or 24 and Dex TypeScript SDK 1.28.0 or later.

## Connect

Install `@superdurable/connector-sendgrid`, `@superdurable/connectors-sdk`, and `@superdurable/dex`. In [SendGrid > Settings > API Keys](https://app.sendgrid.com/settings/api_keys), create or select a key with permission to read global statistics and transactional templates. Store it in the secret `api_key` field in Dex Web Connections. The example uses the static connection name `sendgrid`; its Worker calls `newProjectConnection` after `loadFromEnvironment()`.

The manifest supplies `baseUrl`, `timeout`, and `maxResponseBytes` defaults. Override the base URL only for an EU subuser or a trusted compatible gateway. Credentials are resolved anew for each attempt and never enter Flow state or results.

| Factory | Provider request | Happy branch |
| --- | --- | --- |
| `newGetStatsStep` | `GET /v3/stats` with an inclusive date range | `found` |
| `newListTemplatesStep` | `GET /v3/templates` with generation and page selection | `listed` |

`listTemplates` defaults to dynamic templates and 100 per page; a supplied size must be 1–200. Pass `nextPageToken` to the next call's `pageToken`. The exact provider meaning of `_metadata.count` remains unverified. All nonhappy branches are optional; an unwired one fails the Flow. Invalid input or credentials select `defect`. Definite provider rejections select `providerRejected`. A malformed or oversized successful response selects `invalidResponse`. Transport failures, 408, 429, and 5xx retry.

## Run and verify

The [statistics example](examples/stats/README.md) is a runnable Worker, starter, and real Dex integration test. From this package, run `npm ci` and `npm run check`. With `dexcli dev -dex-port 27110 -web-port 27111 -open=false` running, set `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:27110` and run `npm run test:integration`. The local test binds its provider stand-in to a free port. Shared Go-recorded scenarios replay both operations' requests, branches, outputs, and retries.

Live SendGrid authorization, Dex Web Connections form, regional endpoint, and provider responses remain unverified.
