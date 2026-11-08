# Google Analytics 4 property Flow (TypeScript)

This runnable Dex Flow reads one property and persists its display metadata using the static `google-analytics-4` connection. Configure Google OAuth with the [connector guide](../../../README.md).

From this package run `npm ci` and `npm run check`. For local FDG generation, link the unreleased package with `mkdir -p node_modules/@superdurable && ln -s ../.. node_modules/@superdurable/connector-google-analytics`, then run `dexcli visualize examples/property/flow.ts --schema-version 2.0 --lint app --json --out /tmp/google-analytics-4-typescript-fdg --open=false`.

With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `npm run test:integration` to verify a 503 retry through a real Worker and Client. With a Google OAuth connection saved in Dex Web, run `npm run example:worker` and `npm run example:start` with `GA4_PROPERTY_ID` set to a numeric property ID. Live Google authorization remains unverified.
