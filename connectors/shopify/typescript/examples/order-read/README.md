# Read one Shopify order

`GetOrderFlow` reads one order through the static `shopify-orders` connection.
Its input is only the numeric order ID; the access token stays in the private
Dex Web connection. The Flow retains the nonsecret request and order summary.

Start `dexcli dev` and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address.
Configure `shopify-orders` as described in the
[connector guide](../../../README.md#authentication-and-configuration), then
set `DEX_CONNECTOR_CONFIG_FILE` to the mode-0600 Dex Web `connections.json`.
From `connectors/shopify/typescript`, run the Worker:

```sh
npm run example:order
```

In another terminal with the same configuration, start a Flow:

```sh
npm run example:order:start -- examples/order-read/input.example.json
```

To verify through real Dex with a local Shopify stand-in, set
`DEX_FLOW_SERVICE_ADDRESS` to the running `dexcli dev` address and run
`npm run test:integration`. The stand-in binds an available loopback port.
Live Shopify behavior remains unverified.

Generate and lint the strict Flow Definition Graph 2.0 with Dex CLI 1.28.0 or
newer. From `connectors/shopify/typescript`, make the built package visible
to the analyzer, then run:

```sh
npm run build
npm install --prefix examples/order-read --no-save --no-package-lock --ignore-scripts .
cd examples/order-read
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```
