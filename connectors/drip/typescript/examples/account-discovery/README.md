# Drip TypeScript account discovery

This Flow calls `listAccounts` with the static `drip` Dex Web connection. It stores account IDs and names in a Dex Attribute and completes with the typed result. Copy the personal API Token from [your Drip user profile](https://www.getdrip.com/user/edit) into the secret `api_key` field. Leave `baseUrl` blank for the API v2 default.

With `DEX_PROJECT_*` and `DEX_FLOW_SERVICE_ADDRESS` set for a running Dex development environment, run from this package:

```sh
npm ci
npm run example:worker
```

In another terminal with the same project configuration, start account discovery:

```sh
npm run example:start
```

The Worker has no inbound listener. Optional outcome branches are unwired and fail the Flow when selected. For deterministic real-Dex verification, run `npm run test:integration` against `dexcli dev`. Live Drip authorization has not been tested.

## Strict FDG 2.0 generation

After building the package, make its own package import available to the analyzer:

```sh
npm run build
npm install --prefix examples/account-discovery --no-save --no-package-lock --ignore-scripts .
cd examples/account-discovery
dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/drip-ts-fdg --open=false
```

The generated FDG must report `valid: true` with no diagnostics.
