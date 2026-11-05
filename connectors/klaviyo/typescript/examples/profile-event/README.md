# Klaviyo profile event

`ProfileEventFlow` reads a Klaviyo profile, then submits one event for its email. Its static Dex Web connection name is `klaviyo-profile-events`. The provider calls run inside generated connector Steps; the Flow retains only its nonsecret request and event acceptance.

Start `dexcli dev` and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address. Configure a private Dex Web connection named `klaviyo-profile-events` with the key and scopes in the [connector authorization guide](../../README.md#authorization). Set `DEX_CONNECTOR_CONFIG_FILE` to the mode-0600 local Dex Web `connections.json` file. From `connectors/klaviyo/typescript`, run:

```sh
npm run example:profile
```

In another terminal with the same configuration, replace the profile ID and event metric in `input.example.json`, then start the Flow:

```sh
npm run example:profile:start -- examples/profile-event/input.example.json
```

The starter prints `{ "accepted": true }` after Klaviyo responds with HTTP 202. The event might still be dropped during asynchronous processing. No API key belongs in the input file.

Run the real Dex integration test against the local HTTP fixture with a unique Dex CLI port:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:21034 npm run test:integration
```

Generate and lint the strict Flow Definition Graph 2.0 with Dex CLI 1.28.0 or newer:

```sh
npm run build
npm install --prefix examples/profile-event --no-save --no-package-lock --ignore-scripts .
cd examples/profile-event
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

Live Klaviyo authorization, profile lookup, and event delivery have not been verified.
