# Elasticsearch TypeScript connector

This package indexes, deletes, and searches documents in one Elasticsearch index. Indexing fully replaces a document under a stable ID; deletion ensures that ID is absent. These writes are safe to retry. Search source JSON is untrusted provider data.

Configure the static `elasticsearch` Dex Web connection with the HTTPS cluster REST endpoint and an internal user's Basic credentials. Grant only the required index permissions. The package replays Go-recorded provider scenarios and includes runnable Flows under `examples/provider-operations/`. Live provider authorization remains unverified without a user-controlled connection.
