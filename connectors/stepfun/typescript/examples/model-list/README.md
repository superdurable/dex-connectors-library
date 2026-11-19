# StepFun model catalog example

[`flow.ts`](flow.ts) defines `ModelCatalogFlow` with the static `stepfun` connection. [`worker.ts`](worker.ts) loads that connection from Dex project configuration, and [`start.ts`](start.ts) starts a Flow with no operation parameters.

Start `dexcli dev`, configure `stepfun` in Dex Web Connections, then run the Worker and Client in separate terminals:

```sh
npm run example:worker --prefix connectors/stepfun/typescript
npm run example:start --prefix connectors/stepfun/typescript
```

The Flow persists only the bounded model list. Optional rejection branches remain unwired, so a rejection fails the Flow for inspection. The integration test uses the same Flow against a local provider and real Dex Server; it does not require StepFun credentials.
