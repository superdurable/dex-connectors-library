# MySQL TypeScript connector

The `@superdurable/connector-mysql` package provides native MySQL and MariaDB
`query` and `execute` operations for Dex Flows. Each invocation opens one
connection, resolves the password at call time, and closes the connection after
one transaction. The Connector manifest and [root README](../README.md) define
the connection fields, transaction limits, type mapping, and SQL restrictions.
`BIGINT` values remain decimal strings and `FLOAT` values use their shortest
float32 representation. Negative zero in `FLOAT` or `DOUBLE` fails with
`invalidResponse` because Dex JSON cannot preserve its sign. JSON numbers that
cannot be returned without changing their text cause the `invalidResponse`
branch, with the column named in the failure. Queries use prepared execution,
so chained SQL statements are rejected.

For TypeScript, `verify-identity` requires a DNS host name. The mysql2 TLS
upgrade verifies that name against the server certificate. It cannot verify an
IP address against the certificate IP SAN, so the connector rejects an IP
literal before connecting. Use a DNS name that appears in the certificate or
`verify-ca` if validating the certificate chain without host identity suffices.

Install the package with `npm install @superdurable/connector-mysql`. Configure
a MySQL connection named `mysql-refunds` in Dex Web, then run the checked-in
[refund lookup example](examples/lookup-refund/README.md). The example uses a
parameterized query and a static connection name; it does not store credentials
in Flow state. A write Step can use `newExecuteStatementStep` with a stable
`idempotencyKeyPlaceholder`, and must account for the optional `uncertain`
branch when COMMIT may have reached the server.

Run `npm run check` for unit and type checks. The Dex Worker and Client test in
`test/integration/flow.integration.test.ts` requires a running `dexcli dev`
server and `DEX_FLOW_SERVICE_ADDRESS`. The native database test in
`test/integration.test.ts` runs in CI's MySQL service container using the same
`MYSQL_CONNECTOR_TEST_*` variables as the Go live tests.
