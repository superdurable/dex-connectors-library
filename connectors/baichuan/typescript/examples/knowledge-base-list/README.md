# Baichuan knowledge-base list example

[`flow.ts`](flow.ts) defines `KnowledgeBaseListFlow` with the static `baichuan` connection. [`worker.ts`](worker.ts) loads it from Dex project configuration. [`start.ts`](start.ts) starts a Flow with empty input.

Start `dexcli dev`, configure `baichuan` in Dex Web Connections, then run the Worker and Client in separate terminals:

```sh
npm run example:worker --prefix connectors/baichuan/typescript
npm run example:start --prefix connectors/baichuan/typescript
```

The Flow persists only the knowledge-base list. Optional rejection branches remain unwired, so a rejection fails the Flow for inspection. The integration test uses the same Flow against a local provider and real Dex Server; it does not require Baichuan credentials.
