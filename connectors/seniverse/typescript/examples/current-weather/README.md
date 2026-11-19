# Seniverse current-weather example

[`flow.ts`](flow.ts) defines `WeatherLookupFlow` with a static `seniverse` connection. [`worker.ts`](worker.ts) loads that connection from Dex project configuration. [`start.ts`](start.ts) starts a Flow for Beijing and persists the selected weather result.

Start `dexcli dev -open=false`, configure `seniverse` in Dex Web Connections, then run the Worker and Client in separate terminals:

```sh
npm run example:worker --prefix connectors/seniverse/typescript
npm run example:start --prefix connectors/seniverse/typescript
```

The Flow persists only the current-weather result. Optional rejection branches remain unwired, so a rejection fails the Flow for inspection. The integration test exercises the same Flow against a local provider and real Dex Server without Seniverse credentials.
