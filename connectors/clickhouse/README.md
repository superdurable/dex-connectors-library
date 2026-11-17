# ClickHouse connector

The Go, Python, and TypeScript `listTables` Query returns up to 100 sorted table names from one database and sets `hasMore` when the result exceeds that limit. Go also provides `listDatabases` and `listColumns` Queries. These read `system.databases`, `system.tables`, and `system.columns` through ClickHouse's read-only HTTP GET interface. Database and table inputs are typed query parameters; the connector never runs caller-supplied SQL.

## Connection

In [ClickHouse Cloud](https://clickhouse.cloud), open **Services > your service > Connect > HTTPS**. Set `baseUrl` to that service's HTTPS HTTP endpoint, including its port (commonly 8443). For self-hosted ClickHouse, ask your administrator for the HTTPS HTTP endpoint. Keep credentials, path, query and database out of this URL.

Enter the database `username` and secret `password` shown for the connection. The user needs permission to read `system.databases`, `system.tables`, and `system.columns`. Dex sends credentials as `X-ClickHouse-User` and `X-ClickHouse-Key`; neither appears in Flow input or Results. Leave `timeout` and `maxResponseBytes` blank for their displayed defaults. In the ClickHouse SQL console, use `SHOW DATABASES` and `SHOW TABLES` to find exact non-secret names to pass as `database` and `table`; blank is invalid.

See the [HTTP interface](https://clickhouse.com/docs/concepts/features/interfaces/http), [system.databases](https://clickhouse.com/docs/operations/system-tables/databases), [system.tables](https://clickhouse.com/docs/operations/system-tables/tables), and [system.columns](https://clickhouse.com/docs/operations/system-tables/columns) documentation. [Go](go/examples/table-inspection/README.md), [Python](python/examples/table_inspection/README.md), and [TypeScript](typescript/examples/table-inspection/README.md) contain runnable Dex Flows. The Go example reads databases before listing tables. Go-recorded conformance covers requests, outputs, branches and retries; local integration tests use fixture providers. Live provider authorization and queries remain unverified.
