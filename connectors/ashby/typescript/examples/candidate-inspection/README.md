# Ashby TypeScript candidate inspection

This runnable Flow calls `getCandidate` through the static `ashby` connection and persists the selected candidate profile. Optional branches are unwired and fail the Flow when selected. Configure `api_key` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/ashby/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- <candidate-id>`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service.
