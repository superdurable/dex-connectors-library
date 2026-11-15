# QuickBooks invoice-changed example

This example starts one `QuickBooksInvoiceChange` Flow for every invoice
created or changed in the connected QuickBooks Online company, through the
`invoiceChanged` Trigger:

1. `RecordInvoiceChange` validates the delivered change and records it.
2. `ReadChangedInvoice` calls `quickbooks.NewGetInvoiceStep`, so the record
   holds the invoice's current balance rather than the polled copy.
3. `CompleteInvoiceChange` records the invoice read back and whether its
   `Balance` is zero, then completes the Flow.

The Flow ID is `quickbooks-invoice-change-<Trigger event ID>`, and the Trigger
event ID is stable per invoice version. A redelivered change therefore finds
its existing Flow instead of starting a second one. Only the happy-path
`found` branch is wired. A deleted, rejected, or invalid read fails the Flow.

The first start records the company's existing invoices as a baseline and
starts no Flow for them. See the
[connector README](../../../README.md#invoicechanged-trigger) for the cursor,
change kinds, resets, and rate limits.

## Configure

Follow the [QuickBooks setup](../../../README.md#quickbooks-setup), then
configure the `quickbooks-invoice-watcher` connection under **Connections** in
Dex Web.

- **Poll interval** on the connection is the delay between polls, at least 5
  seconds. Each poll is one QuickBooks request, or up to ten while catching
  up, against the company's 500 requests a minute, which this connection's
  Steps share.
- The `quickbooks-invoice-changes` Trigger binding has no fields: it watches
  every invoice of the authorized company.

The Worker reads the connection once at startup, so restart it after saving.
Authorizing a different company records a new baseline for it.

## Run

Generate strict FDG 2.0 from `connectors/intuit/quickbooks/go` with the latest
stable dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/invoice-changed/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/invoice-changed
```

Run `dexcli dev` with that build directory, then run the Worker. It reads the
`DEX_PROJECT_*` project configuration, or the `DEX_CONNECTOR_CONFIG_FILE`
connections file that Dex Web writes under `dexcli dev`, as documented in
[`sdkgo/projectconfig`](../../../../../../sdkgo/projectconfig/README.md#application-loading):

```bash
go run ./examples/invoice-changed
```

Override `DEX_FLOW_SERVICE_ADDRESS`, `DEX_BLOB_CACHE_DIR`, or `LOG_LEVEL`
(`debug`, `info`, `warn`, or `error`) when needed. For local verification
only, `QUICKBOOKS_LOCAL_PROVIDER_URL` sends QuickBooks API and token requests
to a loopback QuickBooks-compatible fake.

With hosted project storage, each change is recorded in the binding's Trigger
inbox before the watermark commits, so a restarted Worker neither misses nor
repeats a change. Under `dexcli dev`, the poll cursor stays in process memory,
so a restarted local Worker records a new baseline. Changes made while it was
stopped are not delivered.

## Test

```bash
GOWORK=off go test ./examples/invoice-changed/...
GOWORK=off go test -tags=integration ./examples/invoice-changed/... -count=1 -v
```

The integration tests run a real Dex Worker and Client against a local
stand-in for QuickBooks's invoice query and read endpoints. They check that
the baseline starts no Flow, that each invoice version starts exactly one
Flow, and that changes made while the Trigger process is stopped arrive after
a restart whose first poll is throttled with HTTP 429. They also check that a
rejected watermark query records a new baseline without delivering the gap.
Live QuickBooks polling has not been verified.
