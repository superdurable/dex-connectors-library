# Track a Mixpanel event

`EventTrackFlow` imports one event using the static Dex Web connection `mixpanel`. Its provider call runs in the connector Step. The Flow stores only its nonsecret input and import result.

Start `dexcli dev` and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address. In Dex Web, configure the `mixpanel` connection with the service-account username and secret from Organization Settings > Service Accounts, and the numeric Project ID from Project Settings > Overview. Set `DEX_CONNECTOR_CONFIG_FILE` to the local Dex Web `connections.json`. From `connectors/mixpanel/typescript`, run the Worker:

```sh
npm run example:event-track
```

In a second terminal with the same configuration, run the starter with the checked-in input:

```sh
npm run example:event-track:start -- examples/event-track/input.example.json
```

The starter prints `{ "imported": true }` after a confirmed import. Keep service-account credentials out of the input file.

The real Dex integration test uses a local Mixpanel HTTP stand-in:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:23101 npm run test:integration
```

Generate and lint its Flow Definition Graph 2.0 from this directory with Dex CLI 1.28.0 or newer:

```sh
npm run build
npm install --prefix examples/event-track --no-save --no-package-lock --ignore-scripts .
cd examples/event-track
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

Live Mixpanel authorization, import, and export remain unverified.
