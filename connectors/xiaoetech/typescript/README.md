# Xiaoetech TypeScript connector

`@superdurable/connector-xiaoetech` obtains and caches an access token for the active application and store, then reads root goods categories. It returns category IDs, titles, and child availability; credentials and access tokens stay outside Flow input and output.

`examples/basic/` runs a Dex Flow, Worker, and Client using relative local imports. `npm run check` replays Go provider scenarios; `npm run test:integration` uses a synthetic provider with real Dex retry and persistence. Live XiaoeTech authorization is unverified.
