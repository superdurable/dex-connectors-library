# ALAPI IP location example

This example runs `IPLocationFlow` from [`flow.ts`](flow.ts) with the static `alapi` connection. [`worker.ts`](worker.ts) loads that connection from Dex project configuration, and [`start.ts`](start.ts) starts a Flow with a public IPv4 input.

Start `dexcli dev`, configure `alapi` in Dex Web Connections, then run the Worker and Client in separate terminals:

```sh
npm run example:worker --prefix connectors/alapi/typescript
npm run example:start --prefix connectors/alapi/typescript -- 8.8.8.8
```

The Flow stores only the coarse location output. Its optional rejection branches remain unwired, so a rejection fails the Flow for inspection. The integration test uses the same Flow against a local provider and a real Dex Server; it does not require an ALAPI token.
