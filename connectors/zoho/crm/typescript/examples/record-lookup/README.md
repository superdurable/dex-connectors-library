# Zoho CRM TypeScript record lookup

The [Flow](flow.ts) starts with the generated `getRecord` Step and completes with its `found` result. Its `ConnectionName` is statically `zoho-crm-main`. Create that Zoho CRM connection in Dex Web, select the correct data center, and follow the [connector setup guide](../../../README.md) for the application and scopes. Set `DEX_CONNECTOR_CONFIG_FILE` to the private Dex Web `connections.json` file before running the Worker; never put credentials in Flow input.

From `connectors/zoho/crm/typescript`, start Dex and the Worker in separate terminals:

```sh
dexcli dev -dex-port 40842 -web-port 40843 -open=false
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:40842 npm run example:worker
```

Start a lookup with a module API name and numeric Zoho record ID:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:40842 npm run example:start -- Contacts 4150868000000376008
```

The [integration test](integration.test.ts) uses a local provider fake through a real Dex Worker and Server. It proves retry of a 503 and failure on an unwired optional `notFound` branch:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:40842 npm run test:integration
```

The Flow declares Dex FDG 2.0 group and explanation metadata. Live Zoho authorization and API access need a user-controlled Dex Web connection and have not been verified.

Dex CLI v1.33.0 rendered [the Flow](flow.ts) as valid FDG 2.0 with application lint and no diagnostics. From this checkout, build the package and make its package name visible to the local parser before visualizing:

```sh
npm run build
mkdir -p examples/record-lookup/node_modules/@superdurable /tmp/zoho-crm-ts-fdg
ln -s ../../../.. examples/record-lookup/node_modules/@superdurable/connector-zoho-crm
dexcli visualize examples/record-lookup/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/zoho-crm-ts-fdg/lookup
```
