# Facebook Pages post feed Flow (TypeScript)

This runnable Dex Flow reads a Page feed using the static `facebook-pages` connection. It stores post IDs, messages, timestamps, permalinks, and a cursor. Comments and reactions are excluded. See the [connector README](../../../README.md) for Page token setup.

From `connectors/meta/facebook-pages/typescript`, install with `npm ci`, then run `npm run check`. Before local FDG generation, link the unreleased package into `node_modules` with `ln -s ../.. node_modules/@superdurable/connector-meta-facebook-pages`. Then run `dexcli visualize examples/page-posts/flow.ts --schema-version 2.0 --lint app --json --out /tmp/facebook-pages-typescript-fdg --open=false`.

With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `npm run test:integration`. The test uses a synthetic Page token and a local Meta transport to verify a 503 retry through a real Dex Worker and Client. For a real Page, save the Page token in Dex Web, run `npm run example:worker`, and start a Flow with `META_PAGE_ID=<numeric Page ID> npm run example:start`. Live Meta authorization remains unverified.
