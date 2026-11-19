# Wapi TypeScript connector

`@superdurable/connector-wapi` reads vehicle brands and prices in Chinese fen. It signs the fixed HTTPS endpoint with the current AppID, secret, and Unix timestamp. Credentials stay outside Flow input and results. Provider 429, 408, 5xx, transport, and documented temporary codes retry.

`examples/basic/` runs a Dex Flow, Worker, and Client. `npm run check` replays the Go provider scenarios; `npm run test:integration` uses a synthetic provider with real Dex retry and persistence. Live Wapi authorization is unverified.
