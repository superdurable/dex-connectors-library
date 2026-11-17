# Xero Python connector

`dex-connector-xero` v0.2.1 exposes all six Xero contact, invoice, and payment
operations through generated Dex Step factories and the `invoiceChanged` poll
Trigger. It requires `dex-connectors-sdk>=0.12.0` and uses generated Custom
Connection and OAuth refresh wiring. Configure `xero-books` in Dex Web **Connections** using the
[connector setup](../README.md#xero-setup). Provider operations use exact
decimal strings and send writes under the Call ID as `Idempotency-Key`.
Wire decimals retain their exact digits, including `-0` and trailing zeros; exponent-form numbers are rejected as invalid responses.

The [runnable contact lookup](examples/contact_lookup/README.md) is the minimal
consuming application. From this directory, install and verify it with:

```sh
uv sync --locked
dexcli visualize examples/contact_lookup/flow.py --schema-version 2.0 \
  --json --python .venv/bin/python --out /tmp/xero-python-contact-lookup
```

The command reports `valid: true`. Start `dexcli dev` on available ports,
set `DEX_FLOW_SERVICE_ADDRESS` and the project configuration exported by Dex
Web, then run `uv run python -m examples.contact_lookup.main`. The Worker
uses the static `xero-books` connection. Start
`XeroFindContactByEmailPython` with an input such as
`{"emailAddress":"accounts@example.com"}` and a unique Flow ID. The
`found` branch completes with the contact; unwired optional branches fail.

## invoiceChanged Trigger

`invoiceChanged` delivers one `InvoiceChangedEvent` for each sales invoice or
bill created or changed in the organisation, at parity with the Go connector:
it polls `GET /Invoices` with `If-Modified-Since` five seconds before the
newest delivered `UpdatedDateUTC`, ordered by `UpdatedDateUTC`, with the
binding's `type` and `statuses` filters. The first poll records a baseline
from Xero's `DateTimeUTC`, so existing invoices are never delivered. Event IDs
hash the invoice ID and `UpdatedDateUTC`, so a refetch yields the same ID.
A 429 for the minute, app-minute, concurrency, or daily limit pauses the
connection for Retry-After; a cursor saved for another filter or organisation
records a new baseline and logs a WARN record. The cursor document is the Go
connector's JSON byte for byte, so Go and Python replicas share a binding.
The connection's `pollInterval` is at least 30 seconds and defaults to five
minutes, because every poll spends the organisation's daily Xero API limit.

```python
from dex_connectors.sdk import new_dex_flow_trigger_target
from dex_connectors.xero import (
    ProjectInvoiceChangedTriggerRoute,
    ProjectInvoiceTriggerRunnerConfig,
    new_project_connection,
    new_project_invoice_trigger_runner,
)

connection = new_project_connection(project, "xero-invoice-watcher")
target = new_dex_flow_trigger_target(dex_client, flow, accepts_event=accept,
                                     resolve_flow_id=flow_id, map_to_flow_input=to_input)
runner = new_project_invoice_trigger_runner(project, connection, ProjectInvoiceTriggerRunnerConfig(
    [ProjectInvoiceChangedTriggerRoute("invoice-changes", target)]))
await runner.run(context)
```

`new_project_invoice_trigger_runner` loads each binding's saved filter (an
unsaved binding receives every invoice and bill) and records each change in
the binding's project Trigger inbox before the cursor commits in its project
poll cursor. `new_invoice_trigger_runner` takes routes, configurations, and
cursor stores directly, and `new_invoice_changed_trigger` keeps the cursor in
memory, so it records a new baseline on every start. Declare the binding with
`define_invoice_changed_trigger_binding` and the `invoiceChangeFilter` unit.

The [invoice change example](examples/invoice_changed/README.md) starts one
Flow per change and reads the invoice back with `new_get_invoice_step`.
`tests/fixtures/go-invoice-change-cases.json`, recorded from the Go fetcher
against its invoice fake, is replayed through the Python fetcher and the
Python port of the fake, comparing every request, cursor token, event ID, and
stored payload.

## Verification

Targeted verification:

```sh
uv run pytest tests -q
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:45901 \
  uv run pytest examples/contact_lookup/integration_test.py -q
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:45901 \
  uv run pytest examples/invoice_changed/integration_test.py -q
```

The integration test needs an already running `dexcli dev` at the selected
address and binds its provider stand-in to an OS-assigned free port. The
Go-recorded conformance fixtures and generated refresh agreement also run in
`make test-connector-conformance CONNECTOR_DIRECTORY=connectors/xero` from
the repository root. Live Xero authorization, provider behavior, and live
invoice polling are unverified for this port.
