# Wrike TypeScript task inspection

This runnable Flow calls `getTask` through the static `wrike` connection and persists the selected task status. Optional branches are unwired and fail the Flow when selected. Configure `api_token` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/wrike/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- <task-id>`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service.
