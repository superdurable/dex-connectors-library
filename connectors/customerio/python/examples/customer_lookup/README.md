# Customer.io Python customer lookup

This runnable Flow calls `getCustomersByEmail` through the static `customerio` connection, persists matching profile IDs, and completes. Optional branches are unwired and fail the Flow when selected. Configure secret `privateKey` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/customerio/python`, run `uv run python -m examples.customer_lookup.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `CustomerioCustomerLookupPython` with a unique Flow ID and input `{"email":"person@example.com"}`. Replace the sample with a customer email from the Customer.io People page.
