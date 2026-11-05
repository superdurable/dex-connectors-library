# List Pipedrive deals

`ListDealsFlow` lists one bounded page through the static `pipedrive-crm` connection. Its provider call runs in the connector Step; the Flow persists only the safe request and returned page.

Start `dexcli dev` and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address. In Dex Web Connections, configure a Pipedrive connection named `pipedrive-crm` with either a Personal API token from [Personal preferences > API](https://app.pipedrive.com/settings/api) or an OAuth app configured in [Developer Hub](https://app.pipedrive.com/developer-hub). For OAuth, register the redirect URI shown by Dex Web and enable Deals Full and Contacts Full access before consent. Set `DEX_CONNECTOR_CONFIG_FILE` to the mode-0600 local Dex Web `connections.json` file.

From `connectors/pipedrive/typescript`, start the Worker:

```sh
npm run example:list
```

In another terminal with the same configuration, start the Flow:

```sh
npm run example:list:start -- examples/list-deals/input.example.json
```

For the local real-Dex integration test, run `DEX_FLOW_SERVICE_ADDRESS=<your-dev-server-address> npm run test:integration`. The test starts its own Pipedrive HTTP stand-in on an OS-assigned port and requires no provider credential.

For strict Flow Definition Graph 2.0 verification, run these commands from `connectors/pipedrive/typescript`:

```sh
npm run build
npm install --prefix examples/list-deals --no-save --no-package-lock --ignore-scripts .
dexcli visualize examples/list-deals/flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

No live Pipedrive company or OAuth consent has been exercised by this example.
