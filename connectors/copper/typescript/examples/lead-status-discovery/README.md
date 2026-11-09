# Copper TypeScript lead status discovery

This Flow calls `listLeadStatuses` with the static `copper` Dex Web connection. It stores lead status IDs and names in a Dex Attribute and completes with the typed result. In [Copper](https://app.copper.com/), open **Settings > Integrations > API Keys**, generate a key, and save it in `api_key` with the generating user's login email in `user_email`. Leave `baseUrl` blank for the Developer API v1 default.

With `DEX_PROJECT_*` and `DEX_FLOW_SERVICE_ADDRESS` set for a running Dex development environment, run from this package:

```sh
npm ci
npm run example:worker
```

In another terminal with the same project configuration, start lead status discovery:

```sh
npm run example:start
```

The Worker has no inbound listener. Optional outcome branches are unwired and fail the Flow when selected. For deterministic real-Dex verification, run `npm run test:integration` against `dexcli dev`. Live Copper authorization has not been tested.

## Strict FDG 2.0 generation

After building the package, make its own package import available to the analyzer:

```sh
npm run build
npm install --prefix examples/lead-status-discovery --no-save --no-package-lock --ignore-scripts .
cd examples/lead-status-discovery
dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/copper-ts-fdg --open=false
```

The generated FDG must report `valid: true` with no diagnostics.
