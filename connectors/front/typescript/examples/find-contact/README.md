# Find a Front contact

`FindContactByEmailFlow` looks up one contact through the static `front-support` Dex Web connection. Its provider request runs inside the connector Step. The Flow retains the nonsecret email and contact result.

Create a Front API token in [Front Settings > Developers > API Tokens](https://app.frontapp.com/) with contact read permission. Save it as `api_token` in a Dex Web Connection named `front-support`. Set `DEX_CONNECTOR_CONFIG_FILE` to the local Dex Web `connections.json` file and `DEX_FLOW_SERVICE_ADDRESS` to the running `dexcli dev` gRPC address. Start the Worker from `connectors/front/typescript`:

```sh
npm run example:contact
```

In another terminal with the same environment, edit `examples/find-contact/input.example.json` to use a contact email and start the Flow:

```sh
npm run example:contact:start -- examples/find-contact/input.example.json
```

The starter prints the contact result. The input file contains no token.

For a real Dex integration run against a local Front-compatible HTTP fixture, start `dexcli dev`, set `DEX_FLOW_SERVICE_ADDRESS`, and run `npm run test:integration`. The test binds the fixture to a free port and checks a found contact and an unwired authorization failure. Live Front contact lookup remains unverified.

Generate and lint the strict Flow Definition Graph 2.0 with Dex CLI 1.28.0 or newer. From `connectors/front/typescript`, make the package visible to the analyzer, then run:

```sh
npm run build
npm install --prefix examples/find-contact --no-save --no-package-lock --ignore-scripts .
cd examples/find-contact
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```
