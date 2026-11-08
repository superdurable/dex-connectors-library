# ActiveCampaign TypeScript contact inspection

This Flow calls `listContacts` with the static `activecampaign` Dex Web connection. It stores the contact page in a Dex Attribute and completes with the typed result. In ActiveCampaign, open Settings > Developer and copy the account API URL, ending in `/api/3`, into `baseUrl`; copy the API Key into the secret `api_key` field. Both values are required.

With `DEX_PROJECT_*` and `DEX_FLOW_SERVICE_ADDRESS` set for a running Dex development environment, run from this package:

```sh
npm ci
npm run example:worker
```

In another terminal with the same project configuration, start a contact page at a nonnegative offset:

```sh
npm run example:start -- 0
```

The Worker has no inbound listener. Optional outcome branches are unwired and fail the Flow when selected. For deterministic real-Dex verification, run `npm run test:integration` against `dexcli dev`. Live ActiveCampaign authorization has not been tested.

## Strict FDG 2.0 generation

After building the package, make its own package import available to the analyzer:

```sh
npm run build
npm install --prefix examples/contact-inspection --no-save --no-package-lock --ignore-scripts .
cd examples/contact-inspection
dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/activecampaign-ts-fdg --open=false
```

The generated FDG must report `valid: true` with no diagnostics.
