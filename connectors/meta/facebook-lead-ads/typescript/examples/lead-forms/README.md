# Facebook Lead Ads lead-form Flow (TypeScript)

This runnable Dex Flow reads a Page of lead-form IDs, names, and statuses using the static `facebook-lead-ads` connection. It excludes form questions and lead answers. See the [connector README](../../../README.md) for Page token setup.

From `connectors/meta/facebook-lead-ads/typescript`, install with `npm ci`, then run `npm run check`. Before local FDG generation, link the unreleased package into `node_modules` with `ln -s ../.. node_modules/@superdurable/connector-meta-facebook-lead-ads`. Then run `dexcli visualize examples/lead-forms/flow.ts --schema-version 2.0 --lint app --json --out /tmp/facebook-lead-ads-typescript-fdg --open=false`.

With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `npm run test:integration`. The test uses a synthetic Page token and a local Meta transport to verify a 503 retry through a real Dex Worker and Client. For a real Page, save the Page token in Dex Web, run `npm run example:worker`, and start a Flow with `META_PAGE_ID=<numeric Page ID> npm run example:start`. Live Meta authorization remains unverified.
