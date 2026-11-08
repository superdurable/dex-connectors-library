# Recurly TypeScript account inspection

This runnable Flow calls `getAccount` through the static `recurly` connection and persists the selected account identity. Optional branches are unwired and fail the Flow when selected. Configure `api_key` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/recurly/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- <account-id>`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service.
