# Jotform TypeScript form inspection

This Flow calls `listForms` with the static `jotform` Dex Web connection. It persists one form page in a Dex Attribute and completes with the typed result. Optional provider branches are unwired and fail the Flow.

Configure a read-only API key per the [connector guide](../../../README.md). With `DEX_PROJECT_*` and `DEX_FLOW_SERVICE_ADDRESS` set for a running Dex development environment, run from this package:

```sh
npm ci
npm run example:worker
```

In another terminal with the same project configuration, run `npm run example:start`. The Worker has no inbound listener. For deterministic real Dex verification, run `npm run test:integration` against `dexcli dev`. Live provider authorization remains unverified.

## FDG 2.0

After building this package, make its own package import available to the analyzer:

```sh
npm run build
npm install --prefix examples/form-inspection --no-save --no-package-lock --ignore-scripts .
cd examples/form-inspection
dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/jotform-ts-fdg --open=false
```
