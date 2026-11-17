# Pipedrive TypeScript connector

The `@superdurable/connector-pipedrive` package binds six Pipedrive API v2 operations to Dex Steps: `searchObjects`, `getObject`, `listObjects`, `upsertObject`, `createObject`, and `updateObject`. It has no Trigger. Its manifest version is `v0.1.1` for TypeScript; the Go connector has an independent release version. It requires `@superdurable/connectors-sdk` 0.12.0 and `@superdurable/dex` 1.45.4 or newer.

Use `newProjectConnection(project, ConnectionName)` to bind a Dex Web connection. The generated private and shared credential decoders select a Personal API token or Pipedrive OAuth app; the generated manifest refresh declaration handles OAuth access-token renewal. The client sends OAuth requests to Pipedrive's returned `api_domain`. It never puts credentials in Flow input, Attributes, Results, receipts, or logs.

`createObject` and the create path of `upsertObject` use the SDK single-dispatch fence. A later attempt reads back a known provider ID or exact identity before concluding that a prior create succeeded. From 0.1.1, an unknown create outcome, such as a 5xx, 409, 402, redirect, unlisted 4xx, or lost response, is resent while possible sends stay below the Step's send cap, and selects `uncertain` at the cap; `upsertObject` searches the identity before every resend. A definite refusal (400, 401, 403, 404, 411, 413, 414, 415, 422, or the daily token budget 429) selects `providerRejected` only on a first send. `updateObject` is repeat-safe and retries any status that does not prove a refusal. See the [list-deals example](examples/list-deals/README.md) for a runnable Worker and starter.

Run `npm ci && npm run check` in this directory. The provider replay suite compares 78 recorded Go scenarios. The real Dex integration test uses a local Pipedrive stand-in; set `DEX_FLOW_SERVICE_ADDRESS` to a running `dexcli dev` server and run `npm run test:integration`.

Live Pipedrive authorization, OAuth callback and refresh, API-domain rotation, response shapes, rate-limit behavior, and search-index lag after create remain unverified.
