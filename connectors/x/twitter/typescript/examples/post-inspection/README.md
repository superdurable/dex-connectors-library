# X TypeScript post inspection

This runnable Flow calls `getPost` through the static `x-twitter` connection and persists the Post ID and text. Optional branches are unwired and fail the Flow when selected. Configure `access_token` in Dex Web Connections as described in the [connector README](../../README.md).

From `connectors/x/twitter/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- <post-id>`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service.

Run `npm run check` for deterministic package checks. With `DEX_FLOW_SERVICE_ADDRESS` set to the local Dex service, run `npm run test:integration` for the real Worker and Client path.
