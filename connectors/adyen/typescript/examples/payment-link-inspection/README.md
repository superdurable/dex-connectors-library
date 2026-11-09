# Adyen TypeScript payment link inspection

This runnable Flow calls `getPaymentLink` through the static `adyen` connection and persists the selected payment link status. Optional branches are unwired and fail the Flow when selected. Configure `api_key` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/adyen/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- <paymentLink-id>`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service.
