# Kit TypeScript subscriber inspection

This Flow calls `listSubscribers` with the static `kit` Dex Web connection. It stores the subscriber page in a Dex Attribute and completes with the typed result. In [Kit Developer settings](https://app.kit.com/account_settings/developer_settings), create a V4 API Key and copy it into the secret `api_key` field. Leave `baseUrl` blank for the API v4 default.

With `DEX_PROJECT_*` and `DEX_FLOW_SERVICE_ADDRESS` set for a running Dex development environment, run from this package:

```sh
npm ci
npm run example:worker
```

In another terminal with the same project configuration, start the first subscriber page:

```sh
npm run example:start
```

Pass an optional cursor after `--` to read a later page. The Worker has no inbound listener. Optional outcome branches are unwired and fail the Flow when selected. For deterministic real-Dex verification, run `npm run test:integration` against `dexcli dev`. Live Kit authorization has not been tested.

## Strict FDG 2.0 generation

After building the package, make its own package import available to the analyzer:

```sh
npm run build
npm install --prefix examples/subscriber-inspection --no-save --no-package-lock --ignore-scripts .
cd examples/subscriber-inspection
dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/kit-ts-fdg --open=false
```

The generated FDG must report `valid: true` with no diagnostics.
