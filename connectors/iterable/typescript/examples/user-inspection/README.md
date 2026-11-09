# Iterable TypeScript user inspection

This runnable Flow calls `getUser` through the static `iterable` connection and persists the selected user identity. Optional branches are unwired and fail the Flow when selected. Configure `api_key` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/iterable/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- <email>`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service.
