# Campaign Monitor TypeScript client discovery

This Flow calls `listClients` with the static `campaignmonitor` Dex Web connection. It stores client IDs and names in a Dex Attribute and completes with the typed result. Open [Campaign Monitor API keys](https://help.campaignmonitor.com/articles/Knowledge/api-keys) through Profile image > Account settings > API keys and save the full key in secret `api_key`. Leave configuration fields blank for their displayed defaults.

With `DEX_PROJECT_*` and `DEX_FLOW_SERVICE_ADDRESS` set for a running Dex development environment, run from this package:

```sh
npm ci
npm run example:worker
```

In another terminal with the same project configuration, start the Flow:

```sh
npm run example:start
```

The Worker has no inbound listener. Optional outcome branches are unwired and fail the Flow when selected. Run `npm run test:integration` against `dexcli dev` for deterministic real-Dex verification. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

After building the package, make its own package import available to the analyzer:

```sh
npm run build
npm install --prefix examples/client-discovery --no-save --no-package-lock --ignore-scripts .
cd examples/client-discovery
dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/campaignmonitor-ts-fdg --open=false
```

The generated FDG must report `valid: true` with no diagnostics.
