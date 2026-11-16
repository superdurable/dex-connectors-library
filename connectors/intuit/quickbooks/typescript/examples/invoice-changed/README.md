# QuickBooks invoice change Flow

`QuickBooksInvoiceChange` starts once per QuickBooks invoice version created or
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
4. Set `DEX_CONNECTOR_CONFIG_FILE` to the mode-0600 local Dex Web `connections.json`
   file, and from `connectors/intuit/quickbooks/typescript` run
   `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:37201 npm run example:invoice-changed`.

The first poll records a baseline and delivers no existing invoice. A Dex Web project
keeps the poll cursor in process memory, so a local restart records a new baseline;
a hosted project keeps it in project storage. `QUICKBOOKS_LOCAL_PROVIDER_URL` sends
QuickBooks API requests to a local QuickBooks-compatible fake for verification only.

`flow/workflow.ts` declares the binding, `app.ts` routes it into the Flow through the
binding's durable inbox and runs the Worker and the poll runner together, and
`main.ts` is the entrypoint. `test/integration/invoice-changed.integration.test.ts`
runs against a real Dex server with the in-process `test/invoice_fake.ts`: a
baseline, one Flow per version across a restart whose first poll answers 429, and a
rejected watermark that reseeds without delivering the gap. With `dexcli dev` running,
set `DEX_FLOW_SERVICE_ADDRESS` and run `npm run test:integration`.

Generate and lint the strict Flow Definition Graph 2.0 from the package root:

```sh
npm run build
npm install --prefix examples/invoice-changed --no-save --no-package-lock --ignore-scripts .
dexcli visualize examples/invoice-changed/flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```
