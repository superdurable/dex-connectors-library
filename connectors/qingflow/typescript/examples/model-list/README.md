# Qingflow application catalog example

[`flow.ts`](flow.ts) defines `ApplicationCatalogFlow` with the static `qingflow` connection. [`worker.ts`](worker.ts) loads that connection from Dex project configuration, and [`start.ts`](start.ts) starts a Flow with no operation parameters.

Start `dexcli dev -open=false`, configure `qingflow` in Dex Web Connections, then run the Worker and Client in separate terminals:

```sh
npm run example:worker --prefix connectors/qingflow/typescript
npm run example:start --prefix connectors/qingflow/typescript
```

The Flow persists only the bounded application list. Optional rejection branches remain unwired, so a rejection fails the Flow for inspection. The integration test uses the same Flow against a local provider and real Dex Server; it does not require Qingflow credentials.
