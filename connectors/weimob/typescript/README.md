# Weimob TypeScript connector

`@superdurable/connector-weimob` obtains a short-lived access token for the current application and store, then reads the authorized WOS store profile. The token is cached in memory and excluded from Flow input and output. Provider 429, 408, 5xx, and transport failures retry; definitive rejections and malformed responses use optional branches.

`examples/basic/` runs a Dex Flow, Worker, and Client. `npm run check` replays Go provider scenarios; `npm run test:integration` uses a synthetic provider with real Dex retry and persistence. Live Weimob authorization is unverified.
