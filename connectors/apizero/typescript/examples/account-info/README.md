# ApiZero account information example

[`flow.ts`](flow.ts) defines `AccountInfoFlow` with the static `apizero` connection. [`worker.ts`](worker.ts) loads that connection from Dex project configuration. [`start.ts`](start.ts) starts a Flow with empty input because the API Key identifies its own account or prepaid quota.

Start `dexcli dev`, configure `apizero` in Dex Web Connections, then run the Worker and Client in separate terminals:

```sh
npm run example:worker --prefix connectors/apizero/typescript
npm run example:start --prefix connectors/apizero/typescript
```

The Flow persists only the permitted account or prepaid quota fields. Optional rejection branches remain unwired, so a rejection fails the Flow for inspection. The integration test uses the same Flow against a local provider and real Dex Server; it does not require ApiZero credentials.
