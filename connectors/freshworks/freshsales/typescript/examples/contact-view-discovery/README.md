# Freshsales TypeScript contact discovery

This Flow calls `listContactViews` with the static `freshsales` Dex Web connection. It stores contact views in a Dex Attribute and completes with the typed result. In [Freshsales CRM](https://app.freshworks.com/), open the profile picture > Profile Settings > API Settings and copy your personal API key into Dex secret `api_key`. Copy the Bundle alias URL ending in `/crm/sales/api` into `baseUrl`.

With `DEX_PROJECT_*` and `DEX_FLOW_SERVICE_ADDRESS` set for a running Dex development environment, run from this package:

```sh
npm ci
npm run example:worker
```

In another terminal with the same project configuration, start contact discovery:

```sh
npm run example:start
```

The starter uses `{}`. The Worker has no inbound listener. Optional outcome branches are unwired and fail the Flow when selected. For deterministic real-Dex verification, run `npm run test:integration` against `dexcli dev`. Live Freshsales authorization has not been tested.

## Strict FDG 2.0 generation

After building the package, make its own package import available to the analyzer:

```sh
npm run build
npm install --prefix examples/contact-view-discovery --no-save --no-package-lock --ignore-scripts .
cd examples/contact-view-discovery
dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/freshsales-ts-fdg --open=false
```

The generated FDG must report `valid: true` with no diagnostics.
