# Insightly TypeScript contact discovery

This Flow calls `listContacts` with the static `insightly` Dex Web connection. It stores a bounded contact page in a Dex Attribute and completes with the typed result. In [Insightly CRM](https://login.insightly.com/), open the top-right user icon > **User Settings > User Settings > API** and copy your personal API key into Dex secret `api_key`. Copy the v3.1 API URL into `baseUrl` if your pod differs from the displayed na1 default.

With `DEX_PROJECT_*` and `DEX_FLOW_SERVICE_ADDRESS` set for a running Dex development environment, run from this package:

```sh
npm ci
npm run example:worker
```

In another terminal with the same project configuration, start contact discovery:

```sh
npm run example:start
```

The starter uses `{ "top": 20, "skip": 0 }`. The Worker has no inbound listener. Optional outcome branches are unwired and fail the Flow when selected. For deterministic real-Dex verification, run `npm run test:integration` against `dexcli dev`. Live Insightly authorization has not been tested.

## Strict FDG 2.0 generation

After building the package, make its own package import available to the analyzer:

```sh
npm run build
npm install --prefix examples/contact-discovery --no-save --no-package-lock --ignore-scripts .
cd examples/contact-discovery
dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/insightly-ts-fdg --open=false
```

The generated FDG must report `valid: true` with no diagnostics.
