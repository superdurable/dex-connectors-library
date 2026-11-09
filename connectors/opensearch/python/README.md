# OpenSearch Python connector

This package searches one index and replaces or deletes documents under stable IDs. The fixed-ID writes are safe to retry. Search source JSON is untrusted provider data. The package replays Go-recorded provider scenarios and includes runnable Dex Flows under `examples/`.

Configure the static `opensearch` connection with the HTTPS cluster REST endpoint and an internal user's Basic credentials. Grant only the needed index search, indexing, and deletion permissions. Live provider authorization remains unverified without a user-controlled connection.
