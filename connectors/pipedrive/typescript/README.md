# Pipedrive TypeScript connector

The `@superdurable/connector-pipedrive` package binds six Pipedrive API v2 operations to Dex Steps: `searchObjects`, `getObject`, `listObjects`, `upsertObject`, `createObject`, and `updateObject`. It has no Trigger. Its manifest version is `v0.1.0` for TypeScript; the Go connector has an independent release version.

Use `newProjectConnection(project, ConnectionName)` to bind a Dex Web connection. The generated private and shared credential decoders select a Personal API token or Pipedrive OAuth app; the generated manifest refresh declaration handles OAuth access-token renewal. The client sends OAuth requests to Pipedrive's returned `api_domain`. It never puts credentials in Flow input, Attributes, Results, receipts, or logs.

`createObject` and the create path of `upsertObject` use the SDK single-dispatch fence. A later attempt reads back a known provider ID or exact identity before concluding that a prior create succeeded. Ambiguous creation can select `uncertain`, requiring operator reconciliation. See the [list-deals example](examples/list-deals/README.md) for a runnable Worker and starter.

Run `npm ci && npm run check` in this directory. The provider replay suite compares 76 recorded Go scenarios. The real Dex integration test uses a local Pipedrive stand-in; set `DEX_FLOW_SERVICE_ADDRESS` to a running `dexcli dev` server and run `npm run test:integration`.

Live Pipedrive authorization, OAuth callback and refresh, API-domain rotation, response shapes, rate-limit behavior, and search-index lag after create remain unverified.
