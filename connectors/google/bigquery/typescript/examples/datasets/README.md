# Dataset discovery Flow (TypeScript)

This runnable Flow calls `listDatasets` through the static `google-bigquery` Dex Web connection. Configure OAuth in the [connector guide](../../../README.md), run `npm run example:worker`, then `npm run example:start`. The result is stored in a declared Attribute.

For local FDG 2.0 generation, link this unreleased package under `node_modules/@superdurable/connector-google-bigquery`, then run `dexcli visualize examples/datasets/flow.ts --schema-version 2.0 --lint app --json --out /tmp/google-bigquery-typescript-fdg --open=false` from the TypeScript package directory.

With `dexcli dev` running, `npm run test:integration` exercises the Flow through a real Dex Worker and Client against a local provider fixture. Live BigQuery authorization has not been verified.
