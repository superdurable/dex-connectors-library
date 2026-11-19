# DataEyes web reader example

1. Start a Dex development service and configure a Dex Web Connection named `dataeyes` with the secret search/reading Key from [MCP产品 → 接入与密钥](https://doc.shuyanai.com/doc-8441886).
2. In `connectors/dataeyes/typescript`, run `npm install` and `npm run example:worker`.
3. In another terminal, run `npm run example:start -- https://example.com`.

`flow.ts` defines the static connection, `readWebPage` Step, and persisted page Attribute. `worker.ts` loads project configuration and serves the Flow. `start.ts` starts a Flow for the supplied public HTTPS URL and prints the completed output.

For local integration verification, start `dexcli dev`, set `DEX_FLOW_SERVICE_ADDRESS`, and run `npm run test:integration`. The test uses a local provider fixture; live DataEyes access requires a user-owned connection.

Generate a strict FDG 2.0 from the installed example package:

```sh
npm run build
npm install --prefix examples/web-reader --no-save --no-package-lock --ignore-scripts .
cd examples/web-reader
dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```
