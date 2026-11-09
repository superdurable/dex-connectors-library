# Keap TypeScript contact lookup

This runnable Flow reads one Keap contact with `getContact` through the static `keap` connection and persists its profile. Configure secret `personalAccessToken` in Dex Web Connections using the [connection guide](../../../README.md). Optional branches are unwired and fail the Flow when selected.

From `connectors/keap/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- 123`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Replace the sample ID with the ID of a contact in your Keap CRM.
