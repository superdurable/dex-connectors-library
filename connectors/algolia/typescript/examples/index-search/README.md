# Algolia index search, TypeScript

`IndexSearchFlow` accepts `{ "indexName": "products", "query": "notebook", "hitsPerPage": 2 }` and returns a bounded hit page from Algolia. `ReplaceObjectFlow` accepts `{ "indexName": "products", "objectID": "product-1", "attributes": { "name": "Meridian notebook" } }` and returns Algolia's queued indexing task. Both use the static `algolia` connection. Their operation Steps wire only `searched` and `saved`; a selected optional branch fails the Flow.

From `connectors/algolia/typescript`, run `npm ci`. In Dex Web **Connections**, configure the `algolia` connection with the Application ID and restricted API key described in the [connector guide](../../../README.md#authorization). Start `dexcli dev`, set `DEX_FLOW_SERVICE_ADDRESS` to its Dex gRPC address, and run `npm run example:index-search`. Start `AlgoliaIndexSearch` or `AlgoliaReplaceObject` with the matching input above. The Worker loads project configuration through `loadFromEnvironment()` and resolves credentials at call time.

For a credential-free real Dex check, start `dexcli dev` and run `npm run integration:index-search` with the same address. This exercises both operations, a search retry, and an unwired defect through a real Worker and Client using the generated mock.

For strict Flow Definition Graph 2.0 generation, use a Dex CLI release with TypeScript analysis and run from this directory:

```sh
dexcli visualize examples/index-search/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/algolia-index-search
```

The local Dex CLI v1.24.0 lacks TypeScript analysis, so strict FDG generation is not verified here. The real Dex integration check passed on a local `dexcli dev` server.
