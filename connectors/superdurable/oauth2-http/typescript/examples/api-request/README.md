# Request an OAuth-protected API

`APIRequestFlow` sends one GET request to a path under the connection's
`apiBaseUrl`. Its static Dex Web Connection name is `oauth2-http-api`. The
Flow input contains only a path, never a token or client secret.

Create and authorize that Connection using the provider's authorization and
token endpoints, scopes, API base URL, OAuth client ID, and client secret.
Set `DEX_CONNECTOR_CONFIG_FILE` to the local mode-0600 Connections file and
`DEX_FLOW_SERVICE_ADDRESS` to the Dex gRPC address. Start `dexcli dev`, then
run these commands from `connectors/superdurable/oauth2-http/typescript`:

```sh
npm run example:request
npm run example:request:start -- examples/api-request/input.example.json
```

The second command runs in another terminal and prints the bounded API
answer. Edit `input.example.json` to a provider path such as `/userinfo`.

Run the local real Dex integration test with a stand-in API:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:40910 npm run test:integration
```

Generate and lint the strict Flow Definition Graph 2.0 from this directory:

```sh
npm run build
npm install --prefix examples/api-request --no-save --no-package-lock --ignore-scripts .
cd examples/api-request
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

The local integration test uses an HTTP fixture. Live provider authorization
and token refresh through Dex Web remain unverified.
