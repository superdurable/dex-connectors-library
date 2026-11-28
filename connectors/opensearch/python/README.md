# OpenSearch Python connector

This package searches one index and replaces or deletes documents under stable IDs. The fixed-ID writes are safe to retry. Search source JSON is untrusted provider data. The package replays Go-recorded provider scenarios and includes runnable Dex Flows under `examples/`.

Configure the static `opensearch` connection with the HTTPS cluster REST endpoint and an internal user's Basic credentials. Grant only the needed index search, indexing, and deletion permissions. Live provider authorization remains unverified without a user-controlled connection.

The [document retrieval Flow](examples/document_get/flow.py) reads a single document by index and ID. The result contains its source and version. Missing indexes and missing documents select separate optional branches. The Worker uses the same OpenSearch connection as the other examples.
