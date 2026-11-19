# Youdao TypeScript connector

`@superdurable/connector-youdao` translates bounded text with Youdao v3 SHA-256 signing. For source text longer than 20 Unicode characters, it signs the first 10, character count, and last 10. The app secret stays outside Flow input and output. The generated retry policy limits duplicate billable requests.

`examples/basic/` runs a Dex Flow, Worker, and Client using relative local imports. `npm run check` replays Go provider scenarios; `npm run test:integration` uses a synthetic provider with real Dex retry and persistence. Live Youdao authorization is unverified.
