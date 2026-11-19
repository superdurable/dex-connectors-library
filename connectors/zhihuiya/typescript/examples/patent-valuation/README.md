# Zhihuiya patent valuation example

[`flow.ts`](flow.ts) defines `PatentValuationFlow` with the static `zhihuiya` connection. [`worker.ts`](worker.ts) loads that connection from Dex project configuration, and [`start.ts`](start.ts) starts a Flow for publication number `JP6012277B2`.

Start `dexcli dev -open=false`, configure `zhihuiya` in Dex Web Connections, then run the Worker and Client in separate terminals:

```sh
npm run example:worker --prefix connectors/zhihuiya/typescript
npm run example:start --prefix connectors/zhihuiya/typescript
```

The Flow persists only the bounded valuation output. Optional no-data and rejection branches remain unwired and fail the Flow for inspection. Its integration test uses the same Flow against a local provider and real Dex Server without Zhihuiya credentials.
