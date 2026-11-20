# Udesk TypeScript connector

`@superdurable/connector-udesk` reads account-defined ticket statuses, with an optional `enabledOnly` filter that preserves explicit `false`. It signs each request using Udesk Open API v2 SHA-256 and a fresh nonce. The API token stays out of Flow input, outputs, and request URLs.

`examples/basic/` runs a Dex Flow, Worker, and Client. `npm run check` replays Go provider scenarios; `npm run test:integration` uses a synthetic provider with real Dex retry and persistence. Live Udesk authorization is unverified.
