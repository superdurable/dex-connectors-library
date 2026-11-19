# Vidu task status example

[`flow.ts`](flow.ts) defines a durable Flow with the static `shengshu` connection. [`worker.ts`](worker.ts) loads that connection from Dex project configuration, and [`start.ts`](start.ts) reads one existing task.

Start `dexcli dev -open=false`, configure `shengshu` in Dex Web Connections, then run the Worker and Client in separate terminals:

```sh
npm run example:worker --prefix connectors/shengshu/typescript
npm run example:start --prefix connectors/shengshu/typescript
```

The Flow persists only safe task status metadata. Optional rejection branches remain unwired, so a rejection fails the Flow for inspection.
