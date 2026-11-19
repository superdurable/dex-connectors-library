# Zilliz cluster list example

[`flow.ts`](flow.ts) defines `ClusterListFlow` with the static `zilliz` connection. [`worker.ts`](worker.ts) loads that connection from Dex project configuration, and [`start.ts`](start.ts) starts a Flow with empty operation input.

Start `dexcli dev -open=false`, configure `zilliz` in Dex Web Connections, then run the Worker and Client in separate terminals:

```sh
npm run example:worker --prefix connectors/zilliz/typescript
npm run example:start --prefix connectors/zilliz/typescript
```

The Flow persists only the bounded cluster page. Optional rejection branches remain unwired. Its integration test uses the same Flow against a local provider and real Dex Server without Zilliz credentials.
