# Plaid TypeScript accounts inspection

This runnable Flow calls `getAccounts` through the static `plaid` connection and persists the selected account routing summaries. Optional branches are unwired and fail the Flow when selected. Configure `client_id`, `secret`, and `access_token` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/plaid/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- <accounts-id>`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service.
