# Box TypeScript file download Flow

This runnable Flow downloads one file by numeric ID with the static `box` connection. Optional branches are unwired and fail the Flow when selected.

From `connectors/box/typescript`, generate and check the FDG 2.0 graph:

```bash
mkdir -p node_modules/@superdurable
test -e node_modules/@superdurable/connector-box || ln -s ../.. node_modules/@superdurable/connector-box
dexcli visualize examples/file-download/flow.ts --schema-version 2.0 --lint app --json --out /tmp/box-typescript-file-download-fdg --open=false
```

After configuring `box / box` in Dex Web as described in the [connector README](../../../README.md), load a Dex project and run `npm run example:worker`. Start `BoxDownloadFileTypeScript` with a unique Flow ID and `{"fileId":"12345"}`.

For a mock-backed real Dex check, start `dexcli dev` and run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8981 npm run example:integration`. Live Box authorization remains unverified.
