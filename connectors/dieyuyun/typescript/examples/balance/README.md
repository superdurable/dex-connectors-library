# Dieyuyun balance example

1. Start a Dex development service and create a Dex Web Connection named `dieyuyun` with the secret API Key from [Key Management](https://open-console.dieyuyun.com/).
2. In `connectors/dieyuyun/typescript`, run `npm install` and `npm run example:worker`.
3. In another terminal, run `npm run example:start`.

`flow.ts` defines the static connection, `getAccountBalance` Step, and persisted balance Attribute. `worker.ts` loads project configuration. `start.ts` starts the Flow and prints the exact CNY decimal text.

For local integration verification, start `dexcli dev -open=false`, set `DEX_FLOW_SERVICE_ADDRESS`, and run `npm run test:integration`. This uses a local provider fixture; live Dieyuyun access requires a user-owned connection.

Generate strict FDG 2.0 from the installed example package:

```sh
npm run build
npm install --prefix examples/balance --no-save --no-package-lock --ignore-scripts .
cd examples/balance
dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```
