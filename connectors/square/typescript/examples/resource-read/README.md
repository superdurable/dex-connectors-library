# Read a Square payment

`ResourceReadFlow` reads one payment with the static `square` connection. The
provider call runs inside the connector Step. The Flow retains only the payment
ID and safe payment summary.

Start `dexcli dev`, set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address, and
configure the private Dex Web connection named `square` as described in the
[connector README](../../README.md). Set `DEX_CONNECTOR_CONFIG_FILE` to the
mode-0600 local `connections.json` file. From `connectors/square/typescript`,
run the Worker and starter in separate terminals:

```sh
npm run example:read
npm run example:read:start -- examples/resource-read/input.example.json
```

The starter prints the returned payment. No token belongs in the input file.
The real Dex integration test uses a local provider stand-in on a free port:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:28701 npm run test:integration
```

A real Square connection and live provider behavior were not verified.

Generate and lint a strict Flow Definition Graph 2.0 with Dex CLI 1.28.0 or
newer. Make the local package available to the analyzer, then run from this
example directory:

```sh
npm run build --prefix ../..
npm install --no-save --no-package-lock --ignore-scripts ../..
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```
