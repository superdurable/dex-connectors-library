# Chuanglan balance Flow

`ChuanglanBalanceFlow` calls the `getBalance` Query once from a Dex Step and stores the remaining SMS count in the `chuanglan-balance` Attribute. The `fetched` branch completes the Flow. Optional failure branches remain unwired, so Dex records the failure. Credentials never enter Flow input.

Start `dexcli dev`, set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address, and configure the `chuanglan` connection in Dex Web. Set `DEX_CONNECTOR_CONFIG_FILE` to the mode-0600 local `connections.json` written by Dex Web. From `connectors/chuanglan/typescript` run:

```sh
npm run example:worker
```

In another terminal with the same environment, run:

```sh
npm run example:start
```

The starter prints the Flow ID, terminal status, and remaining message count. The example calls no SMS send operation.

Run the provider stand-in and real Dex Worker, Client, retry, and persistence test:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npm run test:integration
```

Generate strict FDG 2.0 from the runnable Flow:

```sh
npm run build
npm install --prefix examples/balance --no-save --no-package-lock --ignore-scripts .
cd examples/balance
dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

Live Chuanglan balance lookup has not been verified with an account.
