# Instagram media listing Flow (TypeScript)

This runnable Dex Flow reads a media page using the static `instagram-business` connection. It stores media IDs, captions, media types, timestamps, permalinks, and a cursor. See the [connector guide](../../../README.md) for User token setup.

From this package, run `npm ci` and `npm run check`. Before local FDG generation, link the unreleased package with `mkdir -p node_modules/@superdurable && ln -s ../.. node_modules/@superdurable/connector-meta-instagram-business`. Then run `dexcli visualize examples/media-list/flow.ts --schema-version 2.0 --lint app --json --out /tmp/instagram-typescript-fdg --open=false`.

With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `npm run test:integration`. The test uses a synthetic User token and local provider transport to verify a 503 retry through a real Dex Worker and Client. For a real account, save the User token in Dex Web, run `npm run example:worker`, and start a Flow with `META_INSTAGRAM_USER_ID=<numeric Instagram account ID> npm run example:start`. Live Meta authorization remains unverified.
