# Xero TypeScript connector

`@superdurable/connector-xero` provides six Xero Accounting operations for Dex: list contacts, find a contact by email, get or list invoices, create an invoice, and record a payment. Amounts remain exact decimal strings. The two writes send the Dex-derived Call ID as Xero's `Idempotency-Key`.

Use a Xero Custom Connection or an OAuth 2.0 web app. Configure the named connection in Dex Web using the [Xero authorization guide](../README.md). The generated credential driver obtains and rotates tokens; credentials stay outside Flow state. For an OAuth connection, `organisation` may be the tenant UUID or its name; when blank, the connector uses the only connected organisation.

The runnable [list contacts example](examples/list-contacts/README.md) uses the static `xero-contacts` connection and includes a real Dex integration test against a local Xero stand-in. Its input contains no credentials. The generated [mock package](src/mock.ts) can exercise branch wiring without Xero.

From this directory:

```sh
npm ci
npm run check
```

Run the integration test with a local `dexcli dev` gRPC address in `DEX_FLOW_SERVICE_ADDRESS`:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:46000 npm run test:integration
```

The port's Go-recorded provider replay is checked with `make test-connector-conformance CONNECTOR_DIRECTORY=connectors/xero` from the repository root. Live Xero authorization, tenant selection, and provider calls have not been verified.
