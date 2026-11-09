# Buffer TypeScript channel lookup

This runnable Flow reads one Buffer channel with `getChannel` through the static `buffer` connection and persists its profile. Configure secret `apiKey` in Dex Web Connections using the [connection guide](../../../README.md). Optional branches are unwired and fail the Flow when selected.

From `connectors/buffer/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- channel-1`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Replace the sample IDs with Buffer channel ID from your Buffer account.
