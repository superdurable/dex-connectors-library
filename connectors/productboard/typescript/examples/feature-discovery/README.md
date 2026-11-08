# Productboard TypeScript feature discovery

This runnable Flow calls `listFeatures` through the static `productboard` connection and persists the returned feature page. Optional branches are unwired and fail the Flow when selected. Configure `api_token` in Dex Web Connections as described in the [connector README](../../README.md).

From `connectors/productboard/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start` for the first page or `npm run example:start -- <next-cursor>` for another page. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service.

Run `npm run check` for deterministic package checks. With `DEX_FLOW_SERVICE_ADDRESS` set to the local Dex service, run `npm run test:integration` for the real Worker and Client path.
