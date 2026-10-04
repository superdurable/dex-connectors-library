# Microsoft SQL Server Connector

> **Verification status: Dex-integrated, not live.** Verified on a real Dex stack against a
> scripted TDS server; no live SQL Server, Azure SQL Database, or Azure SQL Managed Instance has
> been exercised. See [verification status](../../../docs/verification-status.md) for what is and
> is not verified.

This module runs parameterized SQL against Microsoft SQL Server, Azure SQL Database, and Azure SQL
Managed Instance from Dex Steps. It has two operations, the same two the PostgreSQL and MySQL
connectors have:

- `query` (`QueryRows`) runs one `SELECT` inside a transaction that the connector always rolls
  back, and returns bounded rows.
- `execute` (`ExecuteStatement`) runs one `INSERT`, `UPDATE`, `DELETE`, or `MERGE` in its own
  transaction, commits it, and returns the rows affected and any bounded `OUTPUT` rows.

The connector is built on Microsoft's pure-Go `github.com/microsoft/go-mssqldb` driver, pinned at
v1.9.7, and uses it below `database/sql` so that it owns the transaction and the read bounds. v1.9.7
is the newest release whose `go.mod` accepts Go 1.24, which every module in this repository uses;
v1.10.0 and later require Go 1.25. Each operation opens one connection, runs one transaction, and
closes the connection. The connector keeps no pool, so a rotated password takes effect on the next
Step.

## Company directory

The connector lives at `connectors/microsoft/sql-server` with company `Microsoft`, beside the other
Microsoft connectors, and uses the shared `connectors/microsoft/logo.svg`. It needs no Microsoft
account, Graph permission, or Entra application.

## Connection setup

Dex Web **Connections** shows these fields. Every field description in `connector.yaml` names where
to find the value in the Azure portal, Amazon RDS, or a self-hosted server.

| Field | Secret | Default | Meaning |
| --- | --- | --- | --- |
| `host` | no | none, required | DNS name or IP address of one server, without a port or instance name |
| `port` | no | `1433` | TCP port; a named instance's static port; 3342 for a Managed Instance public endpoint |
| `database` | no | none, required | Database to connect to |
| `user` | no | none, required | SQL authentication user |
| `encrypt` | no | `mandatory` | `mandatory`, `strict`, `trust-server-certificate`, or `disable` |
| `connectTimeout` | no | `10s` | TCP, TLS, login, and any Azure SQL redirect |
| `statementTimeout` | no | `5s` | Client-side statement bound, also applied as `LOCK_TIMEOUT` |
| `maxRows` | no | `1000` | Most rows one query or `OUTPUT` clause may return |
| `maxResponseBytes` | no | `1048576` | Most JSON-encoded row bytes one call may return |
| `password` | yes | none, required | The user's password |

The authorization guide in `connector.yaml` starts at Microsoft's `CREATE USER` reference. It tells
the author to allow SQL authentication on Azure SQL (Microsoft Entra-only authentication must be
off), add a firewall rule or private endpoint for the Worker, create a dedicated contained database
user (`CREATE USER ... WITH PASSWORD`) or login and user, grant only the tables a Flow reads or
writes, and rotate or revoke the password with `ALTER USER`, `ALTER LOGIN`, or `DROP USER`.

Only SQL authentication is supported. A user name with a backslash is rejected, because the driver
would switch to NTLM Windows authentication. Microsoft Entra authentication is future work; see
[Limitations](#limitations).

### TLS

TLS is required by default, and the server certificate is verified unless the author explicitly
chooses otherwise. `encrypt` uses Microsoft's `Encrypt` names:

- `mandatory` negotiates TLS 1.2 inside the TDS 7.4 PRELOGIN exchange, then encrypts the login and
  the whole session. It verifies that the certificate chains to a trusted root and names the host,
  like `Encrypt=Mandatory;TrustServerCertificate=false`. It works with every supported SQL Server
  and with Azure SQL.
- `strict` uses TDS 8.0: TLS starts before any TDS byte, with ALPN `tds/8.0`, so PRELOGIN is
  encrypted too. It verifies the certificate the same way. Microsoft supports it on SQL Server 2022
  and later, Azure SQL Database, and Azure SQL Managed Instance.
- `trust-server-certificate` encrypts like `mandatory` but skips certificate verification,
  Microsoft's `TrustServerCertificate=true`. It is an explicit choice for a self-signed certificate
  on a network the author controls: an impostor server could receive the password.
- `disable` sends traffic unencrypted. `New` accepts it only for `localhost`, `127.0.0.1`, or
  `::1`.

Microsoft's `optional` mode is not offered, because it can leave the session, or everything after
the login, unencrypted. A server that answers PRELOGIN without encryption, or with login-only
encryption, fails the connection before the login is sent. TLS 1.2 is the minimum.

`mandatory` and `strict` trust the Worker's system certificate store. A private CA bundle is named by
the connector-defined `SQLSERVER_SSL_CA` environment variable on the Worker host, and the connector
rereads that file for every connection. The connector verifies the chain and host name itself,
against the name the TLS client used, so an Azure SQL redirect to a gateway worker is verified
against the routed host. The driver reads no connection string: host, port, user, password, and
encryption are set as typed fields, so no value can inject a connection-string keyword, and the
driver's `MSSQL_USE_EPA` environment variable is ignored.

Dex Web **Connections** saves `host`, `database`, `user`, and the other settings as ordinary
connection configuration and `password` as the private credential. Load the project configuration
once at application startup and open the connection by the name its operations use, as
[`examples/record-refund/main.go`](examples/record-refund/main.go) does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := sqlserver.NewProjectConnection(project, recordrefund.ConnectionName)
```

`projectconfig.LoadFromEnvironment` reads the `DEX_PROJECT_*` configuration that Dex Web or
Superverse Studio writes; see
[`sdkgo/projectconfig`](../../../sdkgo/projectconfig/README.md#application-loading). Set the same
`ConnectionName` beside the typed `Connection` in each operation: a Step whose `ConnectionName` is
empty or differs from its connection's name panics at construction.

## Statements and parameters

A statement is one SQL statement written as a constant in application code. Values are bound only
through the positional parameters `@p1` through `@pN`, which the driver sends as typed
`sp_executesql` parameters; a statement without parameters is sent as a SQL batch. The connector
never formats a value into SQL text. It cannot detect a statement that an application built by
concatenating untrusted input, so never build `Statement` from Flow input, email text, or model
output.

`sp_executesql` runs a whole batch, so the server does not enforce one statement per call the way
PostgreSQL and MySQL do. Before connecting, the connector scans the T-SQL text, skipping strings,
`N'...'` literals, `[bracketed]` and `"quoted"` identifiers, `--` comments, and nested block
comments, and selects `defect` when:

- the first keyword after comments and opening parentheses is not one the operation accepts:
  `query` accepts `SELECT` and `WITH`, and `execute` accepts `INSERT`, `UPDATE`, `DELETE`, `MERGE`,
  and `WITH`;
- a semicolon appears anywhere but at the end;
- the text uses a T-SQL reserved keyword that begins or configures another statement:
  transaction control (`BEGIN`, `COMMIT`, `ROLLBACK`, `SAVE`, `TRAN`, `TRANSACTION`,
  `DISTRIBUTED`), dynamic SQL and procedure calls (`EXEC`, `EXECUTE`), context switching (`USE`,
  except in `OPTION (USE HINT ...)` and `USE PLAN`, `SETUSER`, `REVERT`), control of flow (`IF`,
  `WHILE`, `GOTO`, `RETURN`, `BREAK`, `CONTINUE`, `WAITFOR`), declarations (`DECLARE`,
  `DEALLOCATE`), schema and permission changes (`CREATE`, `ALTER`, `DROP`, `TRUNCATE`, `GRANT`,
  `DENY`, `REVOKE`, `TRIGGER`, `STATISTICS`), server administration (`BACKUP`, `RESTORE`, `DBCC`,
  `KILL`, `SHUTDOWN`, `RECONFIGURE`, `CHECKPOINT`, `LOAD`, `DUMP`), remote or file access (`BULK`,
  `OPENROWSET`, `OPENDATASOURCE`, `OPENQUERY`), session options (`ROWCOUNT`, `TEXTSIZE`,
  `IDENTITY_INSERT`), messages (`PRINT`, `RAISERROR`), and text pointers (`READTEXT`,
  `WRITETEXT`, `UPDATETEXT`);
- a placeholder is not exactly `@p1` through `@pN` for the bound parameters, in lowercase without
  leading zeros, a bound parameter is never referenced, or the text references any other variable;
  `@@` system functions such as `@@ROWCOUNT` are allowed;
- the driver would send the text as a stored procedure call.

Reserved keywords cannot be undelimited identifiers, so this check has no false positive on a
column name; write such a column as `[commit]`. It is not a parser: a second data modification or
an unreserved statement such as `SET XACT_ABORT OFF` written after the first statement without a
semicolon is not detected, and runs inside the same transaction. The keyword list is Microsoft's
reserved keywords reference, fetched 2026-10-01.

| Go value | Sent as |
| --- | --- |
| `nil` | SQL `NULL` |
| `string`, `json.Number`, `json.RawMessage` | `nvarchar`; valid UTF-8; numbers and JSON are validated first |
| `bool` | `bit` |
| signed integer types, and unsigned values up to 2^63-1 | `bigint` |
| `float32` | `float` of its shortest decimal, so `0.1` stays `0.1` |
| `float64` | `float`; `NaN` and `Infinity` select `defect` |
| `[]byte` | `varbinary`; `nil` is SQL `NULL` |
| `time.Time` | `datetimeoffset(7)` in UTC, years 0001 through 9999 |

Other types, and unsigned values above 2^63-1, select `defect`. Flow input decoded from JSON turns
numbers into `float64`, so pass exact decimals and large integers as strings. SQL Server converts an
`nvarchar` parameter to a column's type exactly on `INSERT` and `UPDATE`, and in comparisons with
`decimal`, `int`, `uniqueidentifier`, and date and time columns, because those types have higher
precedence. Write `CAST(@p1 AS decimal(12, 2))` when the target type is not otherwise known.
Comparing an `nvarchar` parameter with a `varchar` column can prevent an index seek; cast it to the
column's type. A `time.Time` stored in a `datetime2` column keeps its UTC wall-clock time. The
driver declares each parameter's length from its value, so `nvarchar` declarations vary between
calls. At most 2,098 parameters fit beside `sp_executesql`'s own two, within SQL Server's 2,100.

## Result type mapping

Each row is a `map[string]any` keyed by column name. Every value maps to a JSON value that survives a
JSON round trip without losing precision. Every result column needs a unique, non-empty name, so
alias expressions with `AS`; an unnamed or repeated column selects `defect`.

| Server type | JSON value |
| --- | --- |
| any `NULL` | `null` |
| `tinyint`, `smallint`, `int` | number |
| `bigint` | decimal string, such as `"9007199254740993"` |
| `bit` | `true` or `false` |
| `real` | number of its shortest decimal, such as `0.1` |
| `float` | number |
| `decimal`, `numeric`, `money`, `smallmoney` | exact decimal string, such as `"1200.00"` or `"12.3400"` |
| `char`, `varchar`, `text`, `nchar`, `nvarchar`, `ntext`, `xml` | string |
| `binary`, `varbinary`, `image`, `rowversion` | standard base64 string |
| `uniqueidentifier` | lowercase canonical text, such as `"6f9619ff-8b86-d011-b42d-00c04fc964ff"` |
| `date` | `"2026-02-03"` |
| `time` | `"12:34:56.789"`, with up to seven fractional digits |
| `datetime2`, `datetime`, `smalldatetime` | ISO 8601 without an offset, such as `"2026-01-01T12:34:56.5"` |
| `datetimeoffset` | RFC 3339 with the stored offset, such as `"2026-01-01T09:00:00.5+02:00"` |
| CLR types such as `geography`, `geometry`, `hierarchyid` | standard base64 of the serialized value |

`sql_variant` selects `invalidResponse`; cast it to a concrete type. `uniqueidentifier` text is
lowercase, unlike SQL Server's own uppercase string conversion, so it compares equal to the
connector's idempotency key. `Column` reports each column's name and the driver's type name, such as
`DECIMAL` for `numeric` too, `NVARCHAR`, or `GEOGRAPHY`. A `varchar` column in a `_UTF8` collation
is decoded by the driver with that collation's Windows code page, which is unverified for
non-ASCII text; cast it to `nvarchar`. SQL Server sends the 2025 `json` and `vector` types as text
to drivers that, like v1.9.7, do not negotiate them.

## Session settings

Every connection first runs one connector-owned batch: `SET LANGUAGE us_english`,
`SET DATEFORMAT ymd`, `SET XACT_ABORT ON`, `SET NOCOUNT OFF`, `SET IMPLICIT_TRANSACTIONS OFF`, the
seven options indexed views and computed columns require (`ANSI_NULLS`, `ANSI_PADDING`,
`ANSI_WARNINGS`, `ARITHABORT`, `CONCAT_NULL_YIELDS_NULL`, and `QUOTED_IDENTIFIER` on,
`NUMERIC_ROUNDABORT` off), `SET TEXTSIZE 2147483647`,
`SET TRANSACTION ISOLATION LEVEL READ COMMITTED`, `SET LOCK_TIMEOUT` to `statementTimeout` in
milliseconds, and `SELECT @@SPID, SERVERPROPERTY('ProductVersion')` for the receipt.

`us_english` keeps error messages, which the connector reads only for constraint names, in
English. `NOCOUNT OFF` keeps row counts reported even when the server's `user options` turn them
off, which `MaxRowsAffected` depends on. `ANSI_WARNINGS` makes a value that does not fit its column
fail with error 2628 or 8152 instead of being truncated. `QUOTED_IDENTIFIER` makes double quotes
delimit identifiers, so quote strings with single quotes. Azure SQL Database enables
`READ_COMMITTED_SNAPSHOT` by default, so its reads see row versions instead of waiting on locks.

## Operations and branches

`query` begins a transaction and closes the connection without committing, so the server rolls
back anything the statement changed; SQL Server's DDL is transactional, so that includes a
`SELECT ... INTO`. Unlike PostgreSQL and MySQL, SQL Server has no read-only transaction that rejects
a write, so a `WITH ... DELETE` written into `query` runs and is then rolled back, and the operation
selects `defect` because it returned no result set. The rollback is a guard, not a permission
boundary: identity and sequence values, and anything outside the database such as a linked
server, are not rolled back. Grant the user only `SELECT` on the tables a Flow reads.

| Branch | Selected when |
| --- | --- |
| `completed` | The statement ran and returned every row of its one result set, possibly none |
| `truncated` | More rows than `maxRows` or bytes than `maxResponseBytes`; the Result keeps the rows that fit |
| `providerRejected` | Bad credentials or firewall, a missing database, table, or column, a syntax, data, or permission error, `statementTimeout`, or a server that refuses TLS or fails certificate verification |
| `invalidResponse` | A value that does not match its type, such as `sql_variant`, or a reply the driver cannot decode |
| `defect` | Invalid input, credentials unavailable, an unreadable `SQLSERVER_SSL_CA`, no result set, a second result set, or an unnamed or repeated column |

`execute` begins a transaction, runs the statement, reads any `OUTPUT` rows, and sends `COMMIT`.
Set `ReturnsOutputRows` when the statement has an `OUTPUT` clause that returns rows to the client;
the connector then returns them and counts them as the rows affected, one per affected row. Without
it the connector runs the statement for its row count and discards any `OUTPUT` rows. The row count
is the sum of the counts the server reports, which also includes rows an `AFTER` trigger changes
when it does not `SET NOCOUNT ON`, so `MaxRowsAffected` errs toward `limitExceeded`. When the
statement affects more rows than `MaxRowsAffected`, or its `OUTPUT` rows exceed `maxRows` or
`maxResponseBytes`, the connector closes the connection instead of sending `COMMIT`, and the server
rolls the transaction back.

| Branch | Selected when |
| --- | --- |
| `completed` | The server acknowledged `COMMIT` |
| `limitExceeded` | `MaxRowsAffected`, `maxRows`, or `maxResponseBytes` was exceeded; nothing was written |
| `providerRejected` | A duplicate key or constraint violation, which names its constraint or index, or a syntax, data, permission, or connection rejection, `statementTimeout`, or a `COMMIT` the server reports it never began (error 3902); nothing was written |
| `uncertain` | The connection failed after `COMMIT` was sent, or the server reported any other error while committing |
| `invalidResponse` | An `OUTPUT` value that does not match its type, or a reply the driver cannot decode; nothing was written |
| `defect` | Invalid input, including an `IdempotencyKeyPlaceholder` that is not the last placeholder, or `ReturnsOutputRows` on a statement that returned no result set |

Only a happy path is required. Every other branch is optional, and an unwired optional branch fails
the Flow.

## Statement timeout

SQL Server has no session setting that bounds a statement's run time, so `statementTimeout` is
enforced by the client: when it passes, the driver sends a TDS attention, the server cancels the
statement, and the operation selects `providerRejected`. The same limit is applied as
`LOCK_TIMEOUT`, so a statement waiting on a lock fails sooner with error 1222 and is retried, as a
deadlock is. Every call is bounded by `connectTimeout + statementTimeout + 5s` on the client,
including `COMMIT`, so it returns a classified attempt before Dex's 30-second Execute timeout. Keep
that sum under the Step's Execute timeout.

## Retries, uncertainty, and idempotency

Both operations return Retry, which Dex retries with the Step's policy of five attempts within two
minutes, for connection refusal and connect timeouts, a connection lost before `COMMIT` was sent,
deadlocks (1205), lock timeouts (1222), snapshot update conflicts (3960, 3961), resource and
availability errors (701, 1101, 1105, 6005, 8628, 8645, 8651, 9002, 10922), the Azure SQL transient
errors Microsoft lists (615, 926, 4221, 10928, 10929, 10936, 40197, 40501, 40613, 49918, 49919,
49920), a login during script upgrade mode (18401), and any other error of severity 20 or higher,
which ends the connection.

The server rolls back an open transaction when its connection ends, so a write that fails before
`COMMIT` left nothing behind and is safe to retry. The connector counts the bytes it writes below
TLS, so a `COMMIT` that never left the client is also retried. A connection lost after `COMMIT` was
sent is different: the server may have committed. The connector selects `uncertain` instead of
retrying, and the Result's `Receipt.IdempotencyKey` tells the application which write to look for.

Dex can also run `execute` more than once for one Step execution: after a Worker is lost mid-call,
and when an async Step outlasts its local phase of about seven seconds. The example's integration
test holds an `INSERT` for nine seconds in the scripted server: Dex ends the local attempt at about
seven seconds, the driver cancels the first statement, which writes nothing, and a second dispatch
with the same key writes the one row.

Every run of one Step execution uses the same idempotency key, a 36-character lowercase UUID derived
from the Dex Call ID, bound as `nvarchar`. Set `IdempotencyKeyPlaceholder` to bind it as the last
placeholder, store it in a unique `uniqueidentifier` or `char(36)` column, and write with
`INSERT ... SELECT ... WHERE NOT EXISTS (... WITH (UPDLOCK, HOLDLOCK) ...)`. The hints keep the
existence check's key range locked until `COMMIT`, so a second dispatch waits for the first and
then inserts nothing. The key covers one Step execution; add a business unique key, such as one
refund per order, to deduplicate across Flows. `execute` keeps the repository's async default,
because most statements finish in milliseconds, and sync durability would not remove the need for
idempotency: a Worker lost after `COMMIT` still runs the Step execution again.

The runnable example's write, from
[`examples/record-refund/flow/workflow.go`](examples/record-refund/flow/workflow.go):

```go
InsertRefundStatement = "INSERT INTO dbo.refund_ledger (order_id, amount_usd, external_reference, idempotency_key)\n" +
	"OUTPUT " + insertedRefundColumns + "\n" +
	"SELECT @p1, CAST(@p2 AS decimal(12, 2)), @p3, CAST(@p4 AS uniqueidentifier)\n" +
	"WHERE NOT EXISTS (SELECT 1 FROM dbo.refund_ledger WITH (UPDLOCK, HOLDLOCK)\n" +
	"WHERE idempotency_key = CAST(@p4 AS uniqueidentifier) OR order_id = @p1)"
```

```go
return sqlserver.ExecuteStatementInput{
	Statement:                 InsertRefundStatement,
	Parameters:                []any{input.OrderID, input.AmountUSD, externalReference},
	IdempotencyKeyPlaceholder: 4,
	MaxRowsAffected:           &maxRowsAffected,
	ReturnsOutputRows:         true,
}
```

Both `completed` and `uncertain` go to `CompleteRefundRecording`, which completes with the `OUTPUT`
row, or reads the row back by the key and the order when the insert was uncertain or inserted
nothing. When the read-back finds no row, the example fails the Flow for review instead of
inserting again.

## Error mapping

SQL Server errors carry a number, a severity, and a state but no SQLSTATE, so the connector maps
numbers. Every number below was checked on 2026-10-01 against Microsoft's Database Engine events
and errors reference (ranges 0 through 18999) and the Azure SQL troubleshooting guide's transient
error table.

| Kind | Numbers | Branch |
| --- | --- | --- |
| Authentication | 18452, 18456, 18470, 18486, 18487, 18488, 40532, 40615 | `providerRejected` |
| Authorization | 229, 230, 262, 297, 916, 3906 | `providerRejected` |
| Not found | 207, 208, 2812, 4060, 4064, 4104 | `providerRejected` |
| Conflict | 515, 547, 2601, 2627 | `providerRejected` |
| Validation | 102, 137, 156, 241, 242, 245, 266, 334, 574, 2628, 8003, 8114, 8115, 8134, 8152 | `providerRejected` |
| Quota | 40544 | `providerRejected` |
| Session limit | 40549, 40550, 40551, 40552, 40553 | `providerRejected` |
| Conflict, retried | 1205, 1222, 3960, 3961 | Retry |
| Availability, retried | the transient numbers listed above | Retry |

40532 and 40615 are listed by Microsoft's Azure SQL troubleshooting guide only as login failures;
the exact meaning of each, such as 40615 for a firewall rule, is not confirmed in the fetched pages
and is treated as an authentication rejection. Any other error is `providerRejected` with kind
`providerRejection`, or Retry at severity 20 and above. At `COMMIT`, only a deadlock (1205) and
3902 or 3903 prove nothing committed; every other error there is `uncertain`.

## Security boundary

- The password is a `SecretString` resolved before every call. It never enters Flow input,
  Results, Receipts, Failures, or logs, and the driver's logging is off.
- Failures carry the error number, a short description the connector owns, the severity, the
  state, the phase, and the name of a violated key, index, or constraint, read only when it is a
  plain identifier. They never carry other server message text or parameter values, because SQL
  Server messages repeat values, such as the duplicate key value in error 2627.
- Receipts carry the Call ID, idempotency key, `@@SPID`, the product version, and the error
  number, state, and severity.
- Results hold only the rows the statement selected, bounded by `maxRows` and
  `maxResponseBytes`. The connector counts the bytes it reads below TLS and stops reading after
  `maxResponseBytes` plus 64 KiB of row data, or after 4 MiB of any other reply.
- A panic inside the driver, which v1.9.7 can raise on some malformed replies, is recovered and
  selects `invalidResponse`, or `uncertain` during `COMMIT`, instead of crashing the Worker.
- The connector closes sockets itself and never calls the driver's `Close`, which returns the
  connection's buffer to a pool shared by all connections while its reader goroutine may still use
  it. Results abandoned mid-stream are drained first, so no reader goroutine is left behind.
- The connector does not log. SQL Server Audit and Extended Events can record statement text and
  parameter values; configure the server accordingly.

## Runnable example

[`examples/record-refund`](examples/record-refund/README.md) records one refund in a ledger at most
once. It uses `query` for the duplicate check and the read-back, and `execute` for the idempotent
`INSERT ... OUTPUT`, and reconciles an uncertain `COMMIT`.

## Verification

Run the standalone module checks from this directory:

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

The default suite includes a scripted TDS server, `internal/scriptedtds`, written from the MS-TDS
token formats the real driver parses. It drives the real driver through PRELOGIN with every
encryption answer, TLS inside PRELOGIN packets (`mandatory` and `trust-server-certificate`), TLS
before any TDS byte with ALPN `tds/8.0` (`strict`), certificate chain and host name failures, an
Azure SQL style routing redirect verified against the routed host, SQL authentication, login
errors, `sp_executesql` with typed parameters, every mapped result type, row and byte bounds,
server errors, attentions for `statementTimeout`, dropped connections, every `COMMIT` fault, a
malformed PRELOGIN reply, and goroutine cleanup after abandoned results.

With a Dex development server running, the integration suite runs the example Flow on a real Worker
against the scripted server, including the nine-second duplicate dispatch:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./... -count=1
```

The live suite needs a disposable database and a SQL authentication user that can create tables. It
has not been run against SQL Server, Azure SQL Database, or Azure SQL Managed Instance:

```bash
SQLSERVER_CONNECTOR_TEST_HOST=myserver.database.windows.net SQLSERVER_CONNECTOR_TEST_PORT=1433 \
SQLSERVER_CONNECTOR_TEST_DATABASE=dex_connector_test SQLSERVER_CONNECTOR_TEST_USER=dex_test \
SQLSERVER_CONNECTOR_TEST_PASSWORD=... SQLSERVER_CONNECTOR_TEST_ENCRYPT=mandatory \
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=live ./... -count=1
```

## Limitations

- One statement per call, checked lexically as described above. Multi-statement batches, stored
  procedures, temporary tables shared across calls, and cursors are not supported.
- There is no pool. Each Step pays a TCP, TLS, and login handshake, plus one settings round trip.
- SQL authentication only. Microsoft Entra authentication is future work: go-mssqldb's own Entra
  modes live in its `azuread` package, which pulls in the Azure SDK's `azidentity`, `azcore`, and
  MSAL modules. A dependency-free version would exchange a service principal's client secret with
  `sdkgo/oauthtoken` at the fixed `login.microsoftonline.com` tenant endpoint, pass the token
  through the driver's `NewSecurityTokenConnector`, require `mandatory` or `strict`, and cache the
  token per connection; the Azure SQL token scope (`https://database.windows.net/.default` in common
  use) could not be confirmed from the documentation fetched here. Managed identities need a host
  identity endpoint and are also future work.
- go-mssqldb v1.9.7 lacks fixes released in v1.10 and v1.11 for panics on an overflowing PRELOGIN
  option and an NTLM challenge, a capped LOB buffer allocation, and server-aborted transactions.
  The connector recovers driver panics, never uses NTLM or SQL Server Browser, and stops on any
  statement error, but a malicious server can still make the driver allocate a buffer as large as a
  declared LOB length. Upgrade the driver when this repository moves to Go 1.25.
- A private CA bundle is read from the Worker's `SQLSERVER_SSL_CA`; it is not yet a connection
  field. `HostNameInCertificate`, client certificates, and Always Encrypted are not supported.
- Named instances are reached by port only; SQL Server Browser is never queried. Availability group
  failover partners and `MultiSubnetFailover` are not configured.
- There are no Triggers and no Connector Studio units. Studio setup commands are HTTPS `GET`
  requests, so a table or column picker cannot be declared for a database. A change-data Trigger
  would need a durable per-binding cursor, such as a change tracking version, which `sdkgo` does
  not provide.
