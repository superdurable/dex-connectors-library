# Dedao task status example

1. Start a Dex development service and create a Dex Web Connection named `dedao` with the secret API Key and Client ID from the [Dedao Open Platform](https://www.biji.com/openapi).
2. In `connectors/dedao/typescript`, run `npm install` and `npm run example:worker`.
3. In another terminal, run `npm run example:start -- 69c3995e99f5a67e`, replacing the value with a note-save `data.tasks[0].task_id`.

`flow.ts` defines the static connection, `getNoteTaskStatus` Step, and persisted status Attribute. `worker.ts` loads project configuration. `start.ts` starts the Flow and prints the completed status without note content.

For local integration verification, start `dexcli dev -open=false`, set `DEX_FLOW_SERVICE_ADDRESS`, and run `npm run test:integration`. This uses a local provider fixture; live Dedao access requires a user-owned connection.

Generate strict FDG 2.0 from the installed example package:

```sh
npm run build
npm install --prefix examples/task-status --no-save --no-package-lock --ignore-scripts .
cd examples/task-status
dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```
