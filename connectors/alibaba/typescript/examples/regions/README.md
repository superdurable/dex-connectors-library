# Alibaba Cloud ECS regions example

[`flow.ts`](flow.ts) defines `ECSRegionsFlow` with a static `alibaba` connection. [`worker.ts`](worker.ts) loads that connection from Dex project configuration, and [`start.ts`](start.ts) starts a Flow with no operation parameters.

Start `dexcli dev`, configure `alibaba` in Dex Web Connections, then run the Worker and Client in separate terminals:

```sh
npm run example:worker --prefix connectors/alibaba/typescript
npm run example:start --prefix connectors/alibaba/typescript
```

The Flow persists only the returned region catalog. Optional rejection branches remain unwired, so a rejection fails the Flow for inspection. The integration test uses the same Flow against a local provider and real Dex Server; it does not require Alibaba Cloud credentials.
