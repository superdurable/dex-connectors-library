# Razorpay TypeScript payment inspection

This runnable Flow calls `getPayment` through the static `razorpay` connection and persists the selected payment status. Optional branches are unwired and fail the Flow when selected. Configure `key_id` and `key_secret` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/razorpay/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- <payment-id>`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service.
