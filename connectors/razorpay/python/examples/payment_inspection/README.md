# Razorpay Python payment inspection

This runnable Flow calls `getPayment` through the static `razorpay` connection, persists the selected payment status, and completes. Optional branches are unwired and fail the Flow when selected. Configure `key_id` and `key_secret` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/razorpay/python`, run `uv run python -m examples.payment_inspection.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `RazorpayPaymentInspectionPython` with a unique Flow ID and input `{"paymentId":"pay_DG4ZdRK8ZnXC3k"}`. Replace the sample payment ID with one returned by Razorpay.
