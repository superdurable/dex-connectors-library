# Zendesk Sell TypeScript lead discovery

This Flow calls `listLeads` with the static `zendesk-sell` Dex Web connection. It stores one bounded lead page in a Dex Attribute and completes with the typed result. Create a personal access token in [Zendesk Sell](https://app.futuresimple.com/) under Integrations > OAuth > Access Tokens > Add User Token, then save it in Dex `access_token`.

With `DEX_PROJECT_*` and `DEX_FLOW_SERVICE_ADDRESS` set for a running Dex development environment, run from this package:

```sh
npm ci
npm run example:worker
```

In another terminal with the same project configuration, start lead discovery:

```sh
npm run example:start
```

The starter uses `{ "page": 1, "perPage": 20 }`. The Worker has no inbound listener. Optional outcome branches are unwired and fail the Flow when selected. For deterministic real-Dex verification, run `npm run test:integration` against `dexcli dev`. Live provider authorization has not been tested.

## Strict FDG 2.0 generation

After building the package, make its own package import available to the analyzer:

```sh
npm run build
npm install --prefix examples/lead-discovery --no-save --no-package-lock --ignore-scripts .
cd examples/lead-discovery
dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/zendesk-sell-ts-fdg --open=false
```

The generated FDG must report `valid: true` with no diagnostics.
