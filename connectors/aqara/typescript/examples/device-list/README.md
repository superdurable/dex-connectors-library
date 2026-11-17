# Aqara device list example

[`flow.ts`](flow.ts) defines `DeviceListFlow` with the static `aqara` connection. [`worker.ts`](worker.ts) loads that connection from Dex project configuration. [`start.ts`](start.ts) starts a Flow for the first page of fifty devices.

Start `dexcli dev`, configure `aqara` in Dex Web Connections, then run the Worker and Client in separate terminals:

```sh
npm run example:worker --prefix connectors/aqara/typescript
npm run example:start --prefix connectors/aqara/typescript
```

The Flow persists only the returned device page. Optional rejection branches remain unwired, so a rejection fails the Flow for inspection. The integration test uses the same Flow against a local provider and real Dex Server; it does not require Aqara credentials.
