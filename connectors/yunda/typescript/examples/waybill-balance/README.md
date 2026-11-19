# Yunda waybill balance example

[`flow.ts`](flow.ts) defines `WaybillBalanceFlow` with the static `yunda` connection. [`worker.ts`](worker.ts) loads that connection from Dex project configuration, and [`start.ts`](start.ts) starts a Flow with empty operation input.

Start `dexcli dev -open=false`, configure `yunda` in Dex Web Connections, then run the Worker and Client in separate terminals:

```sh
npm run example:worker --prefix connectors/yunda/typescript
npm run example:start --prefix connectors/yunda/typescript
```

The Flow persists only free and paid waybill counts. Optional rejection branches remain unwired, so a rejection fails the Flow for inspection. The integration test uses the same Flow against a local provider and real Dex Server; it does not require Yunda credentials.
