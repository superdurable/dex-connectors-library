# Google Tag Manager accounts Flow (TypeScript)

This runnable Dex Flow lists accessible accounts and persists their paths and display names using the static `google-tag-manager` connection. Configure Google OAuth with the [connector guide](../../../README.md).

From this package run `npm ci` and `npm run check`. For local FDG generation, link the unreleased package with `mkdir -p node_modules/@superdurable && ln -s ../.. node_modules/@superdurable/connector-google-tag-manager`, then run `dexcli visualize examples/accounts/flow.ts --schema-version 2.0 --lint app --json --out /tmp/google-tag-manager-typescript-fdg --open=false`.

With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `npm run test:integration` to verify a 503 retry through a real Worker and Client. With a Google OAuth connection saved in Dex Web, run `npm run example:worker` and `npm run example:start`. Live Google authorization remains unverified.
