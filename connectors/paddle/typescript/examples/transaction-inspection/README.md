# Paddle TypeScript transaction inspection

This runnable Flow calls `getTransaction` through the static `paddle` connection and persists the selected transaction status. Optional branches are unwired and fail the Flow when selected. Configure `api_key` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/paddle/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- <transaction-id>`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service.
