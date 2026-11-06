# List Xero contacts

`ListContactsFlow` reads one bounded contact page using the static `xero-contacts` connection. The Worker calls Xero from the connector Step and persists only the safe request and page.

Start `dexcli dev` with free gRPC and Web ports, then set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address. In Dex Web, configure `xero-contacts` as a Xero Custom Connection or OAuth connection following the [connector authorization guide](../../../README.md). Point `DEX_CONNECTOR_CONFIG_FILE` at the private mode-0600 `connections.json` file exported by Dex Web. From `connectors/xero/typescript`, run:

```sh
npm run example:list
```

In a second terminal with the same environment, edit `examples/list-contacts/input.example.json` and run:

```sh
npm run example:list:start -- examples/list-contacts/input.example.json
```

The starter prints the contact page. Never put a token or client secret in the input file.

To test with a local Xero stand-in through real Dex, start `dexcli dev`, then run `DEX_FLOW_SERVICE_ADDRESS=<gRPC-address> npm run test:integration`. The test starts its provider on an OS-assigned free port.

Generate and lint the strict Flow Definition Graph 2.0 from `connectors/xero/typescript` after building the local package:

```sh
npm run build
npm install --prefix examples/list-contacts --no-save --no-package-lock --ignore-scripts .
dexcli visualize examples/list-contacts/flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

Live Xero Custom Connection and OAuth setup, including organisation selection, remains unverified.
