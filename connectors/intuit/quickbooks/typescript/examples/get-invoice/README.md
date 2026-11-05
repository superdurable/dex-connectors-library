# Read one QuickBooks invoice

`GetQuickBooksInvoiceFlow` reads an invoice through the static
`quickbooks-accounting` connection. Its provider call runs in the connector
Step. The Flow persists only the invoice ID and returned invoice, never OAuth
credentials.

Start `dexcli dev` and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address.
Configure the connection in Dex Web using the manifest's Intuit OAuth guide.
Set `DEX_CONNECTOR_CONFIG_FILE` to the mode-0600 local Dex Web
`connections.json` file. From `connectors/intuit/quickbooks/typescript`, run:

```sh
npm run example:invoice
```

In another terminal with the same configuration, replace `invoiceId` in
`input.example.json` with an invoice ID from the connected company and run:

```sh
npm run example:invoice:start -- examples/get-invoice/input.example.json
```

The starter prints the invoice. To exercise this Flow without a live Intuit
company, start `dexcli dev` on an available port, set
`DEX_FLOW_SERVICE_ADDRESS`, and run `npm run test:integration`. The test binds
its QuickBooks stand-in to an OS-assigned loopback port, runs the Flow through
a real Dex Worker and Client, and verifies an unwired not-found branch fails.

Generate and lint the strict Flow Definition Graph 2.0 from the package root:

```sh
npm run build
npm install --prefix examples/get-invoice --no-save --no-package-lock --ignore-scripts .
dexcli visualize examples/get-invoice/flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```
