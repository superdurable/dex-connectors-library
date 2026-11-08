# Odoo TypeScript partner search

This runnable Flow calls `searchPartners` through the static `odoo` connection and persists the partner page. Optional branches are unwired and fail the Flow when selected. Configure `baseUrl`, `database`, and secret `apiKey` using the [connection guide](../../../README.md).

From `connectors/odoo/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- "%Acme%" 5`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service.

Generate and lint the Flow Definition Graph 2.0 from the same directory:

```bash
dexcli visualize examples/partner-search/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/odoo-typescript-fdg --open=false
```
