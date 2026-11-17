# Chukou1 warehouse list Flow

`Chukou1WarehouseListFlow` calls the `listWarehouses` Query from a Dex Step, stores the typed warehouse page in the `chukou1-warehouses` Attribute, and completes on the `listed` branch. Optional failure branches stay unwired so Dex records the failure. The protected token never enters Flow input.

Start `dexcli dev`, set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address, and configure the static `chukou1` connection in Dex Web. Set `DEX_CONNECTOR_CONFIG_FILE` to the mode-0600 local `connections.json` written by Dex Web. From `connectors/chukou1/typescript` run:

```sh
npm run example:worker
```

In another terminal with the same environment, run:

```sh
npm run example:start
```

The starter prints the Flow ID, terminal status, and authorized warehouse metadata. Input is empty and carries no credentials.

Run the stand-in provider and real Dex Worker, Client, retry, and persistence test:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npm run test:integration
```

Generate strict FDG 2.0 from the runnable Flow:

```sh
npm run build
npm install --prefix examples/warehouse-list --no-save --no-package-lock --ignore-scripts .
cd examples/warehouse-list
dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

Live Chukou1 authorization and production warehouse listing remain unverified.
