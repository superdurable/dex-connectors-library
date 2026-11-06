# Read a Mailchimp audience contact

`GetMemberFlow` reads one contact with the static `mailchimp-audience` connection and persists its safe input and result. Configure that API-key connection in Dex Web, start `dexcli dev`, and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address. Set `DEX_CONNECTOR_CONFIG_FILE` to the local Dex Web `connections.json` file. From `connectors/intuit/mailchimp/typescript`, run the Worker and starter in separate terminals:

```sh
npm run example:get
npm run example:get:start -- examples/get-member/input.example.json
```

Replace the example audience ID and email address with values in the connected account. The starter prints the contact; no API key belongs in the input file. For local Dex verification with a Mailchimp stand-in, run `DEX_FLOW_SERVICE_ADDRESS=<dex-host:port> npm run test:integration`.

Generate and lint the strict Flow Definition Graph 2.0 with Dex CLI 1.28.0 or newer:

```sh
npm run build
npm install --prefix examples/get-member --no-save --no-package-lock --ignore-scripts .
cd examples/get-member
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

Live Mailchimp authorization and provider behavior have not been verified for this TypeScript example.
