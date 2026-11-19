# ElevenLabs voice demo

`ElevenLabsDemoFlow` calls `listVoices` through the static `elevenlabs`
connection and completes with the returned page. The provider call runs in
Dex Step Execute. Optional rejection branches remain unwired and fail the
Flow when selected.

Create an ElevenLabs API key at
[Developers > API Keys](https://elevenlabs.io/app/developers/api-keys), then
save it in Dex Web Connections as `api_key` for the `elevenlabs` connection.
Leave the manifest's base URL, timeout, and response limit blank for defaults.
Set `DEX_CONNECTOR_CONFIG_FILE` to your private Dex connection file and
`DEX_FLOW_SERVICE_ADDRESS` to the local Dex gRPC address. Start `dexcli dev`
with an available gRPC and Web port. From `connectors/elevenlabs/typescript`:

```sh
npm ci
npm run example:demo
```

In another terminal with the same environment, start a Flow:

```sh
npm run example:demo:start -- examples/demo/input.example.json
```

The starter prints the voice page. No credential belongs in the input file.

For strict Flow Definition Graph 2.0 generation with Dex CLI 1.28.0 or newer,
first build the local package and make it visible to the analyzer, then run:

```sh
npm run build
npm install --prefix examples/demo --no-save --no-package-lock --ignore-scripts .
cd examples/demo
dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

The local integration test exercises this same Flow through a real Dex Worker
and Client with a local provider fixture:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:21220 npm run test:integration
```

Live ElevenLabs authorization and provider behavior remain unverified.
