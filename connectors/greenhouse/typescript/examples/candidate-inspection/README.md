# Greenhouse TypeScript candidate inspection

This runnable Flow calls `getCandidate` through the static `greenhouse` connection and persists the candidate profile. Optional branches are unwired and fail the Flow when selected. Configure `client_id` and secret `client_secret` using the [connection guide](../../../README.md); leave `access_token` blank for automatic renewal.

From `connectors/greenhouse/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- 53883394`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service.

Generate and lint the Flow Definition Graph 2.0 from the same directory:

```bash
dexcli visualize examples/candidate-inspection/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/greenhouse-typescript-fdg --open=false
```
