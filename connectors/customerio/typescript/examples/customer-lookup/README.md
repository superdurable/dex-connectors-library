# Customer.io TypeScript customer lookup

This runnable Flow calls `getCustomersByEmail` through the static `customerio` connection and persists matching profile IDs. Optional branches are unwired and fail the Flow when selected. Configure secret `privateKey` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/customerio/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- person@example.com`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Replace the sample with a customer email from the Customer.io People page.
