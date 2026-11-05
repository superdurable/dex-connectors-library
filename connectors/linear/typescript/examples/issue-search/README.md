# Linear issue search example

This Flow searches one bounded page of Linear issues through the static `linear-issue-search` connection. It saves the page as a Dex Attribute and completes with the same result. `providerRejected`, `invalidResponse`, and `defect` remain unwired optional branches, so those outcomes fail the Flow.

Configure that connection in Dex Web **Connections** using a personal API key or Linear OAuth. The authorization guide is in the connector manifest. Keep credentials in the connection, never in the start input.

From `connectors/linear/typescript`, run `npm ci && npm run build`, start `dexcli dev` in another terminal, then run `npm run example:issue-search` for the Worker. Start a request with `npm run example:issue-search:start -- examples/issue-search/input.example.json`. Set `DEX_FLOW_SERVICE_ADDRESS` when Dex uses a nondefault gRPC port.

The integration test in `test/integration/issue-search.integration.test.ts` runs this Flow with a local Linear stand-in through a real Dex Worker and Client. It covers a successful page and an unwired provider rejection. Live Linear authorization and GraphQL behavior remain unverified by this example.

Generate strict FDG 2.0 from this package directory after `npm ci && npm run build`:

```sh
mkdir -p node_modules/@superdurable
test -e node_modules/@superdurable/connector-linear || ln -s ../.. node_modules/@superdurable/connector-linear
dexcli visualize ./examples/issue-search/flow/workflow.ts \
  --language typescript --schema-version 2.0 --lint app --json --open=false \
  --out /tmp/linear-issue-search-fdg
```

The generated JSON must have `valid: true` and `schemaVersion: "2.0"`. The link is local and ignored by Git; it lets Dex CLI resolve this unreleased package's checked-in `dex-connector.json`.
