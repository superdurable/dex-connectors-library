# PayPal connector for Dex TypeScript

This package implements the manifest's `getOrder` and `listTransactions` Query operations. `PayPalClient` resolves the connection's client ID and secret at invocation time, exchanges them for a bearer token, and sends bounded, no-redirect HTTP requests through the Connector SDK. The client keeps a token only until its safe expiry and clears it after a read returns HTTP 401.

Use `newProjectConnection(project, 'paypal')` when loading a Dex project, or bind a `PayPalClient` with `newConnection(client, { provider: 'paypal', name: 'paypal' })`. Build a Flow with the generated `newGetOrderStep` or `newListTransactionsStep` factory. The [runnable resource-read example](examples/resource-read/README.md) shows the `getOrder` path, Worker entrypoint, and Dex integration test.

Run `npm ci`, `npm run check`, and `DEX_FLOW_SERVICE_ADDRESS=<dev-address> npm run test:integration` from this directory. The conformance tests replay Go-recorded provider exchanges for both operations. Live PayPal provider behavior remains unverified.
