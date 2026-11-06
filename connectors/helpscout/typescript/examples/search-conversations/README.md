# Search Help Scout conversations

`SearchConversationsFlow` reads one page with the static
`helpscout-support-inbox` connection. The provider request runs in the
connector Step; the Flow retains only safe search filters and the result page.

Start `dexcli dev`, set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address, and
configure a Dex Web Connection named `helpscout-support-inbox`. In Help Scout,
open **Your Profile > My Apps**, create an app, and copy its App ID and App
Secret into `app_id` and `app_secret`. Leave `access_token` blank. Set
`DEX_CONNECTOR_CONFIG_FILE` to the mode-0600 local Dex Web `connections.json`
file. From `connectors/helpscout/typescript`, start the Worker:

```sh
npm run example:search
```

In another terminal with the same connection configuration, edit
`examples/search-conversations/input.example.json` to name your inbox and run:

```sh
npm run example:search:start -- examples/search-conversations/input.example.json
```

The starter prints one page. No credential belongs in the input file. To
verify the same Flow against a local provider stand-in and a real Dex stack,
run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:<dev-port> npm run test:integration`.

For a strict Flow Definition Graph 2.0, build the package, make it visible to
the analyzer, then lint the Flow with Dex CLI 1.28.0 or newer:

```sh
npm run build
npm install --prefix examples/search-conversations --no-save --no-package-lock --ignore-scripts .
cd examples/search-conversations
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

Live Help Scout authorization and provider calls have not been verified.
