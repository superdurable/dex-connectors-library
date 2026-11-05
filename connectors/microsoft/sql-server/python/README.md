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
system CA file, a bundled `certifi` CA fallback when Python exposes no system CA file, or
`SQLSERVER_SSL_CA` on the Worker host. An explicitly configured CA path must exist. Hostname
verification uses Subject Alternative Names, including DNS names and IP addresses; a Common
Name alone is insufficient. `encrypt=disable` is allowed only for
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
The package restores declared DECIMAL/NUMERIC scale and MONEY/SMALLMONEY's four decimal
places after driver decoding. It returns serialized GEOGRAPHY, GEOMETRY, and HIERARCHYID
values as base64 with their SQL type names. Unknown SQL types fail `invalidResponse`.

## Operations

`new_query_step` runs a parameterized `SELECT` or `WITH` statement in a transaction. Closing
its connection without COMMIT makes SQL Server roll it back. It returns `completed` with
bounded rows, `truncated` when the row or JSON byte bound is reached, `providerRejected`
for conclusive SQL Server errors,
`invalidResponse` for undecodable values, and `defect` for invalid local input. Transient
failures retry.

`new_execute_step` runs one parameterized `INSERT`, `UPDATE`, `DELETE`, `MERGE`, or `WITH`
statement in its own transaction. The optional final `@pN` idempotency-key placeholder
binds Dex's stable Call ID. A write that exceeds `maxRowsAffected` or an OUTPUT bound
rolls back and selects `limitExceeded`. A failed COMMIT whose outcome is unknown selects
`uncertain`; a COMMIT failure retries only when no bytes were sent. Failures before COMMIT
retry only when safe. Make every write statement idempotent across retries and Worker replacement.
`rowsAffected` sums all SQL Server DONE counts, including trigger statements. If the server
supplies no count, `maxRowsAffected` fails `invalidResponse` before COMMIT. Receipts include
the session process ID and server version; server errors add number, state, and severity.
The connector enforces an attempt deadline and a response read budget in addition to the
configured row and JSON response bounds. Exhausting the read budget returns truncated
query rows or closes an execute session before COMMIT, causing SQL Server to roll it back.

Both operations accept `@p1` through `@pN` positional parameters in SQL text. Do not
interpolate user input into the statement. The connector checks for one allowed statement,
rejects transaction control and other reserved operations, and binds values through
`python-tds` named parameters. Python `None` binds as a SQL `NULL` literal, small integers
bind as SQL `INT`, and timezone-aware `datetime` inputs are unsupported. Use explicit SQL
casts when a parameter needs a specific SQL type. Read the [runnable Flow](examples/record_refund/flow.py) for
the exact application call pattern.

## Verification

The package unit tests cover routing, driver error numbers, input validation, row bounds,
and commit uncertainty. The example integration test runs its Worker and Client against a
real `dexcli dev` server with scripted connector operations. The SQL Server service-container
workflow runs `tests/integration/test_database.py`; its mandatory TLS pass depends on the
separate CI workflow change in PR #518. No live
SQL Server, Azure SQL Database, or Azure SQL Managed Instance has been verified through a
user-controlled Dex Web connection for this Python package.
