# Personio TypeScript person directory

This runnable Flow calls `listPersons` through the static `personio` connection and persists a filtered page with its next cursor. Optional branches are unwired and fail the Flow when selected. Configure `client_id` and secret `client_secret` using the [connection guide](../../../README.md); leave `access_token` blank for automatic renewal.

From `connectors/personio/typescript`, run `npm run directory:worker`. In another terminal, run `npm run directory:start`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service.

Generate and lint the Flow Definition Graph 2.0 from the same directory:

```bash
mkdir -p node_modules/@superdurable
ln -s ../.. node_modules/@superdurable/connector-personio
dexcli visualize examples/person-directory/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/personio-list-typescript-fdg --open=false
```
