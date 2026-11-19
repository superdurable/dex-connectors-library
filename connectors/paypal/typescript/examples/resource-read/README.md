# Read a PayPal order

`ResourceReadFlow` uses the static `paypal` connection. Its provider call runs in the generated getOrder Query Step. A missing order selects the unwired optional `notFound` branch and fails the Flow.

Start `dexcli dev`, set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address, and create a `paypal` connection in Dex Web Connections using the authorization guide. Set `DEX_CONNECTOR_CONFIG_FILE` to the mode-0600 local Connections file. From `connectors/paypal/typescript`, run `npm run example:resource-read`. In another terminal, run `npm run example:resource-read:start -- examples/resource-read/input.example.json`. The starter prints the safe order summary; credentials never enter Flow input.

Run the real Worker and Client integration test against a local PayPal stand-in with `DEX_FLOW_SERVICE_ADDRESS=<dev-address> npm run test:integration`.

Generate and lint strict FDG 2.0 from this package directory:

```sh
npm run build
npm install --prefix examples/resource-read --no-save --no-package-lock --ignore-scripts .
dexcli visualize examples/resource-read/flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```
