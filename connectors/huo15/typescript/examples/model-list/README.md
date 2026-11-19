# Huo15 model catalog example

[`flow.ts`](flow.ts) defines `ModelCatalogFlow` with the static `huo15` connection. [`worker.ts`](worker.ts) loads that connection from Dex project configuration, and [`start.ts`](start.ts) starts a Flow with no operation parameters.

Start `dexcli dev -open=false`, configure `huo15` in Dex Web Connections, then run the Worker and Client in separate terminals:

```sh
npm run example:worker --prefix connectors/huo15/typescript
npm run example:start --prefix connectors/huo15/typescript
```

The Flow persists only the bounded model list. Optional rejection branches remain unwired, so a rejection fails the Flow for inspection. The integration test uses the same Flow against a local provider and real Dex Server; it does not require Huo15 credentials.
