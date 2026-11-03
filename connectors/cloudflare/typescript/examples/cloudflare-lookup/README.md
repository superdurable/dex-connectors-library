# Cloudflare zone lookup

`CloudflareLookupFlow` lists the first zone page through the static `cloudflare`
connection. Its provider call runs in the connector Step. The Flow persists
only the empty request and the typed zone result.

Start `dexcli dev` with a gRPC port in 21200–21299 and set
`DEX_FLOW_SERVICE_ADDRESS` to that address. In Dex Web, create a private
connection named `cloudflare` and store a scoped Cloudflare API token with
Zone Read permission. Set `DEX_CONNECTOR_CONFIG_FILE` to the mode-0600 Dex Web
`connections.json` file. From `connectors/cloudflare/typescript`, run:

```sh
npm run example:lookup
```

In another terminal with the same environment, start the Flow:

```sh
npm run example:lookup:start -- examples/cloudflare-lookup/input.example.json
```

The starter prints one zone page. No token belongs in the Flow input.

Run the real Dex integration test against a local Cloudflare HTTP fixture:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:21200 npm run test:integration
```

Generate and lint its strict Flow Definition Graph 2.0 source from this example
directory after building the package and installing it locally for the analyzer:

```sh
npm run build
npm install --prefix examples/cloudflare-lookup --no-save --no-package-lock --ignore-scripts .
cd examples/cloudflare-lookup
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

Live Cloudflare authorization and zone listing have not been verified.
