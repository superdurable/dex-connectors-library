# Get Mailgun events

`GetEventsFlow` reads one bounded delivery events page through the static Dex Web connection `mailgun`. Its provider request runs inside the `getEvents` connector Step. A successful page is stored in the Flow's `mailgun-events-page` Attribute and returned. An unwired optional branch fails the Flow.

Start `dexcli dev` and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address. In Dex Web Connections, save a Mailgun connection named `mailgun` with the secret `api_key` from [Mailgun > Settings > API Security > API Keys](https://app.mailgun.com/settings/api_security). If needed, set the public EU API base URL. Set `DEX_CONNECTOR_CONFIG_FILE` to the mode-0600 local Dex Web `connections.json` file. From `connectors/mailgun/typescript`, start the Worker:

```sh
npm run example:events
```

In another terminal with the same environment, edit `input.example.json` to name your sending domain, then start the Flow:

```sh
npm run example:events:start -- examples/get-events/input.example.json
```

The starter prints the events page. Never put an API key in Flow input.

Run the real Dex integration test with a local Mailgun HTTP fixture:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:21290 npm run test:integration
```

Generate and lint the strict Flow Definition Graph 2.0 using Dex CLI 1.28.0 or newer:

```sh
npm run build
npm install --prefix examples/get-events --no-save --no-package-lock --ignore-scripts .
cd examples/get-events
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

The live Mailgun authorization form, API key permissions, US/EU routing, and provider event response have not been verified.
