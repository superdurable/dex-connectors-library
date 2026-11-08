# Lucid TypeScript document discovery

This runnable Flow calls `listDocuments` through the static `lucid` connection and persists the returned document page. Optional branches are unwired and fail the Flow when selected. Configure `api_key` in Dex Web Connections as described in the [connector README](../../README.md).

From `connectors/lucid/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start` for the first page or `npm run example:start -- <next-page-token>` for another page. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service.

Run `npm run check` for deterministic package checks. With `DEX_FLOW_SERVICE_ADDRESS` set to the local Dex service, run `npm run test:integration` for the real Worker and Client path.
