# WooCommerce resource read example

`ResourceReadFlow` lists one order page using the static `woocommerce` connection. Its successful `listed` branch completes with an `OrderPage`; optional error branches are unwired and fail the Flow. The application accepts `ListOrdersInput`, for example `{"page":1,"perPage":10}`.

Configure the connection in Dex Web **Connections** using the authorization guide in `connector.yaml`. Set `DEX_CONNECTOR_CONFIG_FILE` to the local connection file created for your Dex project. Keep the consumer key and secret out of Flow input.

Start a local Dex stack on available ports, then run the Worker from this package directory:

```sh
npm ci
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:30541 npm run example:resource-read
```

Start `WooCommerceTypeScriptResourceRead` in Dex Web with a unique Flow ID and the input above. The Flow calls the real provider through the configured connection.

Run the deterministic real Dex integration test with a local HTTP provider fixture:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:30541 npm run test:integration
```

Generate a strict Flow Definition Graph 2.0 from the checked-in Flow source:

```sh
npm run build
npm install --prefix examples/resource-read --no-save --no-package-lock --ignore-scripts .
cd examples/resource-read
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

Live WooCommerce authorization and provider behavior require a user-controlled store and remain unverified.
