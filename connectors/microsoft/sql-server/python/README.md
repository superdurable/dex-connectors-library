# Microsoft SQL Server Python connector

The Python package exposes the manifest's `query` and `execute` operations through native
`python-tds` sessions. Each attempt resolves the current private password, opens one SQL
Server connection, pins session settings, and closes it. It never persists the password in
Flow input, output, a receipt, or an Attribute.

## Install and configure

Install `dex-connector-microsoft-sql-server==0.1.0`. Create a Dex Web connection named
`sqlserver-refunds` for the [runnable example](examples/record_refund/README.md), following
the authorization guide in the [connector README](../README.md#connection-setup).
The connection needs a host, database, SQL authentication user and secret password. Port,
timeouts, row limits, and encryption have manifest defaults.

The Python package supports `encrypt=mandatory` with a verified server certificate from the
system CA file, or from `SQLSERVER_SSL_CA` on the Worker host, and `encrypt=disable` only for
`localhost`, `127.0.0.1`, or `::1`. `python-tds` 1.17.1 has no public per-connection mode for
TDS 8.0 strict encryption or for encryption without certificate validation. The Python
package therefore rejects `encrypt=strict` and `encrypt=trust-server-certificate` during
client construction, before any network connection. These modes remain supported by the Go
connector. Choose the Go connector if either is required; do not change Python's setting
to another mode without reviewing the server's TLS requirements.

`python-tds` converts SQL Server `TIME`, `DATETIME2`, and `DATETIMEOFFSET` values to
Python's microsecond-resolution types. For a result column with scale 7, this package
selects `invalidResponse` and names the column before emitting any row, because the
seventh fractional digit would otherwise be lost. Use a SQL `CAST` to scale 6 or lower
when microsecond precision is sufficient. Scales 0 through 6 retain their exact values.

## Operations

`new_query_step` runs a parameterized `SELECT` or `WITH` statement in a transaction it
always rolls back. It returns `completed` with bounded rows, `truncated` when the row or
JSON byte bound is reached, `providerRejected` for conclusive SQL Server errors,
`invalidResponse` for undecodable values, and `defect` for invalid local input. Transient
failures retry.

`new_execute_step` runs one parameterized `INSERT`, `UPDATE`, `DELETE`, `MERGE`, or `WITH`
statement in its own transaction. The optional final `@pN` idempotency-key placeholder
binds Dex's stable Call ID. A write that exceeds `maxRowsAffected` or an OUTPUT bound
rolls back and selects `limitExceeded`. A failed COMMIT whose outcome is unknown selects
`uncertain`; failures before COMMIT retry only when safe. Make every write statement
idempotent across retries and Worker replacement.

Both operations accept `@p1` through `@pN` positional parameters in SQL text. Do not
interpolate user input into the statement. The connector checks for one allowed statement,
rejects transaction control and other reserved operations, and binds values through
`python-tds` named parameters. Read the [runnable Flow](examples/record_refund/flow.py) for
the exact application call pattern.

## Verification

The package unit tests cover routing, driver error numbers, input validation, row bounds,
and commit uncertainty. The example integration test runs its Worker and Client against a
real `dexcli dev` server with scripted connector operations. CI runs
`tests/integration/test_database.py` against a real SQL Server service container. No live
SQL Server, Azure SQL Database, or Azure SQL Managed Instance has been verified through a
user-controlled Dex Web connection for this Python package.
