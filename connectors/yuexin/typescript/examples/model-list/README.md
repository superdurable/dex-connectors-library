# Yuexin model catalog example

[`flow.ts`](flow.ts) defines `ModelCatalogFlow` with the static `yuexin` connection. [`worker.ts`](worker.ts) loads that connection from Dex project configuration, and [`start.ts`](start.ts) starts a Flow with no operation parameters.

Start `dexcli dev`, configure `yuexin` in Dex Web Connections, then run the Worker and Client in separate terminals:

```sh
npm run example:worker --prefix connectors/yuexin/typescript
npm run example:start --prefix connectors/yuexin/typescript
```

The Flow persists only the bounded model list. Optional rejection branches remain unwired, so a rejection fails the Flow for inspection. The integration test uses the same Flow against a local provider and real Dex Server; it does not require Yuexin credentials.
