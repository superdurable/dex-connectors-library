# Bilibili authorized user example

`flow.ts` runs the `getAuthorizedUser` Query with the static `bilibili` connection and persists the public identity. Configure `clientID`, `appSecret`, and `accessToken` through Dex Web Connections using the connector guide; the Flow input is `{}` and contains no credentials.

Run `npm run example:worker` in one terminal and `npm run example:start` in another. Use `dexcli dev -open=false` for a local Dex stack. For local FDG analysis before this package is published, run `mkdir -p node_modules/@superdurable && ln -s ../.. node_modules/@superdurable/connector-bilibili` once from the TypeScript package directory. Generate FDG 2.0 with `dexcli visualize --open=false --schema-version 2.0 examples/authorized-user/flow.ts`.

The checked-in integration test uses a local provider fake; real Bilibili authorization and API behavior require an account and are not verified here.
