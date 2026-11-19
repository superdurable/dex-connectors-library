# 17TRACK shipment list example

[`flow.ts`](flow.ts) defines a durable Flow with the static `seventeentrack` connection. [`worker.ts`](worker.ts) loads that connection from Dex project configuration, and [`start.ts`](start.ts) reads the first page of registered shipments.

Start `dexcli dev -open=false`, configure `seventeentrack` in Dex Web Connections, then run the Worker and Client in separate terminals:

```sh
npm run example:worker --prefix connectors/seventeentrack/typescript
npm run example:start --prefix connectors/seventeentrack/typescript
```

The Flow persists only bounded tracking status data. Optional rejection branches remain unwired, so a rejection fails the Flow for inspection.
