# Dada order status example

1. Start a Dex development service and create a Dex Web Connection named `dada` with `appKey`, secret `appSecret`, and `sourceID` from the [Dada Open Platform](https://newopen.imdada.cn).
2. In `connectors/dada/typescript`, run `npm install` and `npm run example:worker`.
3. In another terminal, run `npm run example:start -- 20170301000001`, replacing the value with a third-party order ID.

`flow.ts` defines the static connection, `queryOrderStatus` Step, and persisted status Attribute. `worker.ts` loads project configuration. `start.ts` starts the Flow and prints the completed status without rider personal data.

For local integration verification, start `dexcli dev`, set `DEX_FLOW_SERVICE_ADDRESS`, and run `npm run test:integration`. This uses a local provider fixture; live Dada access requires a user-owned connection.

Generate strict FDG 2.0 from the installed example package:

```sh
npm run build
npm install --prefix examples/order-status --no-save --no-package-lock --ignore-scripts .
cd examples/order-status
dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```
