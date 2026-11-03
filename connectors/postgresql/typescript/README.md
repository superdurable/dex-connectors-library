# PostgreSQL TypeScript connector

`@superdurable/connector-postgresql` exposes the generated `newQueryRowsStep` and
`newExecuteStatementStep` factories, typed models, named connections, and mock
operations. The hand-written `PostgreSQLClient` uses the native `pg` driver,
opens a fresh connection for each Dex Step attempt, and resolves the password
outside Flow state. `query` uses a read-only transaction. `execute` commits
one write transaction and supports a bound Dex idempotency key.

See the [runnable refund example](examples/record-refund/README.md) for the
Worker, starter, static `postgresql-ledger` connection name, and SQL schema.
`connector.yaml` defines the connection fields and their Dex Web guidance.

The TypeScript configuration uses milliseconds for `connectTimeoutMs` and
`statementTimeoutMs`. Its other fields follow the connector manifest. Keep SQL
in application source and bind only values as positional parameters. Decimal
and `int8` result values are strings, preserving precision beyond JavaScript's
safe integer range. A `RETURNING` result exceeding `maxRows` or
`maxResponseBytes` selects `limitExceeded` and rolls back. A connection loss
after `COMMIT` is sent selects `uncertain`; inspect the database using the
idempotency key before retrying a non-idempotent statement.

Run local checks with `npm ci && npm run check`. The CI service-container job
runs `npm run test:integration` against a real PostgreSQL database using the
same `POSTGRESQL_CONNECTOR_TEST_*` variables as the Go live tests. Run
`npm run test:dex` against a `dexcli dev` server with
`DEX_FLOW_SERVICE_ADDRESS` set; that test exercises the example with the
generated mock through a real Dex Worker and Client.

Live TypeScript authorization and provider behavior outside the CI database
have not been verified.
