# Heap TypeScript user-properties Flow

This runnable Flow calls `setUserProperties` through the static `heap` connection, stores acceptance in an Attribute, and completes. Optional branches are unwired and fail the Flow when selected. Enter the numeric `appId` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/heap/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- member-1 Pro`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Replace the sample identity and plan with a permanent unique identity and a non-secret value.

Generate and lint the Flow Definition Graph 2.0 from the same directory. The local package link lets Dex CLI resolve the manifest before this package is published:

```bash
ln -s ../.. node_modules/@superdurable/connector-heap
dexcli visualize examples/user-properties/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/heap-typescript-fdg --open=false
```
