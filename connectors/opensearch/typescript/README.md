# OpenSearch TypeScript connector

This package indexes, deletes, and searches documents in one OpenSearch index. Indexing fully replaces a document under a stable ID; deletion ensures that ID is absent. These writes are safe to retry. Search source JSON is untrusted provider data.

Configure the static `opensearch` Dex Web connection with the HTTPS cluster REST endpoint and an internal user's Basic credentials. Grant only the required index permissions. The package replays Go-recorded provider scenarios and includes runnable Flows under `examples/provider-operations/`. Live provider authorization remains unverified without a user-controlled connection.

The [document retrieval Flow](examples/provider-operations/document-get-flow.ts) reads a single document by index and ID. The result contains its source and version. Missing indexes and missing documents select separate optional branches. The Worker uses the same OpenSearch connection as the other examples.
