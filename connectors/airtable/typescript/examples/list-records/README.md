# List Airtable records

`ListRecordsFlow` lists one bounded page with the static `airtable-policies`
connection. Its provider call runs inside the connector Step. The Flow retains
only the safe input and returned page.

Start `dexcli dev` and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address.
Configure the private Dex Web connection named `airtable-policies` with an
Airtable personal access token and `data.records:read` access to the selected
base. Set `DEX_CONNECTOR_CONFIG_FILE` to the mode-0600 local Dex Web
`connections.json` file. From `connectors/airtable/typescript`, start the
Worker:

```sh
npm run example:list
```

In another terminal with the same configuration, edit
`input.example.json` to use your base and table IDs, then start the Flow:

```sh
npm run example:list:start -- examples/list-records/input.example.json
```

The starter prints the returned page. No token belongs in the input file.

Run the real Dex integration test against a local Airtable HTTP fixture:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npm run test:integration
```

Generate and lint the strict Flow Definition Graph 2.0 with Dex CLI 1.28.0
or newer. From `connectors/airtable/typescript`, make the local package visible
to the analyzer, then run:

```sh
npm run build
npm install --prefix examples/list-records --no-save --no-package-lock --ignore-scripts .
cd examples/list-records
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

Dex Web TypeScript connector discovery requires CLI/Web R4 support. Until it
is available, configure the private project connection and verify it at
Worker startup. Live Airtable authorization and base or table selection have
not been exercised.
