# Search Re:amaze conversations

`SearchConversationsFlow` searches one bounded page using the static `reamaze-triage` connection. The provider call runs inside a Dex Step, and only the nonsecret request and returned page are persisted.

Start `dexcli dev` and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address. Configure the private Dex Web connection named `reamaze-triage` with the brand subdomain, staff login email, and API token; follow the connector authorization guide in Dex Web. Set `DEX_CONNECTOR_CONFIG_FILE` to the mode-0600 local Dex Web `connections.json` file. From `connectors/reamaze/typescript`, run:

```sh
npm run example:search
```

In another terminal with the same configuration, edit `input.example.json` to name a real requester email, then run:

```sh
npm run example:search:start -- examples/search-conversations/input.example.json
```

The starter prints the page. No credential belongs in the input file. A real Dex integration test runs against a local Re:amaze-compatible HTTP stand-in:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:30011 npm run test:integration
```

From `connectors/reamaze/typescript`, build and lint the strict FDG 2.0 source with Dex CLI 1.28.0 or newer:

```sh
npm run build
npm install --prefix examples/search-conversations --no-save --no-package-lock --ignore-scripts .
cd examples/search-conversations
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

Live Re:amaze authorization and provider responses have not been verified.
