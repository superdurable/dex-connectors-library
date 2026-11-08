# Personio TypeScript person inspection

This runnable Flow calls `getPerson` through the static `personio` connection and persists the person profile. Optional branches are unwired and fail the Flow when selected. Configure `client_id` and secret `client_secret` using the [connection guide](../../../README.md); leave `access_token` blank for automatic renewal.

From `connectors/personio/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- 3003`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service.

Generate and lint the Flow Definition Graph 2.0 from the same directory:

```bash
mkdir -p node_modules/@superdurable
ln -s ../.. node_modules/@superdurable/connector-personio
dexcli visualize examples/person-inspection/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/personio-typescript-fdg --open=false
```
