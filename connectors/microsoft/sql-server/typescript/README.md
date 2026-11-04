# Microsoft SQL Server TypeScript connector

`@superdurable/connector-microsoft-sql-server` implements the SQL Server `query` and `execute`
operations for Dex. The runnable [query Flow](examples/query-rows/README.md) and
[record-refund Flow](examples/record-refund/README.md) are application entry points. Both
operations use a fresh SQL authentication connection and a transaction. A query
rolls back; an execute commits only after validating its result and row limit.

The package uses `mssql` for pooling and request APIs and its Tedious driver for TDS. It disables
Tedious's internal transient-login retries so Dex sees each attempt. Every transaction pins
`LANGUAGE us_english`, `DATEFORMAT ymd`, `XACT_ABORT ON`, `NOCOUNT OFF`,
`IMPLICIT_TRANSACTIONS OFF`, `ANSI_NULLS ON`, `ANSI_PADDING ON`, `ANSI_WARNINGS ON`,
`ARITHABORT ON`, `CONCAT_NULL_YIELDS_NULL ON`, `QUOTED_IDENTIFIER ON`,
`NUMERIC_ROUNDABORT OFF`, `TEXTSIZE 2147483647`, read-committed isolation, and
`LOCK_TIMEOUT` in milliseconds from `statementTimeoutMs`. The receipt includes the SQL Server
process ID and version, plus safe error number, state, and severity when the server supplies them.

Statements are limited to 1 MiB and 2,098 positional parameters; their bound values may total at
most 16 MiB. TypeScript accepts `null`, strings, booleans, finite safe integers, finite floats,
valid UTC `Date` values, and `Buffer` values. It binds integers as `BIGINT`, floating numbers as
`FLOAT`, strings as `NVARCHAR`, dates as `DATETIME2(7)`, and buffers as `VARBINARY`. JavaScript
has no `json.Number` or `json.RawMessage` value type: pass an exact decimal or JSON document as
text and `CAST(@pN AS ...)` in the constant statement when SQL needs a specific type.

For `mandatory`, set `SQLSERVER_SSL_CA` in the Worker environment to a PEM bundle
when the server uses a private CA. The connector rereads the file for every connection and treats
an unreadable or invalid bundle as a configuration defect. Without it, Node uses its bundled CA
roots by default; Node does not use the operating system's trust store unless configured through
Node's own CA settings. The connector verifies the certificate chain and matches only DNS or IP
subject alternative names with Go's exact wildcard rules; the common name is ignored. The
connector verifies a redirected Azure SQL connection against the routed server's certificate
name. Tedious does not expose the strict TDS 8 TLS socket before LOGIN7, so `strict` fails validation when
constructing the client, before opening a connection. Use `mandatory` for verified TLS with this
TypeScript package.

The result mapping follows the [Go connector](../README.md): `BIGINT` is decimal text, binary
values are base64, `UNIQUEIDENTIFIER` is lowercase, and supported date and time values use Go's
canonical strings with up to seven fractional digits. `REAL` uses the shortest decimal value that
round trips to the SQL float32 value. `DECIMAL` and `NUMERIC` with precision at most 15 return
exact text with the declared scale.

## Known TypeScript limitation

Tedious decodes `DECIMAL` and `NUMERIC` above precision 15, `MONEY`, and `SMALLMONEY` through
JavaScript numbers and discards the stored offset of `DATETIMEOFFSET`. Those values cannot be
reconstructed exactly. The connector inspects column metadata before emitting any rows and takes
the terminal `invalidResponse` branch with the column name and SQL type for these columns, even
when the result set is empty. An `execute` rolls back its transaction on that branch. Cast such
columns to a supported exact representation in SQL when needed, for example `varchar` for a
high precision decimal or `datetimeoffset` value.

Tedious also parses `GEOGRAPHY` and `GEOMETRY` into objects instead of exposing their original
serialized bytes, and does not reliably expose `HIERARCHYID` as the Go driver's raw bytes. The
connector rejects these CLR types and `sql_variant` from column metadata as terminal
`invalidResponse`, even when no row follows, rather than returning a lossy value.

Tedious materializes large `NVARCHAR(MAX)`, `VARBINARY(MAX)`, and `XML` values before emitting a
row. The connector limits raw response bytes while the statement runs and closes the connection
once `maxResponseBytes` plus 64 KiB of protocol overhead is exceeded. Queries then return
`truncated`; executes return `limitExceeded` and roll back. `FOR JSON` and `FOR XML` results use
Tedious's row events so each provider chunk is counted and returned as a row before `mssql`
concatenates chunks into one value. This path relies on `mssql` 12's internal request hook.
Transaction setup, COMMIT, and ROLLBACK replies retain a separate 4 MiB wire budget. The user
statement alone uses `statementTimeoutMs`; the whole SQL attempt, including connection,
session setup, statement, and COMMIT, is bounded by `connectTimeoutMs` plus
`statementTimeoutMs` plus five seconds.

Run `npm run check` for unit tests, typecheck, and lint. CI runs the integration suite against its
SQL Server service container, then repeats it with mandatory TLS and a generated CA bundle. Real
SQL Server and Dex Worker execution were not verified locally.
