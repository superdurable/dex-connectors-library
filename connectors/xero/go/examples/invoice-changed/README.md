# Xero invoice-changed example

This example starts one `XeroInvoiceChange` Flow for every sales invoice or
bill created or changed in the connected Xero organisation, through the
`invoiceChanged` Trigger:

1. `RecordInvoiceChange` validates the delivered change and records it.
2. `ReadChangedInvoice` calls `xero.NewGetInvoiceStep`, so the record holds the
   invoice's current state rather than the polled copy.
3. `CompleteInvoiceChange` records the invoice read back and completes the
   Flow.

The Flow ID is `xero-invoice-change-<Trigger event ID>`, and the Trigger event
ID is stable per invoice and `UpdatedDateUTC`. A redelivered change therefore
finds its existing Flow instead of starting a second one. Only the happy-path
`found` branch is wired. A rejected or invalid read fails the Flow.

The first start records the organisation's existing invoices as a baseline
and starts no Flow for them. See the
[connector README](../../../README.md#invoicechanged-trigger) for polling,
the overlap window, cursor resets, and rate limits.

## Configure

Follow the [Xero Connector setup](../../../README.md#xero-setup), then
configure the `xero-invoice-watcher` connection under **Connections** in Dex
Web.

- **Poll interval** on the connection is the delay between polls, at least 30
  seconds. Every poll spends at least one call of the organisation's daily
  Xero API limit, shared with the connection's operation Steps.
- **Invoices that start this Flow** on the `invoice-changes` Trigger binding
  chooses sales invoices (`ACCREC`), bills (`ACCPAY`), or both, and optionally
  the statuses an invoice must have after its change. Leave it unsaved to
  start a Flow for every invoice and bill.

The Worker reads the saved values once at startup, so restart it after saving.
A changed filter records a new baseline.

## Run

Generate strict FDG 2.0 from `connectors/xero/go` with the latest stable
dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/invoice-changed/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/invoice-changed
```

Run `dexcli dev` with that build directory, then run the Worker. It reads the
`DEX_PROJECT_*` project configuration, or the `DEX_CONNECTOR_CONFIG_FILE`
connections file that Dex Web writes under `dexcli dev`, as documented in
[`sdkgo/projectconfig`](../../../../../sdkgo/projectconfig/README.md#application-loading):

```bash
go run ./examples/invoice-changed
```

Override `DEX_FLOW_SERVICE_ADDRESS`, `DEX_BLOB_CACHE_DIR`, or `LOG_LEVEL`
(`debug`, `info`, `warn`, or `error`) when needed. `XERO_LOCAL_PROVIDER_URL`
sends every Xero request to a loopback Xero stand-in, for local verification
only.

With hosted project storage, each change is recorded in the binding's Trigger
inbox before the poll cursor commits, so a restarted Worker neither misses nor
repeats a change. Under `dexcli dev`, the poll cursor stays in process memory,
so a restarted local Worker records a new baseline. Changes made while it was
stopped are not delivered.

## Test

```bash
GOWORK=off go test ./examples/invoice-changed/...
GOWORK=off go test -tags=integration ./examples/invoice-changed/... -count=1 -v
```

The integration tests run a real Dex Worker and Client against a local
stand-in for Xero's `GET /Invoices`. They check that the baseline starts no
Flow, that each change starts exactly one Flow, and that changes made while
the Trigger process is stopped arrive after a restart. They also check that a
changed filter records a new baseline without delivering the gap, and that a
minute-limit 429 pauses the poll before the change arrives. Live Xero polling
has not been verified.
