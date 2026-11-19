# Winit TypeScript connector

`@superdurable/connector-winit` reads authorized overseas warehouse IDs, codes, and names. It signs the fixed warehouse operation twice using the seller token and application secret. Those secrets stay outside Flow input and output.

`examples/basic/` runs a Dex Flow, Worker, and Client. `npm run check` replays Go provider scenarios; `npm run test:integration` uses a synthetic provider with real Dex retry and persistence. Live Winit authorization is unverified.
