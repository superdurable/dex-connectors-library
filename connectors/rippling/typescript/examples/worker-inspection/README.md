# Rippling TypeScript worker inspection

This runnable Flow calls `getWorker` through the static `rippling` connection and persists the selected worker status. Optional branches are unwired and fail the Flow when selected. Configure `api_token` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/rippling/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- <worker-id>`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service.

The same Worker serves [the worker page Flow](list-flow.ts). Start `RipplingWorkerListTypeScript` with `{"limit":2}`. Pass the returned `nextCursor` as `cursor` to read the next page. An empty cursor in the result marks the last page.

From the TypeScript package directory, validate the page Flow with `dexcli visualize examples/worker-inspection/list-flow.ts --schema-version 2.0 --lint app --json --open=false --out /tmp/rippling-typescript-page`.
