# Xero invoice change Flow

`XeroInvoiceChangePython` starts once per Xero sales invoice or bill created or
changed in the connected organisation, through the `invoiceChanged` poll
Trigger. `RecordInvoiceChange` records the delivered change,
`ReadChangedInvoice` reads the invoice back with `getInvoice`, and
`CompleteInvoiceChange` completes with an `InvoiceChangeOutcome` holding the
delivered status and the invoice's current state. Only the `found` branch is
wired, so any other `getInvoice` branch fails the Flow.

The Flow ID is `xero-invoice-change-<Trigger event ID>`, and the event ID is
stable per invoice and `UpdatedDateUTC`, so a redelivered change finds its
existing Flow.

1. Run `dexcli dev -dex-port 37201 -web-port 37202 -open=false`.
2. In Dex Web Connections, create the `xero-invoice-watcher` Xero connection,
   as the [connector setup](../../../README.md#xero-setup) describes.
   `pollInterval` is optional; it is at least 30 seconds, and every poll spends
   the organisation's daily Xero API limit.
3. On the `invoice-changes` binding, **Invoices that start this Flow** chooses
   sales invoices (`ACCREC`), bills (`ACCPAY`), or both, and optionally the
   statuses an invoice must have after its change. Leave it unsaved to start a
   Flow for every invoice and bill. Restart the Worker after saving; a changed
   filter records a new baseline.
4. From `python/`, run
   `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:37201 uv run python -m examples.invoice_changed.main`.

The first poll records a baseline and delivers no existing invoice. A Dex Web
project keeps the poll cursor in process memory, so a local restart records a
new baseline; a hosted project keeps it in project storage, and each change is
recorded in the binding's Trigger inbox before the cursor commits.
`XERO_LOCAL_PROVIDER_URL` sends Xero API and token requests to a local
Xero-compatible fake for verification only.

`flow.py` declares the binding and its filter unit, `triggers.py` routes it
into the Flow through the binding's durable inbox, and `main.py` runs the
Worker and the poll runner together. `integration_test.py` runs against a real
Dex server with the in-process `invoice_fake.py`: a baseline and one Flow per
change across a restart, a changed filter that records a new baseline without
delivering the gap, and a minute-limit 429 that pauses the poll before the
change arrives. Start `dexcli dev -dex-port 44500 -web-port 44501 -open=false`
and run
`DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:44500 uv run pytest -q examples/invoice_changed/integration_test.py`
from `python/`.

## Flow Definition Graph

From `connectors/xero/python`, generate strict FDG 2.0 for the Flow:

```sh
dexcli visualize examples/invoice_changed/flow.py --language python \
  --schema-version 2.0 --json --out /tmp/xero-python-invoice-changed \
  --python .venv/bin/python
```

The generated JSON reports `valid: true`. dexcli v1.47.3 adds one
`connector_configuration_ui` warning for the binding's filter unit, as it
does for every Python Flow that declares a Configuration UI unit.
