# Zoho CRM TypeScript record lookup

The [Flow](flow.ts) starts with the generated `getRecord` Step and completes with its `found` result. Its `ConnectionName` is statically `zoho-crm-main`. Create that Zoho CRM connection in Dex Web, select the correct data center, and follow the [connector setup guide](../../../README.md) for the application and scopes. Set `DEX_CONNECTOR_CONFIG_FILE` to the private Dex Web `connections.json` file before running the Worker; never put credentials in Flow input.

From `connectors/zoho/crm/typescript`, start Dex and the Worker in separate terminals:

```sh
dexcli dev -dex-port 47120 -web-port 47121 -open=false
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:47120 npm run example:worker
```

Start a lookup with a module API name and numeric Zoho record ID:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:47120 npm run example:start -- Contacts 4150868000000376008
```

The [integration test](integration.test.ts) uses a provider stand-in bound to an OS-assigned free port through a real Dex Worker and Server. It proves retry of a 503 and failure on an unwired optional `notFound` branch:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:47120 npm run test:integration
```

The Flow declares Dex FDG 2.0 group and explanation metadata. Live Zoho authorization and API access need a user-controlled Dex Web connection and have not been verified.
