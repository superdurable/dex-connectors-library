# Eventbrite TypeScript organization inspection

This Flow calls `listOrganizations` with the static `eventbrite` Dex Web connection. It stores the organization page in a Dex Attribute and completes with the typed result. Configure the secret Private Token from Eventbrite Account Settings > Developer Links > API Keys in Dex Web Connections. Leave the API base URL blank for its displayed default.

With `DEX_PROJECT_*` and `DEX_FLOW_SERVICE_ADDRESS` set for a running Dex development environment, run from this package:

```sh
npm ci
npm run example:worker
```

In another terminal with the same project configuration, start a page:

```sh
npm run example:start -- 1
```

The Worker has no inbound listener. Optional outcome branches are unwired and fail the Flow when selected. For deterministic real-Dex verification, run `npm run test:integration` against `dexcli dev`. Live Eventbrite authorization has not been tested.

## Strict FDG 2.0 generation

After building the package, make its own package import available to the analyzer:

```sh
npm run build
npm install --prefix examples/organization-inspection --no-save --no-package-lock --ignore-scripts .
cd examples/organization-inspection
dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/eventbrite-ts-fdg --open=false
```

The generated FDG must report `valid: true` with no diagnostics.
