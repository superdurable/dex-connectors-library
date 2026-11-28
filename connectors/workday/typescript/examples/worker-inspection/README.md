# Workday worker inspection (TypeScript)

This runnable Flow calls `newGetWorkerStep` with the static `workday` Dex Web connection and persists the returned worker WID and display name. Configure OAuth as described in the [connector guide](../../../README.md), run `npm run example:worker`, then set `WORKDAY_WORKER_ID` to an accessible worker's WID and run `npm run example:start`.

From the TypeScript package directory, link this unreleased local package, then generate and lint its FDG 2.0 graph:

```sh
mkdir -p node_modules/@superdurable
ln -s ../.. node_modules/@superdurable/connector-workday
dexcli visualize examples/worker-inspection/flow.ts --schema-version 2.0 --lint app --open=false --json --out /tmp/workday-typescript-fdg
dexcli visualize examples/worker-inspection/list-flow.ts --schema-version 2.0 --lint app --open=false --json --out /tmp/workday-typescript-page-fdg
```

With `dexcli dev` running and `DEX_FLOW_SERVICE_ADDRESS` set, `npm run test:integration` exercises the Flow through a real Dex Worker and Client against a local provider fixture. Live Workday authorization requires a user-controlled account.

The same worker also serves [WorkdayWorkerListTypeScript](list-flow.ts) using `newListWorkersStep`. Start that Flow in Dex Web with a zero-based offset, a page size up to 100, and an optional search prefix. For example, the input `{"limit":2,"offset":5,"search":"Ada"}` requests two matches starting at offset five. The Flow returns the total and the matching WIDs and display names.
