# Hesi department directory example

[`flow.ts`](flow.ts) defines `DepartmentCatalogFlow` with the static `hesi` connection. [`worker.ts`](worker.ts) loads that connection from Dex project configuration, and [`start.ts`](start.ts) starts a Flow with `start` and `count` page parameters.

Start `dexcli dev -open=false`, configure `hesi` in Dex Web Connections, then run the Worker and Client in separate terminals:

```sh
npm run example:worker --prefix connectors/hesi/typescript
npm run example:start --prefix connectors/hesi/typescript
```

The Flow persists only department IDs, names, codes, active flags, and pagination metadata. Optional rejection branches remain unwired, so a rejection fails the Flow for inspection. The integration test uses the same Flow against a local provider and real Dex Server; it does not require Hesi credentials.
