# MySQL Connector

This module runs parameterized SQL against a MySQL or MariaDB database from
Dex Steps. It has two operations:

- `query` (`QueryRows`) runs one read statement inside a read-only
  transaction and returns bounded rows.
- `execute` (`ExecuteStatement`) runs one write statement in its own
  transaction, commits it, and returns the rows affected, the last insert ID,
  the warning count, and any bounded MariaDB `RETURNING` rows.

The connector is built on the pure-Go `github.com/go-sql-driver/mysql`
driver, pinned at v1.10.1, and uses it below `database/sql` so that it owns
the transaction and the read bounds. Each operation opens one connection, runs
one transaction, and closes the connection. The connector keeps no pool, so a
rotated password takes effect on the next Step. It is written for MySQL 8.0
and later and MariaDB 10.6 and later, and was verified against MySQL 8.4.11
and MariaDB 13.0.2.

## Company directory

The first directory below `connectors/` is the company that
`metadata.company` names. This connector lives at `connectors/mysql` with
company `MySQL`, not below `connectors/oracle`:

- The repository files a product under the brand that ships it, not its
  corporate parent: GitHub, Slack, and LinkedIn are not filed under their
  owners. MySQL is a product brand of Oracle, like those.
- The connector speaks the MySQL client/server protocol to any compatible
  server, including MariaDB, Amazon RDS and Aurora, Google Cloud SQL, and
  Azure Database for MySQL. It uses no Oracle account, API, or credential,
  so an `Oracle` company card would misdescribe it.
- It mirrors `connectors/postgresql`, the other database protocol connector.

`logo.svg` is a generic database glyph in MySQL's colors. It does not copy
Oracle's dolphin trademark.

## Connection setup

Dex Web **Connections** shows these fields. Every field description in
`connector.yaml` names where to find the value on Amazon RDS, Google Cloud
SQL, Azure Database for MySQL, or a self-hosted server.

| Field | Secret | Default | Meaning |
| --- | --- | --- | --- |
| `host` | no | none, required | DNS name, IP address, or Unix-socket path of one server |
| `port` | no | `3306` | TCP port |
| `database` | no | none, required | Default database (schema) |
| `user` | no | none, required | Account user name without `@host` |
| `sslMode` | no | `required` | `required`, `verify-ca`, `verify-identity`, or `disabled` |
| `connectTimeout` | no | `5s` | TCP, TLS, and authentication bound |
| `statementTimeout` | no | `5s` | Server-side statement and lock-wait bound |
| `maxRows` | no | `1000` | Most rows one query or `RETURNING` clause may return |
| `maxResponseBytes` | no | `1048576` | Most JSON-encoded row bytes one call may return |
| `password` | yes | none, required | The account's password |

The authorization guide in `connector.yaml` starts at MySQL's `CREATE USER`
reference. It tells the author to create a dedicated least-privilege account
with `REQUIRE SSL`, grant only the tables a Flow reads or writes, and rotate the
password with `ALTER USER` or revoke access with `ACCOUNT LOCK`.

TLS is required by default. `sslMode` uses the `mysql` client's `--ssl-mode`
names:

- `required` encrypts but does not verify the server certificate.
- `verify-ca` also verifies the chain.
- `verify-identity` also verifies that the certificate names the host.
- `disabled` sends plaintext. `New` accepts it only for `localhost`,
  `127.0.0.1`, `::1`, or a Unix-socket path, and a Unix-socket path must use
  it.

`preferred` is not offered because it silently falls back to plaintext; a
server without TLS fails the connection instead. TLS 1.2 is the minimum.
`required` protects against eavesdropping but not against an impostor server:
when `caching_sha2_password` needs full authentication it sends the password
inside the unverified TLS session. Use `verify-identity`, or `verify-ca`, on any
network you do not control.
`verify-ca` and `verify-identity` trust the Worker's system certificate store.
A private CA bundle, such as Amazon RDS's
`https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem`, is named
by the connector-defined `MYSQL_SSL_CA` environment variable on the Worker host.
The connector rereads that file for every connection. The driver reads no
DSN, option file, or other environment variable, so `MYSQL_HOST`, `MYSQL_PWD`,
`MYSQL_TCP_PORT`, and `~/.my.cnf` cannot redirect or weaken a connection.

Authentication uses `caching_sha2_password`, `mysql_native_password`, or
MariaDB's `client_ed25519`. The cleartext plugin, `LOAD DATA LOCAL INFILE`,
multi-statement queries, and client-side parameter interpolation are all
disabled.

A local connection file looks like this:

```json
{
  "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
  "connections": [{
    "connectorId": "mysql",
    "modulePath": "github.com/superdurable/dex-connectors-library/connectors/mysql",
    "moduleVersion": "v0.1.0",
    "provider": "mysql",
    "connectionName": "mysql-ledger",
    "configuration": {"host": "db.example.com", "database": "app", "user": "dex_app"},
    "credentials": {"password": "..."}
  }]
}
```

## Statements and parameters

A statement is one SQL statement written as a constant in application code.
Values are bound only through positional `?` placeholders, sent as
server-side prepared-statement parameters. The connector never formats a
value into SQL text. It cannot detect a statement that an application built
by concatenating untrusted input, so never build `Statement` from Flow input,
email text, or model output.

A prepared statement holds exactly one statement, so `SELECT 1; DROP ...` is
rejected by the server. Before connecting, the connector checks the
statement's first keyword after whitespace, comments, and opening
parentheses:

- `query` accepts `SELECT`, `WITH`, `TABLE`, `VALUES`, `SHOW`, `EXPLAIN`,
  `DESCRIBE`, and `DESC`.
- `execute` accepts `INSERT`, `UPDATE`, `DELETE`, `REPLACE`, and `WITH`.

Everything else selects `defect`. MySQL commits implicitly before and after
DDL such as `CREATE`, `ALTER`, `DROP`, `RENAME`, and `TRUNCATE`, and before
`LOCK TABLES`, so those statements would escape the connector's transaction.
MySQL 8.4 even runs a `CREATE TABLE` sent inside `START TRANSACTION READ ONLY`.
Transaction control, `SET`, `CALL`, and `LOAD DATA` are also rejected, as is an
executable comment (`/*! ... */` or `/*M! ... */`) before the keyword,
because the server runs its text. After `COM_STMT_PREPARE` the connector also
selects `defect`, before executing anything, when the placeholder count
differs from the bound parameters. When the result arrives, two result columns
that share a name, which a row map cannot hold, select `defect`; for `execute`
the transaction is then rolled back.

| Go value | Bound as |
| --- | --- |
| `nil` | SQL `NULL` |
| `string` | text; valid UTF-8 |
| `bool` | `1` or `0` |
| signed integer types | signed `BIGINT` |
| unsigned integer types | unsigned `BIGINT` |
| `float32` | `DOUBLE` of its shortest decimal, so `0.1` stays `0.1` |
| `float64` | `DOUBLE`; `NaN` and `Infinity` select `defect` |
| `json.Number` | the number's exact text |
| `json.RawMessage` | validated JSON text |
| `[]byte` | binary string; `nil` is SQL `NULL` |
| `time.Time` | UTC text `YYYY-MM-DD HH:MM:SS.ffffff`, truncated to microseconds |

Other types select `defect`. Flow input decoded from JSON turns numbers into
`float64`, so pass exact decimals and integers beyond 2^53 as strings. MySQL
converts a string parameter to a column's type exactly on `INSERT` and
`UPDATE`, but compares a string with a number as `DOUBLE`. Write
`CAST(? AS DECIMAL(12,2))` or `CAST(? AS UNSIGNED)` to compare exactly. To
bind base64 data from JSON, pass the string and decode it in SQL with
`FROM_BASE64(?)`. A `time.Time` stored in a `TIMESTAMP` column keeps its
instant; a `DATETIME` column stores the UTC wall-clock time.

## Result type mapping

Each row is a `map[string]any` keyed by column name. Every value maps to a
JSON value that survives a JSON round trip without losing precision.

| Server type | JSON value |
| --- | --- |
| any `NULL` | `null` |
| `TINYINT`, `SMALLINT`, `MEDIUMINT`, `INT`, `YEAR`, signed or unsigned | number |
| `BIGINT`, `BIGINT UNSIGNED` | decimal string, such as `"9007199254740993"` |
| `FLOAT` | number of its shortest decimal, such as `0.1` |
| `DOUBLE` | number |
| `DECIMAL` | exact decimal string, such as `"1200.00"` |
| `CHAR`, `VARCHAR`, `TEXT` variants, `ENUM`, `SET` | string |
| `BINARY`, `VARBINARY`, `BLOB` variants, `GEOMETRY`, `VECTOR` | standard base64 string |
| `BIT` | decimal string of the unsigned value, such as `"513"` |
| `JSON` (MySQL) | the JSON value as MySQL's normalized text |
| `TIMESTAMP` | RFC 3339 in UTC, such as `"2026-01-01T00:00:00.123456Z"` |
| `DATETIME` | ISO 8601 without an offset, such as `"2026-01-01T12:34:56.5"` |
| `DATE` | `"2026-02-03"` |
| `TIME` | the server's text, such as `"-12:34:56.50"` |

`BOOLEAN` is `TINYINT(1)` and returns `0` or `1`. MariaDB stores `JSON` as
`LONGTEXT`, so it returns the original text as a string. MySQL's `JSON` type
re-serializes documents, sorting keys and normalizing numbers such as `1.10`
to `1.1`. A zero or partial date such as `"0000-00-00 00:00:00"` keeps the
server's text. `Column` reports each column's name and the driver's
`TypeName`, such as `DECIMAL`, `UNSIGNED BIGINT`, or `BLOB`.

The connector pins these session settings on every connection:
`time_zone = '+00:00'`, MySQL 8.0's default `sql_mode`
(`ONLY_FULL_GROUP_BY,STRICT_TRANS_TABLES,NO_ZERO_IN_DATE,NO_ZERO_DATE,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION`),
`wait_timeout = 30`, and the statement timeout below. `time_zone` makes
`TIMESTAMP` values and `NOW()` UTC. The strict `sql_mode` makes a value that
does not fit its column fail instead of being truncated with a warning, and
makes double quotes delimit strings, so quote identifiers with backticks.

## Operations and branches

`query` sends `START TRANSACTION READ ONLY` and closes the connection without
committing. A write inside the statement, for example through a stored
function, fails with error 1792 (SQLSTATE 25006) and selects
`providerRejected`. A read-only transaction is a guard, not a permission
boundary: `SELECT ... INTO OUTFILE`, `GET_LOCK()`, and `FOR UPDATE` locks still
act. Grant the account only `SELECT` on the tables a Flow reads.

| Branch | Selected when |
| --- | --- |
| `completed` | The statement ran and returned every row, possibly none |
| `truncated` | More rows than `maxRows` or bytes than `maxResponseBytes`; the Result keeps the rows that fit |
| `providerRejected` | Bad credentials, a missing database, table, or column, a syntax, data, or permission error, a write in the read-only transaction, `statementTimeout`, or a server that refuses TLS or fails certificate verification |
| `invalidResponse` | A value that does not match its type's format, a reply the driver cannot decode, or a non-row reply beyond 4 MiB |
| `defect` | Invalid input, credentials unavailable, an unreadable `MYSQL_SSL_CA`, or a placeholder or column-name defect |

`execute` sends `START TRANSACTION`, runs the statement, reads any `RETURNING`
rows, then reads `ROW_COUNT()`, `LAST_INSERT_ID()`, and `@@warning_count` in
the same transaction, and sends `COMMIT`. `MaxRowsAffected` guards against a
missing `WHERE` clause. When the statement affects more rows, or its
`RETURNING` rows exceed `maxRows` or `maxResponseBytes`, the connector closes
the connection instead of sending `COMMIT`. The server then rolls the
transaction back.

| Branch | Selected when |
| --- | --- |
| `completed` | The server acknowledged `COMMIT` |
| `limitExceeded` | `MaxRowsAffected`, `maxRows`, or `maxResponseBytes` was exceeded; nothing was written |
| `providerRejected` | A duplicate key or constraint violation, which names its key, or a syntax, data, permission, or connection rejection; nothing was written |
| `uncertain` | The connection failed after `COMMIT` was sent, or the server reported an error while committing other than a deadlock, a lock-wait timeout, SQLSTATE 40001, or a constraint error |
| `invalidResponse` | A `RETURNING` value or `ROW_COUNT()` result that does not match its type, or a reply the driver cannot decode; nothing was written |
| `defect` | Invalid input, including an `IdempotencyKeyPlaceholder` that is not the last placeholder |

Only a happy path is required. Every other branch is optional, and an unwired
optional branch fails the Flow.

Rollback, "nothing was written", and safe retries before `COMMIT` all need a
transactional storage engine such as InnoDB, the default. A write to a MyISAM,
MEMORY, or Aria table is applied as the statement runs, so a `limitExceeded`
`DELETE` has already deleted its rows and a retried statement runs twice. Keep
every table a Flow writes on InnoDB.

A reply the driver cannot decode, such as an unknown column type, selects
`invalidResponse` instead of retrying, because it would not decode on a new
connection either.

## Statement timeout

`statementTimeout` bounds statements on the server, and the server's names
differ:

- MySQL: `max_execution_time`, in milliseconds, which bounds `SELECT`
  statements only, plus `innodb_lock_wait_timeout` and `lock_wait_timeout`,
  rounded up to whole seconds, which bound row and metadata lock waits for
  every statement.
- MariaDB: `max_statement_time`, which bounds every statement, plus the same
  lock-wait settings.

The connector reads `VERSION()` on every connection to choose. A statement
that runs past the limit fails with error 3024 or 1969 and selects
`providerRejected`. A lock wait past it fails with error 1205 and is retried,
as is a deadlock. A MySQL write that is still running when the call deadline
passes loses its connection; the server rolls it back, and the attempt is
retried.

## Retries, uncertainty, and idempotency

Both operations return Retry, which Dex retries with the Step's policy of five
attempts within two minutes, for:

- connection refusal and connect timeouts;
- error 1040 (too many connections) and 1203 (account connection limit);
- server shutdown, killed connections, and network errors;
- deadlocks (1213, also a Galera certification conflict) and lock-wait
  timeouts (1205);
- a connection lost before `COMMIT` was sent.

The server rolls back an open transaction when its connection ends, so a
write that fails before `COMMIT` left nothing behind and is safe to retry. The
connector counts the bytes it writes below TLS, so a `COMMIT` that never left
the client is also retried.

A connection lost after `COMMIT` was sent is different: the server may have
committed. The connector selects `uncertain` instead of retrying, and the
Result's `Receipt.IdempotencyKey` tells the application which write to look
for. The same applies when the server reports an error such as 1180 while
committing.

Dex can also run `execute` more than once for one Step execution. It does so
after a Worker is lost mid-call. It also does so when an async Step outlasts
its local phase of about seven seconds: Dex ends the local attempt and
dispatches the Step again. MySQL keeps the abandoned statement waiting until
its lock is granted and then rolls it back, because its client is gone;
MariaDB aborts it when the client disconnects. The example's live test holds a
table lock for nine seconds and observes a second `InsertRefund` dispatch reach
the server on a new connection after about seven seconds.

Every run of one Step execution uses the same idempotency key, a 36-character
UUID derived from the Dex Call ID. Set `IdempotencyKeyPlaceholder` to bind it
as the last `?`, store it in a unique column, and write with
`INSERT ... ON DUPLICATE KEY UPDATE`. A second dispatch then waits on the
first one's key and changes nothing instead of writing twice. The key covers
one Step execution; add a business unique key, such as one refund per order,
to deduplicate across Flows.

`ON DUPLICATE KEY UPDATE` fires on a conflict with any unique key, not only
the idempotency key. The runnable example assigns the primary key to itself
through `LAST_INSERT_ID(expr)`, so a conflict changes nothing, reports zero
rows affected, and reports the existing row's ID in `LastInsertID`. It then
reads the row back and compares its idempotency key to tell its own replay
from another Flow's refund for the same order. The MySQL manual warns that
with several unique indexes a conflict updates only one matching row; here the
key and `order_id` can only match the same row, because one Step execution's
key belongs to one order. `INSERT IGNORE` is not a safe substitute: it also
turns data errors into warnings.

`execute` keeps the repository's async default because most statements finish
in milliseconds. Sync durability would not remove the need for idempotency,
because a Worker lost after `COMMIT` still runs the Step execution again. Make
every write idempotent, and treat a statement that is not idempotent as a
defect in the Flow.

The runnable example's write, from
[`examples/record-refund/flow/workflow.go`](examples/record-refund/flow/workflow.go):

```go
InsertRefundStatement = `INSERT INTO refund_ledger (order_id, amount_usd, external_reference, idempotency_key)
VALUES (?, ?, ?, ?)
ON DUPLICATE KEY UPDATE refund_id = LAST_INSERT_ID(refund_id)`
```

```go
return mysql.ExecuteStatementInput{
	Statement:                 InsertRefundStatement,
	Parameters:                []any{input.OrderID, input.AmountUSD, externalReference},
	IdempotencyKeyPlaceholder: 4,
	MaxRowsAffected:           &maxRowsAffected,
}
```

MySQL has no `RETURNING` clause, so both `completed` and `uncertain` go to a
read-back by the key and the reported ID:

```go
Completed:           sdkgo.GoTo(sdkgo.StepRef[mysql.ExecuteStatementResult](findInsertedRefundStepType)),
Uncertain:           sdkgo.GoTo(sdkgo.StepRef[mysql.ExecuteStatementResult](findInsertedRefundStepType)),
ResultAttribute:     &refundInsertResultAttribute,
```

When the read-back finds no row, the example fails the Flow for review instead
of inserting again. A `COMMIT` stalled on semi-synchronous replication can
still become visible after the read.

Every call is bounded by `connectTimeout + statementTimeout + 5s` on the
client, including `COMMIT`, which the driver sends without a context, so it
returns a classified attempt before Dex's 30-second Execute timeout. Keep that
sum under the Step's Execute timeout.

## Security boundary

- The password is a `SecretString` resolved before every call. It never enters
  Flow input, Results, Receipts, Failures, or logs.
- Failures carry the server error number, its symbol, the SQLSTATE, the phase,
  and the name of a violated unique key or check constraint, read only when it
  is a plain identifier. They never carry other server message text or
  parameter values, because MySQL messages repeat values, such as the
  duplicate value in error 1062 or the account in error 1045. A duplicate-column
  defect names the column, which can be an unaliased expression from the
  statement constant.
- Receipts carry the Call ID, idempotency key, `CONNECTION_ID()`, `VERSION()`,
  error number, and SQLSTATE.
- Results hold only the rows the statement selected, bounded by `maxRows` and
  `maxResponseBytes`. The connector counts the bytes it reads below TLS and
  stops reading after `maxResponseBytes` plus 64 KiB of row data, or after
  4 MiB of any other reply, such as the handshake, column definitions, or
  `COMMIT`. A huge row is therefore never buffered whole; the driver may still
  allocate one packet buffer of up to 16 MiB that a reply's header declares.
- The connector does not log; the driver's logger is replaced with a no-op.
  The server's general and slow query logs can record statement text and
  parameter values. Configure the server accordingly.

## Runnable example

[`examples/record-refund`](examples/record-refund/README.md) records one
refund in a ledger at most once. It uses `query` for the duplicate check and
the read-back, and `execute` for the idempotent upsert, and reconciles an
uncertain `COMMIT`.

## Verification

Run the standalone module checks from this directory:

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

The default suite includes a scripted MySQL protocol server,
`internal/scriptedmysql`. It drives the real driver through
`mysql_native_password` authentication, TLS with each `sslMode`, refused TLS,
prepared statements, binary rows of every mapped type family, row and byte bounds,
server errors, dropped connections, and every `COMMIT` fault, and checks the
MariaDB statement timeout setting.

With a Dex development server running, the integration suite runs the example
Flow on a real Worker against the scripted server:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./... -count=1
```

The live suite needs a disposable database and an account with all privileges
on it. On MySQL with binary logging, also set
`log_bin_trust_function_creators = 1` so the read-only test can create a
writing function. It covers:

- the full type mapping and read-only enforcement;
- idempotent replay, constraint and limit rollbacks, and `statementTimeout`;
- two concurrent dispatches of one Step execution blocked by a table lock;
- TLS `required`, a failing `verify-identity`, and `verify-ca` with
  `MYSQL_CONNECTOR_TEST_CA_FILE`;
- a proxy, `internal/commitfaultproxy`, that drops the connection at
  `COM_STMT_EXECUTE`, before `COMMIT`, and after `COMMIT`.

It also runs the example Flow on a real Dex Worker:

```bash
MYSQL_CONNECTOR_TEST_HOST=127.0.0.1 MYSQL_CONNECTOR_TEST_PORT=3306 \
MYSQL_CONNECTOR_TEST_DATABASE=dex_connector_test MYSQL_CONNECTOR_TEST_USER=dex_test \
MYSQL_CONNECTOR_TEST_PASSWORD=... MYSQL_CONNECTOR_TEST_SSL_MODE=required \
MYSQL_CONNECTOR_TEST_CA_FILE=/path/to/mysql/data/ca.pem \
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=live ./... -count=1
```

## Limitations

- One statement per call. Multi-statement transactions, DDL, `LOCK TABLES`,
  stored procedures, `LOAD DATA`, and cursors are not supported.
- `execute` returns rows only for MariaDB `RETURNING`; MySQL has no such
  clause, so read rows back with `query`.
- There is no pool. Each Step pays a TCP, TLS, and authentication handshake,
  plus a `VERSION()` and a `SET SESSION` round trip. Put ProxySQL, RDS Proxy,
  or a similar pooler in front of a busy server.
- A private CA bundle is read from the Worker's `MYSQL_SSL_CA`; it is not yet
  a connection field. Client certificates and AWS IAM authentication tokens are
  not supported.
- TiDB, Vitess, PlanetScale, and other MySQL-compatible servers are untested;
  the session settings may not apply there.
- There are no Triggers and no Connector Studio units. Studio setup commands
  are HTTPS `GET` requests, so a table or column picker cannot be declared for
  a database.
