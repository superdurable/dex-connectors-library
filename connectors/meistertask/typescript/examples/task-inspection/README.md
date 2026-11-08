# MeisterTask TypeScript task inspection

This runnable Flow calls `getTask` through the static `meistertask` connection and persists the task summary. Optional branches are unwired and fail the Flow when selected. Configure `api_token` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/meistertask/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- <task-id>`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service.
