# Yanwen TypeScript connector

`@superdurable/connector-yanwen` reads authorized handover warehouse codes, names, and regions. It signs each read with the customer number, API token, and Unix millisecond timestamp. The token stays outside Flow input and output.

`examples/basic/` runs a Dex Flow, Worker, and Client using relative local imports. `npm run check` replays Go provider scenarios; `npm run test:integration` uses a synthetic provider with real Dex retry and persistence. Live Yanwen authorization is unverified.
