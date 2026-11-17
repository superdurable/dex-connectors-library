# Cloopen balance Flow

`CloopenBalanceFlow` calls the `getBalance` Query from a Dex Step, stores the exact provider balance string in the `cloopen-balance` Attribute, and completes on `fetched`. Optional failure branches stay unwired so Dex records the failure. Both credentials remain outside Flow input and state.

Start `dexcli dev`, set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address, and configure the static `cloopen` connection in Dex Web. Set `DEX_CONNECTOR_CONFIG_FILE` to the mode-0600 local `connections.json` written by Dex Web. From `connectors/cloopen/typescript` run:

```sh
npm run example:worker
```

In another terminal with the same environment, run:

```sh
npm run example:start
```

The starter prints the Flow ID, terminal status, and exact balance string. Input is `{}` and carries no credentials.

Run the signed stand-in provider and real Dex Worker, Client, retry, and persistence test:

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

Live Cloopen authorization and production balance lookup remain unverified.
