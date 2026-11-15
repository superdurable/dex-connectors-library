# QuickBooks Online Python connector

`dex-connector-intuit-quickbooks` v0.2.0 provides all seven QuickBooks Online operations for Dex Python through the generated operation-specific Step factories and OAuth project connection, and the `invoiceChanged` poll Trigger. It requires `dex-connectors-sdk` 0.12.0 or newer.

The [invoice lookup example](examples/get_invoice/) is a runnable Flow using the static `quickbooks-invoice-reader` connection. For authorization setup, exact decimal rules, retry and uncertainty behavior, and provider caveats, see the [connector README](../README.md).

## invoiceChanged Trigger

`invoiceChanged` delivers one `InvoiceChangedEvent` per invoice version created or changed in the connected company: `realm_id`, `environment`, `change_kind` (`created` for SyncToken 0, otherwise `updated`, which includes payments, emails, and voids), and the polled `invoice`. It polls `select * from Invoice where MetaData.LastUpdatedTime >= ...` in LastUpdatedTime order, at most 100 invoices per request, at parity with the Go connector; see the [connector README](../README.md#invoicechanged-trigger) for the window, bounds, and limitations. The first poll records a baseline and delivers no existing invoice, and deleted invoices are never reported. The binding has no configuration fields.

- `new_project_invoice_trigger_runner(project, connection, ProjectInvoiceTriggerRunnerConfig([...]))` records each change in the binding's project Trigger inbox before its watermark commits in the binding's project poll cursor, so a restarted or replaced replica neither misses nor repeats a change.
- `new_invoice_trigger_runner(InvoiceTriggerRunnerConfig(connection, [InvoiceChangedTriggerRoute(...)]))` takes targets and cursor stores directly; a route without a cursor store keeps it in memory.
- `new_invoice_changed_trigger(...)` is one Trigger with an in-memory cursor, as Go's `NewInvoiceChangedTrigger`; it records a new baseline on every start.
- `define_invoice_changed_trigger_binding` declares the binding in a Flow's `get_connector_trigger_bindings`.

The `pollInterval` connection setting (at least 5 seconds) sets the delay between polls. A 429 pauses every binding of the connection for `Retry-After`, or 60 seconds without it. A cursor for another company or environment, or a QuickBooks validation Fault for the watermark query, records a new baseline and logs a WARN record; changes in that gap are not delivered. Delivery is at least once, and each invoice version keeps one Trigger event ID, a hash of the environment, company, Id, SyncToken, and LastUpdatedTime. Cursor documents and inbox payloads match the Go connector's, so Go and Python replicas share one binding.

The [invoice change example](examples/invoice_changed/) wires the project runner to a Flow that reads each changed invoice back with `getInvoice` and records whether it is paid.

## Verification

The Go-recorded conformance suite covers provider request and outcome agreement. `tests/fixtures/go-invoice-change-cases.json`, recorded from the Go fetcher, is replayed against the same invoice fake and compares every request, cursor token, event ID, and stored payload. Both examples' real Dex integration tests use local QuickBooks stand-ins. Live Intuit authorization, QuickBooks company calls, and live invoice polling have not been verified.
