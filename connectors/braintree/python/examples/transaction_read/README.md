# Braintree Python transaction lookup

This runnable Flow reads one Braintree transaction with `getTransaction` through the static `braintree` connection and persists its profile. Configure `publicKey` and secret `privateKey` in Dex Web Connections using the [connection guide](../../../README.md). Optional branches are unwired and fail the Flow when selected.

From `connectors/braintree/python`, run `uv run python -m examples.transaction_read.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `BraintreeTransactionReadPython` with a unique Flow ID and input `{"transactionId":"txn-1"}`. Replace the sample IDs with GraphQL transaction ID from your Braintree account.
