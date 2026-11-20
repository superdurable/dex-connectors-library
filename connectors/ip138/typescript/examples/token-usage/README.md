# iP138 token usage example

[`flow.ts`](flow.ts) defines `TokenUsageFlow` with the static `ip138` connection. [`worker.ts`](worker.ts) loads that connection from Dex project configuration, and [`start.ts`](start.ts) starts a Flow with no operation parameters.

Start `dexcli dev -open=false`, configure `ip138` in Dex Web Connections, then run the Worker and Client in separate terminals:

```sh
npm run example:worker --prefix connectors/ip138/typescript
npm run example:start --prefix connectors/ip138/typescript
```

The Flow persists only the three usage and quota counts. Optional rejection branches remain unwired, so a rejection fails the Flow for inspection. The integration test uses the same Flow against a local provider and real Dex Server; it does not require iP138 credentials.
