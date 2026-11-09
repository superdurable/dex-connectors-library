# Paddle Python transaction inspection

This runnable Flow calls `getTransaction` through the static `paddle` connection, persists the selected transaction status, and completes. Optional branches are unwired and fail the Flow when selected. Configure `api_key` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/paddle/python`, run `uv run python -m examples.transaction_inspection.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `PaddleTransactionInspectionPython` with a unique Flow ID and input `{"transactionId":"txn_01hv8wptq8987qeep44cyrewp9"}`. Replace the sample transaction ID with one returned by Paddle.
