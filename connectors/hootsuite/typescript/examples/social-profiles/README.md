# Hootsuite social profiles Flow (TypeScript)

This runnable Dex Flow lists the profiles visible to the connected Hootsuite user and stores the page. Create the static `hootsuite` connection in Dex Web using the [connector guide](../../../README.md).

From this package, run `npm ci` and `npm run check`. For local FDG generation of the unreleased package, run `mkdir -p node_modules/@superdurable && ln -s ../.. node_modules/@superdurable/connector-hootsuite`, then `dexcli visualize examples/social-profiles/flow.ts --schema-version 2.0 --lint app --json --out /tmp/hootsuite-typescript-fdg --open=false`.

With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `npm run test:integration`. The test uses a synthetic token and local Hootsuite fixture to verify a 503 retry through a real Dex Worker and Client. With authorized provider credentials saved in Dex Web, run `npm run example:worker` and `npm run example:start` to list real profiles. Live Hootsuite authorization remains unverified.
