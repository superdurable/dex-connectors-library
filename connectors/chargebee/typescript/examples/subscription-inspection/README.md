# Chargebee TypeScript subscription inspection

This runnable Flow calls `getSubscription` through the static `chargebee` connection and persists the selected subscription status. Optional branches are unwired and fail the Flow when selected. Configure `api_key` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/chargebee/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- <subscription-id>`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service.
