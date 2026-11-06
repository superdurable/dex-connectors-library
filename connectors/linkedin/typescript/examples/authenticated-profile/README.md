# Authenticated profile Flow

This Flow reads the LinkedIn OpenID Connect UserInfo profile using the static `linkedin-signup` connection. Configure that connection in Dex Web using the [package setup guide](../../README.md).

From `connectors/linkedin/typescript`, run `npm ci` and `npm run example:profile` to start the Worker. In a second terminal, run `npm run example:profile:start -- examples/authenticated-profile/input.example.json`. Set `DEX_FLOW_SERVICE_ADDRESS` when Dex uses a nondefault address. The Flow accepts `{}` and persists only the bounded profile. Its access token stays in the connection.

`DEX_FLOW_SERVICE_ADDRESS=<host:port> npm run test:integration` verifies the Flow through a real Dex Worker and Client with a local provider stand-in.

Generate strict Flow Definition Graph 2.0 JSON from this package directory after `npm run build`:

```sh
mkdir -p node_modules/@superdurable
ln -sfn ../.. node_modules/@superdurable/connector-linkedin
dexcli visualize ./examples/authenticated-profile/flow/workflow.ts \
  --language typescript --schema-version 2.0 --json --out /tmp/linkedin-typescript-flow
```
