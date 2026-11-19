# iFLYTEK Spark chat example

[`flow.ts`](flow.ts) defines `ChatCompletionFlow` with the static `iflytek` connection. [`worker.ts`](worker.ts) loads that connection from Dex project configuration, and [`start.ts`](start.ts) starts a Flow with a bounded prompt.

Start `dexcli dev -open=false`, configure `iflytek` in Dex Web Connections, then run the Worker and Client in separate terminals:

```sh
npm run example:worker --prefix connectors/iflytek/typescript
npm run example:start --prefix connectors/iflytek/typescript
```

The Flow persists only the assistant answer and token usage. Optional rejection branches remain unwired, so a rejection fails the Flow for inspection. The integration test uses the same Flow against a local provider and real Dex Server; it does not require IFLYTEK credentials.
