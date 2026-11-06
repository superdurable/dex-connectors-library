# Search shared Notion pages

`SearchPagesFlow` uses the static `notion` connection. Create and share a Notion internal connection as described in the [connector README](../../README.md). Point `DEX_CONNECTOR_CONFIG_FILE` to the local Dex Web `connections.json` file and `DEX_FLOW_SERVICE_ADDRESS` to the gRPC address shown by `dexcli dev`.

From `connectors/notion/typescript`, install and start the Worker:

```sh
npm ci
npm run example:worker
```

In another terminal with the same connection configuration, start a search:

```sh
npm run example:start -- "Form submissions"
```

The starter prints the search result. An empty first page selects `noMatch` and fails the Flow because the example wires only the happy path. The token is resolved from the project connection and never enters the Flow input.

Run the local integration test against a real Dex service with `npm run test:integration`. Its Notion stand-in binds to a free port, exercises a retry, and checks an unwired `noMatch` branch. Live Notion behavior is unverified.
