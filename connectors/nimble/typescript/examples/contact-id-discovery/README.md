# Nimble TypeScript contact ID discovery

This Flow calls `listContactIDs` with the static `nimble` Dex Web connection. It stores a bounded contact ID page in a Dex Attribute and completes with the typed result. In [Nimble CRM](https://app.nimble.com/), open **Settings > API Token > Generate New Token** as an administrator and save the generated token in Dex secret `api_key`. An administrator may need to grant API access under **Settings > Users**.

With `DEX_PROJECT_*` and `DEX_FLOW_SERVICE_ADDRESS` set for a running Dex development environment, run from this package:

```sh
npm ci
npm run example:worker
```

In another terminal with the same project configuration, start contact ID discovery:

```sh
npm run example:start
```

The starter uses `{ "page": 1, "perPage": 20 }`. The Worker has no inbound listener. Optional outcome branches are unwired and fail the Flow when selected. For deterministic real-Dex verification, run `npm run test:integration` against `dexcli dev`. Live Nimble authorization has not been tested.

## Strict FDG 2.0 generation

After building the package, make its own package import available to the analyzer:

```sh
npm run build
npm install --prefix examples/contact-id-discovery --no-save --no-package-lock --ignore-scripts .
cd examples/contact-id-discovery
dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/nimble-ts-fdg --open=false
```

The generated FDG must report `valid: true` with no diagnostics.
