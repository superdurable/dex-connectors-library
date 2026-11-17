# CTYun knowledge-base directory example

1. Start a Dex development service and create a Dex Web Connection named `ctyun` with the secret AK, SK, and `tenantId` from the [CTYun console](https://www.ctyun.cn/).
2. In `connectors/ctyun/typescript`, run `npm install` and `npm run example:worker`.
3. In another terminal, run `npm run example:start`.

`flow.ts` defines the static connection, `listKnowledgeBases` Step, and persisted resource Attribute. `worker.ts` loads project configuration and serves the Flow. `start.ts` starts the Flow and prints the completed list.

For local integration verification, start `dexcli dev`, set `DEX_FLOW_SERVICE_ADDRESS`, and run `npm run test:integration`. This uses a local provider fixture; live CTYun access requires a user-owned connection.

Generate strict FDG 2.0 from the installed example package:

```sh
npm run build
npm install --prefix examples/knowledge-bases --no-save --no-package-lock --ignore-scripts .
cd examples/knowledge-bases
dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```
