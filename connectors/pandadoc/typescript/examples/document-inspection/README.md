# PandaDoc TypeScript document inspection

This runnable Flow calls `getDocument` through the static `pandadoc` connection and persists the selected document status. Optional branches are unwired and fail the Flow when selected. Configure `api_key` in Dex Web Connections as described in the [connector README](../../README.md).

From `connectors/pandadoc/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- <document-id>`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service.

Run the real Dex integration scenario with `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npm run test:integration`.
