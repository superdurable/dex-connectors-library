# Facebook Pages text publishing Flow (TypeScript)

This runnable Dex Flow publishes one plain-text Page post using the static `facebook-pages` connection and stores its confirmed post ID. Optional `providerRejected` and `uncertain` branches are unwired and fail the Flow. Inspect the Page feed before manually retrying an uncertain publish. See the [connector README](../../../README.md) for Page token setup and the `pages_manage_posts` permission.

From `connectors/meta/facebook-pages/typescript`, install with `npm ci`, then run `npm run check`. Before local FDG generation, link the unreleased package into `node_modules` with `ln -s ../.. node_modules/@superdurable/connector-meta-facebook-pages`. Then run `dexcli visualize examples/publish-post/flow.ts --schema-version 2.0 --lint app --json --out /tmp/facebook-pages-typescript-publish-fdg --open=false`.

With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `npm run test:integration`. The test uses a synthetic Page token and a local Meta transport to verify a 503 retry and an uncertain HTTP 500 through a real Dex Worker and Client. For a real Page, save the Page token in Dex Web, run `npm run example:publish-worker`, and start a Flow with `META_PAGE_ID=<numeric Page ID> META_POST_MESSAGE='Hello from Dex' npm run example:publish-start`. Live Meta authorization remains unverified.
