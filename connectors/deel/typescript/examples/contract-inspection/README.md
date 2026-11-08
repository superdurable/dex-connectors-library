# Deel TypeScript contract inspection

This runnable Flow calls `getContract` through the static `deel` connection and persists the selected contract status. Optional branches are unwired and fail the Flow when selected. Configure `api_token` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/deel/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- <contract-id>`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service.
