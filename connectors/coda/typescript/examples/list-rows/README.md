# List Coda rows

`ListRowsFlow` lists one bounded page with the static `coda` connection.
Configure that private connection in Dex Web with a Coda API token, then set
`DEX_CONNECTOR_CONFIG_FILE` to the mode-0600 local `connections.json`
file. The token stays outside Flow input and output.

Start `dexcli dev`, set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address,
and run these commands from `connectors/coda/typescript`:

```sh
npm ci
npm run example:list
```

In another terminal with the same environment, replace the IDs in
`input.example.json` with your own document and table IDs, then start a
Flow:

```sh
npm run example:list:start -- examples/list-rows/input.example.json
```

Run the real Dex integration test against its local provider fixture:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:21061 npm run test:integration
```

Generate and lint a strict Flow Definition Graph 2.0 from the runnable source:

```sh
npm run build
npm install --prefix examples/list-rows --no-save --no-package-lock --ignore-scripts .
cd examples/list-rows
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

Live Coda account authorization and permissions remain unverified.
