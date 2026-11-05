# Search Freshdesk tickets

`SearchTicketsFlow` reads one bounded Freshdesk filter page through the static `freshdesk-helpdesk` connection. It saves the safe search input and ticket page as registered Attributes. Other provider outcomes fail the Flow through unwired optional branches.

Start a Dex development server and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address. In Dex Web Connections, create a Freshdesk connection named `freshdesk-helpdesk`, set the helpdesk domain, and store the agent API key in `api_key`. Point `DEX_CONNECTOR_CONFIG_FILE` at the local mode-0600 `connections.json` written by Dex Web. From `connectors/freshworks/freshdesk/typescript`, run:

```sh
npm run example:search
```

In another terminal with the same environment, edit `examples/search-tickets/input.example.json` to use statuses and tags from your helpdesk, then start a Flow:

```sh
npm run example:search:start -- examples/search-tickets/input.example.json
```

The starter prints the returned ticket page. No credential belongs in the input file.

Run the real Dex integration test against a local Freshdesk-compatible HTTP stand-in:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:21040 npm run test:integration
```

Generate and lint the strict Flow Definition Graph 2.0 with Dex CLI 1.28.0 or newer:

```sh
npm run build
npm install --prefix examples/search-tickets --no-save --no-package-lock --ignore-scripts .
cd examples/search-tickets
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

Live Freshdesk authorization and search behavior remain unverified. The integration test uses a provider stand-in through a real Dex Worker and Client.
