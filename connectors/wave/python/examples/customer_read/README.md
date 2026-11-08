# Wave Python customer lookup

This runnable Flow reads one Wave customer with `getCustomer` through the static `wave` connection and persists its profile. Configure secret `accessToken` in Dex Web Connections using the [connection guide](../../../README.md). Optional branches are unwired and fail the Flow when selected.

From `connectors/wave/python`, run `uv run python -m examples.customer_read.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `WaveCustomerReadPython` with a unique Flow ID and input `{"businessId":"business-1","customerId":"customer-1"}`. Replace the sample IDs with IDs from your Wave business.
