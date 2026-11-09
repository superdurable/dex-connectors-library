# Workday worker inspection (TypeScript)

This runnable Flow calls `newGetWorkerStep` with the static `workday` Dex Web connection and persists the returned worker WID and display name. Configure OAuth as described in the [connector guide](../../../README.md), run `npm run example:worker`, then set `WORKDAY_WORKER_ID` to an accessible worker's WID and run `npm run example:start`.

From the TypeScript package directory, link this unreleased local package, then generate and lint its FDG 2.0 graph:

```sh
mkdir -p node_modules/@superdurable
ln -s ../.. node_modules/@superdurable/connector-workday
dexcli visualize examples/worker-inspection/flow.ts --schema-version 2.0 --lint app --json --out /tmp/workday-typescript-fdg
```

With `dexcli dev` running and `DEX_FLOW_SERVICE_ADDRESS` set, `npm run test:integration` exercises the Flow through a real Dex Worker and Client against a local provider fixture. Live Workday authorization requires a user-controlled account.
