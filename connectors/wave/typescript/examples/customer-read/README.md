# Wave TypeScript customer lookup

This runnable Flow reads one Wave customer with `getCustomer` through the static `wave` connection and persists its profile. Configure secret `accessToken` in Dex Web Connections using the [connection guide](../../../README.md). Optional branches are unwired and fail the Flow when selected.

From `connectors/wave/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- business-1 customer-1`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Replace the sample IDs with IDs from your Wave business.
