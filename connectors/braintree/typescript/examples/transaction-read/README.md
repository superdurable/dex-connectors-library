# Braintree TypeScript transaction lookup

This runnable Flow reads one Braintree transaction with `getTransaction` through the static `braintree` connection and persists its profile. Configure `publicKey` and secret `privateKey` in Dex Web Connections using the [connection guide](../../../README.md). Optional branches are unwired and fail the Flow when selected.

From `connectors/braintree/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- txn-1`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Replace the sample IDs with GraphQL transaction ID from your Braintree account.
