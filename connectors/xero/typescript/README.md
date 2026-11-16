# Xero TypeScript connector

`@superdurable/connector-xero` provides six Xero Accounting operations for Dex: list contacts, find a contact by email, get or list invoices, create an invoice, and record a payment. Amounts remain exact decimal strings. The two writes send the Dex-derived Call ID as Xero's `Idempotency-Key`.

Use a Xero Custom Connection or an OAuth 2.0 web app. Configure the named connection in Dex Web using the [Xero authorization guide](../README.md). The generated credential driver obtains and rotates tokens; credentials stay outside Flow state. For an OAuth connection, `organisation` may be the tenant UUID or its name; when blank, the connector uses the only connected organisation.

Version `0.2.0` also implements the `invoiceChanged` poll Trigger on `@superdurable/connectors-sdk` `0.12.0`, which requires `@superdurable/dex` 1.45.4 or newer.

## invoiceChanged Trigger

`invoiceChanged` delivers one `InvoiceChangedEvent` for each sales invoice or bill created or changed in the organisation, at parity with the Go connector: it polls `GET /Invoices` with `If-Modified-Since` five seconds before the newest delivered `UpdatedDateUTC`, ordered by `UpdatedDateUTC`, with the binding's `type` and `statuses` filters. The first poll records a baseline from Xero's `DateTimeUTC`, so existing invoices are never delivered. Event IDs hash the invoice ID and `UpdatedDateUTC`, so a refetch yields the same ID. A 429 for the minute, app-minute, concurrency, or daily limit pauses the connection for Retry-After; a cursor saved for another filter or organisation records a new baseline and logs a WARN record. The cursor document is the Go connector's JSON byte for byte, so Go and TypeScript replicas share a binding. The connection's `pollInterval` is at least 30 seconds and defaults to five minutes, because every poll spends the organisation's daily Xero API limit.

```ts
const connection = newProjectConnection(project, 'xero-invoice-watcher', { logger });
const runner = newProjectInvoiceTriggerRunner(project, connection, {
  invoiceChangedRoutes: [{ bindingName: 'invoice-changes', target: newDexFlowTriggerTarget(client, flow, accept, resolveFlowId, mapToFlowInput) }],
});
await runner.run(newTriggerContext(signal));
```

`newProjectInvoiceTriggerRunner` loads each binding's saved filter (an unsaved binding receives every invoice and bill) and records each change in the binding's project Trigger inbox before the cursor commits in its project poll cursor. `newInvoiceTriggerRunner` takes routes, configurations, and cursor stores directly, and `newInvoiceChangedTrigger` keeps the cursor in memory, so it records a new baseline on every start. Declare the binding with `defineInvoiceChangedTriggerBinding` and the `invoiceChangeFilter` unit. The [invoice change example](examples/invoice-changed/README.md) starts one Flow per change and reads the invoice back with `newGetInvoiceStep`. `test/fixtures/go-invoice-change-cases.json`, recorded from the Go fetcher against its invoice fake, is replayed through the TypeScript fetcher and the TypeScript port of the fake, comparing every request, cursor token, event ID, and stored payload.

## Examples

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

The port's Go-recorded provider replay is checked with `make test-connector-conformance CONNECTOR_DIRECTORY=connectors/xero` from the repository root. Live Xero authorization, tenant selection, provider calls, and live invoice polling have not been verified.
