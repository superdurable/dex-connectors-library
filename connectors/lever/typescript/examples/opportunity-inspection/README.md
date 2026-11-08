# Lever TypeScript opportunity inspection

This runnable Flow calls `getOpportunity` through the static `lever` connection and persists the selected opportunity stage. Optional branches are unwired and fail the Flow when selected. Configure `api_key` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/lever/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- <opportunity-id>`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service.
