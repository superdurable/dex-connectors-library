# Segment TypeScript source inspection

This runnable Flow calls `getSource` through the static `segment` connection and persists the selected Source identity. Optional branches are unwired and fail the Flow when selected. Configure `api_key` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/twilio/segment/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- <source-id>`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service.
