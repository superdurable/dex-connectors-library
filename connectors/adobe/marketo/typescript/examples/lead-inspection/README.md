# Marketo TypeScript lead inspection

This runnable Flow calls `getLead` through the static `adobe-marketo` connection and persists the lead profile. Optional branches are unwired and fail the Flow when selected. Configure `client_id` and secret `client_secret` using the [connection guide](../../../README.md); leave `access_token` blank for automatic renewal.

From `connectors/adobe/marketo/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- 318581`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service.

Generate and lint the Flow Definition Graph 2.0 from the same directory:

```bash
dexcli visualize examples/lead-inspection/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/adobe-marketo-typescript-fdg --open=false
```
