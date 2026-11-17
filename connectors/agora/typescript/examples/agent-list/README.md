# Agora running agents example

[`flow.ts`](flow.ts) defines `RunningAgentsFlow` with the static `agora` connection. [`worker.ts`](worker.ts) loads that connection from Dex project configuration, and [`start.ts`](start.ts) starts a Flow with no operation parameters.

Start `dexcli dev`, configure `agora` in Dex Web Connections, then run the Worker and Client in separate terminals:

```sh
npm run example:worker --prefix connectors/agora/typescript
npm run example:start --prefix connectors/agora/typescript
```

The Flow persists only the bounded agent page. Optional provider rejection branches remain unwired, so a rejection fails the Flow for inspection. The integration test uses the same Flow against a local provider and real Dex Server; it does not require Agora credentials.
