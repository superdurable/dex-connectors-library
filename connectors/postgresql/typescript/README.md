# PostgreSQL TypeScript connector

`@superdurable/connector-postgresql` exposes the generated `newQueryRowsStep`,
`newExecuteStatementStep`, and `newExecuteStatementNonIdempotentStep`
factories, typed models, named connections, and mock operations. The
hand-written `PostgreSQLClient` uses the native `pg` driver, opens a fresh
connection for each Dex Step attempt, and resolves the password outside Flow
state. `query` uses a read-only transaction. `execute` commits one repeat-safe
write transaction and supports a bound Dex idempotency key; it is
`repeatOnUnknown: safe`, so a `COMMIT` whose outcome is unknown returns Retry
and the statement runs again. `executeNonIdempotent` takes the same input for
a statement that must not run twice. It is `repeatOnUnknown: unsafe`: it
records a single-dispatch claim in the sync Step heartbeat before `COMMIT`,
selects `uncertain` when `COMMIT` may have reached PostgreSQL, and selects
`uncertain` without connecting on a later attempt after such a `COMMIT` or a
lost Worker. A proven rollback returns Retry, and an unreadable checkpoint
returns Retry without connecting.

See the [runnable refund example](examples/record-refund/README.md) for the
Worker, starter, static `postgresql-ledger` connection name, and SQL schema.
`connector.yaml` defines the connection fields and their Dex Web guidance.

The TypeScript configuration uses milliseconds for `connectTimeoutMs` and
`statementTimeoutMs`. Its other fields follow the connector manifest. Keep SQL
in application source and bind only values as positional parameters. Decimal
and `int8` result values are strings, preserving precision beyond JavaScript's
safe integer range. `timestamptz` values retain PostgreSQL's fractional-second
precision in UTC. JSON containing a number that JavaScript would rewrite or
round selects `invalidResponse` and names the column instead of returning a
lossy value. JSON with duplicate keys or keys that JavaScript would reorder
also selects `invalidResponse` and names the column. A `RETURNING` result exceeding `maxRows` or
`maxResponseBytes` selects `limitExceeded` and rolls back. Each call is
bounded by `connectTimeoutMs` plus `statementTimeoutMs` plus five seconds; a
stalled `COMMIT` returns Retry for `execute` and selects `uncertain` for
`executeNonIdempotent`, which the application reconciles by inspecting the
database.

Run local checks with `npm ci && npm run check`. The CI service-container job
runs `npm run test:integration` against a real PostgreSQL database using the
same `POSTGRESQL_CONNECTOR_TEST_*` variables as the Go live tests. Run
`npm run test:dex` against a `dexcli dev` server with
`DEX_FLOW_SERVICE_ADDRESS` set; that test exercises the example with the
generated mock through a real Dex Worker and Client.

Live TypeScript authorization and provider behavior outside the CI database
have not been verified.
