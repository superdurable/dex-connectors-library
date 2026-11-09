# Amplitude TypeScript user search

This runnable Flow calls `searchUsers` through the static `amplitude` connection and persists the returned user IDs. Optional branches are unwired and fail the Flow when selected. Configure `apiKey` and `secretKey` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/amplitude/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- example-user`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Replace the sample with a user ID or prefix from Amplitude User Lookup.
