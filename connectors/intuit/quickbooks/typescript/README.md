# QuickBooks Online TypeScript connector

`@superdurable/connector-intuit-quickbooks` v0.2.0 supplies seven QuickBooks Online
operations and the `invoiceChanged` poll Trigger for Dex TypeScript Flows. `connector.yaml` generates the models,
validators, Step factories, OAuth refresh declaration, named connection, and
scripted mocks. The package implements the provider client and operation
invocations. Amounts are exact base-ten strings in the public models and are
sent as JSON number literals to QuickBooks.

## Connect

Install the connector, `@superdurable/connectors-sdk` 0.12.0, and
`@superdurable/dex` 1.45.4 or later on Node.js 22 or 24. Configure the
`quickbooks-accounting` connection in Dex Web with an Intuit app and the
QuickBooks Online Accounting API. Follow the authorization guide in the
manifest: add Dex Web's exact redirect URI in the Intuit app, select the
matching Development or Production keys, and authorize the company. Dex
requests `com.intuit.quickbooks.accounting` and `openid`. The access token,
rotating refresh token, client secret, and ID token stay in host-owned
credentials, outside Flow state and output.

At Worker startup, call `loadFromEnvironment()` and bind the static
`ConnectionName` with `newProjectConnection(project, ConnectionName)`. The
[checked-in example](examples/get-invoice/) is the runnable application. The
client reads `environment`, `realmId`, `maxResponseBytes`, and `pollInterval` from
the project connection. If Intuit's ID token names a company, its realm ID must agree with
an explicitly configured `realmId`.

## Operations

| Factory | Provider call | Happy branch |
| --- | --- | --- |
| `newFindCustomerStep` | Query Customer by email or display name | `found` |
| `newCreateCustomerStep` | `POST /customer` | `created` |
| `newCreateInvoiceStep` | `POST /invoice` | `created` |
| `newGetInvoiceStep` | `GET /invoice/{id}` | `found` |
| `newListInvoicesStep` | Query a bounded Invoice page | `listed` |
| `newSendInvoiceStep` | `POST /invoice/{id}/send` | `sent` |
| `newRecordPaymentStep` | `POST /payment` | `recorded` |

All other declared branches are optional; an unwired optional branch fails
the Flow. Mutations use the Dex Call ID as QuickBooks `requestid` across
attempts and send under the SDK single-dispatch fence with the manifest's
`maxSends` (3 unless the application overrides it). An unknown outcome is
resent below the cap and selects `uncertain` at it; 429, 503, and a request
that never left the process are retried without counting the send. An
accepted write with an unusable response is read back by provider ID, and
later attempts read that ID back instead of resending. See
[Duplicate safety](../README.md#duplicate-safety).

## invoiceChanged Trigger

`invoiceChanged` delivers one `InvoiceChangedEvent` per invoice version created or
changed in the connected company: `realmId`, `environment`, `changeKind` (`created`
for SyncToken 0, otherwise `updated`, which includes payments, emails, and voids), and
the polled `invoice`. It polls the invoice query by `MetaData.LastUpdatedTime`, at
most 100 invoices per request, at parity with the Go connector; see the
[connector README](../README.md#invoicechanged-trigger) for the 30-second lookback
window, the 1,000-version bound, and limitations. The first poll records a baseline
and delivers no existing invoice, and deleted invoices are never reported. The
binding has no configuration fields.

- `newProjectInvoiceTriggerRunner(project, connection, { invoiceChangedRoutes })`
  records each change in the binding's project Trigger inbox before its watermark
  commits in the binding's project poll cursor, so a restarted or replaced replica
  neither misses nor repeats a change.
- `newInvoiceTriggerRunner({ connection, invoiceChangedRoutes })` takes targets and
  cursor stores directly; a route without a cursor store keeps it in memory.
- `newInvoiceChangedTrigger(...)` is one Trigger with an in-memory cursor, as Go's
  `NewInvoiceChangedTrigger`; it records a new baseline on every start.
- `defineInvoiceChangedTriggerBinding` declares the binding in a Flow's
  `getConnectorTriggerBindings`.

The `pollInterval` connection setting (at least 5 seconds) sets the delay between
polls, and the client's `logger` option receives the runner's records. A 429 pauses
every binding of the connection for `Retry-After`, or 60 seconds without it. A cursor
for another company or environment, or a QuickBooks validation Fault for the
watermark query, records a new baseline and logs a WARN record; changes in that gap
are not delivered. Delivery is at least once, and each invoice version keeps one
Trigger event ID. Cursor documents match the Go connector's byte for byte, so Go and
TypeScript replicas share one binding. The
[invoice change example](examples/invoice-changed/) wires the project runner to a
Flow that reads each changed invoice back and records whether it is paid.

## Verify

From this package run `npm ci`, `npm run check`, and, with `dexcli dev` and
`DEX_FLOW_SERVICE_ADDRESS` set, `npm run test:integration`. The integration
test uses a loopback QuickBooks stand-in and a real Dex Worker and Client.
The shared Go-recorded conformance fixture covers provider branches, retries,
transport failures, stable request IDs, and accepted-write read-back.
`test/fixtures/go-invoice-change-cases.json`, recorded from the Go fetcher, is
replayed against the same invoice fake and compares every request, cursor token,
event ID, and stored payload. The invoice change example's integration test runs
through a real Dex Worker and Client against the in-process invoice fake. Live
Intuit app authorization, token refresh, realm claim delivery, QuickBooks API
calls, and live invoice polling remain unverified.
