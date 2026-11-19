# Weiling TypeScript connector

`@superdurable/connector-weiling` reads CRM department identifiers, names, and parent identifiers. It obtains and caches a short-lived access token for the current credential pair, then reads the department directory. Employee and leader identifiers are excluded from Flow output.

`examples/basic/` runs a Dex Flow, Worker, and Client. `npm run check` replays Go provider scenarios; `npm run test:integration` uses a synthetic provider with real Dex retry and persistence. Live Weiling authorization is unverified.
