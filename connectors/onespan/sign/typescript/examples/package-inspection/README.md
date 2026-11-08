# OneSpan Sign TypeScript package inspection

This runnable Flow calls `getPackage` through the static `onespan-sign` connection and persists the selected package status. Optional branches are unwired and fail the Flow when selected. Configure `api_key` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/onespan/sign/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- <package-id>`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service.
