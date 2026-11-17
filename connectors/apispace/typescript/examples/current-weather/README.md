# APISpace current weather example

[`flow.ts`](flow.ts) defines `CurrentWeatherFlow` with the static `apispace` connection. [`worker.ts`](worker.ts) loads that connection from Dex project configuration. [`start.ts`](start.ts) starts a Flow for the sample Beijing area code; replace it with a city ID from the product's Domestic Cities attachment.

Start `dexcli dev`, configure `apispace` in Dex Web Connections, then run the Worker and Client in separate terminals:

```sh
npm run example:worker --prefix connectors/apispace/typescript
npm run example:start --prefix connectors/apispace/typescript
```

The Flow persists only the returned weather observation. Optional rejection branches remain unwired, so a rejection fails the Flow for inspection. The integration test uses the same Flow against a local provider and real Dex Server; it does not require APISpace credentials.
