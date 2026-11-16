# QuickBooks invoice change Flow

`QuickBooksInvoiceChangePython` starts once per QuickBooks invoice version created or
changed in the connected company, through the `invoiceChanged` poll Trigger. It
records the delivered change, reads the invoice back with `getInvoice`, and completes
with an `InvoiceChangeOutcome` that says whether the invoice is paid. Only the `found`
branch is wired, so any other `getInvoice` branch fails the Flow.

1. Run `dexcli dev -dex-port 37201 -web-port 37202 -open=false`.
2. In Dex Web Connections, create the `quickbooks-invoice-watcher` QuickBooks Online
   connection and authorize the company, as the
   [connector README](../../../README.md#quickbooks-setup) describes. `pollInterval`
   is optional.
3. The `quickbooks-invoice-changes` binding watches the whole company and has no
   configuration fields, so there is nothing to save for it.
4. From `python/`, run
   `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:37201 uv run python -m examples.invoice_changed.main`.

The first poll records a baseline and delivers no existing invoice. A Dex Web project
keeps the poll cursor in process memory, so a local restart records a new baseline;
a hosted project keeps it in project storage. `QUICKBOOKS_LOCAL_PROVIDER_URL` sends
QuickBooks API and Intuit token requests to a local QuickBooks-compatible fake for
verification only.

`flow.py` declares the binding, `triggers.py` routes it into the Flow through the
binding's durable inbox, and `main.py` runs the Worker and the poll runner together.
`integration_test.py` runs against a real Dex server with the in-process
`invoice_fake.py`: a baseline, one Flow per version across a restart whose first poll
answers 429, and a rejected watermark that reseeds without delivering the gap. Start
`dexcli dev -dex-port 44500 -web-port 44501 -open=false` and run
`DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:44500 uv run pytest -q examples/invoice_changed/integration_test.py`
from `python/`.

## Flow Definition Graph

From `connectors/intuit/quickbooks/python`, generate strict FDG 2.0 for the Flow:

```sh
dexcli visualize examples/invoice_changed/flow.py --language python \
  --schema-version 2.0 --json --out /tmp/quickbooks-python-invoice-changed \
  --python .venv/bin/python
```

The generated JSON must report `valid: true` and have no diagnostics.
