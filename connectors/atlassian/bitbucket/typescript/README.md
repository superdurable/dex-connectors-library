# Bitbucket TypeScript connector

`@superdurable/connector-atlassian-bitbucket` implements the `listRepositories` and `listPullRequests` read operations as typed Dex Query Steps. It uses the static connection name supplied by the consuming Flow and resolves the workspace access token at call time.

The [resource-read example](examples/resource-read/README.md) contains a runnable Worker, Flow starter, and a real Dex Worker/Client integration test. The [connector README](../README.md) describes authorization, configuration, branches, and retry behavior.

Error responses are classified by HTTP status even if their bodies cannot be read. A `null` item in a provider `values` array becomes a zero-value repository or pull request, matching the Go connector.

Run `npm ci && npm run check` to verify the package. Run `npm run test:integration` with `DEX_FLOW_SERVICE_ADDRESS` set to a running `dexcli dev` server.
