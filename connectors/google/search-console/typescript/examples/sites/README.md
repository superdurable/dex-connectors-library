# Google Search Console sites Flow (TypeScript)

This runnable Dex Flow lists accessible property URLs and permissions using the static `google-search-console` connection. Configure Google OAuth with the [connector guide](../../../README.md).

From this package run `npm ci` and `npm run check`. For local FDG generation, link the unreleased package with `mkdir -p node_modules/@superdurable && ln -s ../.. node_modules/@superdurable/connector-google-search-console`, then run `dexcli visualize examples/sites/flow.ts --schema-version 2.0 --lint app --json --out /tmp/search-console-typescript-fdg --open=false`.

With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `npm run test:integration` to verify a 503 retry through a real Worker and Client. With a Google OAuth connection saved in Dex Web, run `npm run example:worker` and `npm run example:start` to list real properties. Live Google authorization remains unverified.
