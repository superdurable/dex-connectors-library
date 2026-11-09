# ClickHouse connector

The Go, Python, and TypeScript `listTables` Query returns up to 100 sorted table names from one database and sets `hasMore` when the result exceeds that limit. It reads `system.tables` through ClickHouse's read-only HTTP GET interface using a typed `database` parameter. It never runs caller-supplied SQL.

## Connection

In [ClickHouse Cloud](https://clickhouse.cloud), open **Services > your service > Connect > HTTPS**. Set `baseUrl` to that service's HTTPS HTTP endpoint, including its port (commonly 8443). For self-hosted ClickHouse, ask your administrator for the HTTPS HTTP endpoint. Keep credentials, path, query and database out of this URL.

Enter the database `username` and secret `password` shown for the connection. The user needs permission to read `system.tables`. Dex sends them as `X-ClickHouse-User` and `X-ClickHouse-Key`; neither appears in Flow input or Results. Leave `timeout` and `maxResponseBytes` blank for their displayed defaults. In the ClickHouse SQL console, use `SHOW DATABASES` to find the exact non-secret database name to pass as `database`; blank is invalid.

See the [HTTP interface](https://clickhouse.com/docs/concepts/features/interfaces/http) and [system.tables](https://clickhouse.com/docs/reference/system-tables/tables) documentation. [Go](go/examples/table-inspection/README.md), [Python](python/examples/table_inspection/README.md), and [TypeScript](typescript/examples/table-inspection/README.md) contain runnable Dex Flows. Go-recorded conformance covers requests, outputs, branches and retries; local integration tests use fixture providers. Live provider authorization and queries remain unverified.
