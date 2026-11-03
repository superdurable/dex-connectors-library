# Algolia connector for Dex TypeScript

`@superdurable/connector-algolia` provides `search` and `saveObject` operations for Dex TypeScript applications. Install with `npm install @superdurable/connector-algolia @superdurable/dex`. Use Node.js 22 or 24.

`search` posts one query to an Algolia index and returns only each hit's `objectID` and optional `name`. `saveObject` replaces the complete string-attribute record at a stable `objectID`; a `saved` result confirms an indexing task was queued and does not mean the record is searchable yet. Both operations retry transport failures, 408, 409, 429, and 5xx responses. Optional `providerRejected`, `invalidResponse`, and `defect` branches fail a Flow when left unwired.

The [index search and replacement example](examples/index-search) is the runnable consuming application. It binds the static `algolia` connection, runs both Flows in a Worker from project configuration, and has a credential-free integration check against a real Dex Worker and Client. The operation Steps wire only `searched` and `saved`. The generated `GeneratedMock` provides deterministic defaults for both operations.

Configure Dex Web **Connections** with the Algolia Application ID and restricted API key described in the [connector authorization guide](../README.md#authorization). The Worker resolves both values for each call. Keep the API key out of Flow input, Attributes, and Results. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank to use manifest defaults; use a compatible HTTPS gateway URL, a positive Go duration, or a positive byte limit only when the deployment needs an override.

The package's `dex-connector.json` and `src/generated/` contracts come from `connector.yaml`. Regenerate and check from the repository root:

```sh
GOWORK=off go run ./cmd/connectorctl generate connectors/algolia/connector.yaml
GOWORK=off go run ./cmd/connectorctl generate --check connectors/algolia/connector.yaml
```

Run `npm ci && npm run check` in this directory. The Go-recorded provider scenarios under `conformance/connectors/algolia/` are replayed by the TypeScript suite. Live Algolia authorization, indexing, and search responses have not been verified.
