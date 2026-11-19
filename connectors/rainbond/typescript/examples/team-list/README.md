# Rainbond team-list example

[`flow.ts`](flow.ts) defines `TeamCatalogFlow` with the static `rainbond` connection. [`worker.ts`](worker.ts) loads that connection from Dex project configuration. [`start.ts`](start.ts) starts one page request and persists the result.

Start `dexcli dev -open=false`, configure `rainbond` in Dex Web Connections, then run the Worker and Client in separate terminals:

```sh
npm run example:worker --prefix connectors/rainbond/typescript
npm run example:start --prefix connectors/rainbond/typescript
```

The Flow persists only team IDs, names, active flags, and total count. Optional rejection branches remain unwired, so a rejection fails the Flow for inspection. The integration test uses the same Flow against a local provider and real Dex Server without Rainbond credentials.
