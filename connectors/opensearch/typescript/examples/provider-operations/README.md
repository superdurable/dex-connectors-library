# OpenSearch TypeScript provider operations

`DocumentIndexFlow` replaces one document under a stable ID, `DocumentDeleteFlow` ensures a document is absent, and `DocumentSearchFlow` reads one bounded result page. All use the static Dex Web connection name `opensearch`.

Install dependencies with `npm ci`, configure the OpenSearch connection in Dex Web, and run `npm run example:worker` with `DEX_FLOW_SERVICE_ADDRESS` set. Start each Flow with its generated input type. The password stays in Dex credentials.

Generate strict FDG 2.0 graphs from this package directory:

```bash
dexcli visualize examples/provider-operations/document-index-flow.ts --schema-version 2.0 --json --out /tmp/opensearch-typescript-index-fdg --lint app
dexcli visualize examples/provider-operations/document-delete-flow.ts --schema-version 2.0 --json --out /tmp/opensearch-typescript-delete-fdg --lint app
dexcli visualize examples/provider-operations/document-search-flow.ts --schema-version 2.0 --json --out /tmp/opensearch-typescript-search-fdg --lint app
```

The integration test runs these Flows through a real `dexcli dev` server and manifest mocks. It does not contact OpenSearch. Live provider authorization remains unverified until a user-owned connection is available.
