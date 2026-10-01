# PostgreSQL Connector

> **Verification status: live.** Verified against local PostgreSQL 17.11 with TLS on a real Dex stack; managed services such as RDS, Supabase, and Neon are not verified. See
> [verification status](../../docs/verification-status.md) for what is and
> is not verified.

This module runs parameterized SQL against a PostgreSQL database from Dex
Steps. It has two operations:

- `query` (`QueryRows`) runs one read statement inside a read-only
  transaction and returns bounded rows.
- `execute` (`ExecuteStatement`) runs one write statement in its own
  transaction, commits it, and returns the rows affected and any bounded
  `RETURNING` rows.

The connector is built on the pure-Go `github.com/jackc/pgx/v5/pgconn` wire
protocol client. Each operation opens one connection, runs one transaction,
and closes the connection. The connector keeps no pool, so a rotated password
takes effect on the next Step. It supports PostgreSQL 12 and later and was
verified against PostgreSQL 17.

## Connection setup

Dex Web **Connections** shows these fields. Every field description in
`connector.yaml` names where to find the value on Amazon RDS, Supabase, Neon,
or a self-hosted server.

| Field | Secret | Default | Meaning |
| --- | --- | --- | --- |
| `host` | no | none, required | DNS name, IP address, or Unix-socket directory of one server |
| `port` | no | `5432` | TCP port |
| `database` | no | none, required | Database name |
| `user` | no | none, required | Login role |
| `sslMode` | no | `require` | `require`, `verify-ca`, `verify-full`, or `disable` |
| `connectTimeout` | no | `5s` | TCP, TLS, and authentication bound |
| `statementTimeout` | no | `5s` | PostgreSQL `statement_timeout` for every statement |
| `maxRows` | no | `1000` | Most rows one query or `RETURNING` clause may return |
| `maxResponseBytes` | no | `1048576` | Most JSON-encoded row bytes one call may return |
| `password` | yes | none, required | The login role's password |

The authorization guide in `connector.yaml` starts at PostgreSQL's
`CREATE ROLE` reference. It tells the author to create a dedicated
least-privilege role, grant only the tables a Flow reads or writes, and rotate
or revoke the password with `ALTER ROLE`.

TLS is required by default. `sslMode` uses libpq's names:

- `require` encrypts but does not verify the server certificate.
- `verify-ca` also verifies the chain.
- `verify-full` also verifies that the certificate names the host.
- `disable` sends plaintext. `New` accepts it only for `localhost`,
  `127.0.0.1`, `::1`, or a Unix-socket directory.

`prefer` and `allow` are not offered because they silently fall back to
plaintext. `verify-ca` and `verify-full` trust the Worker's system certificate
store. A private CA bundle, such as Amazon RDS's
`https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem`, is
supplied through the standard `PGSSLROOTCERT` environment variable on the
Worker host. The connector sets host, port, database, user, password,
`sslmode`, and session parameters explicitly, so `PGHOST`, `PGUSER`,
`PGPASSWORD`, `PGSSLMODE`, `PGOPTIONS`, `PGTZ`, and `PGTARGETSESSIONATTRS`
cannot redirect or weaken a connection.

A local connection file looks like this:

```json
{
  "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
  "connections": [{
    "connectorId": "postgresql",
    "modulePath": "github.com/superdurable/dex-connectors-library/connectors/postgresql",
    "moduleVersion": "v0.1.0",
    "provider": "postgresql",
    "connectionName": "postgresql-ledger",
    "configuration": {"host": "db.example.com", "database": "app", "user": "dex_app"},
    "credentials": {"password": "..."}
  }]
}
```

## Statements and parameters

A statement is one SQL statement written as a constant in application code.
Values are bound only through positional `$1..$n` parameters. The connector
never formats a value into SQL text. It cannot detect a statement that an
application built by concatenating untrusted input, so never build
`Statement` from Flow input, email text, or model output.

The extended protocol accepts exactly one statement, so `SELECT 1; DROP ...`
is rejected by PostgreSQL. The connector prepares the statement first and
selects `defect`, before executing anything, when:

- the placeholder count differs from the bound parameters;
- two result columns share a name, which a row map cannot hold;
- the statement starts with a transaction-control keyword, such as `BEGIN`,
  `COMMIT`, `ROLLBACK`, `SAVEPOINT`, or `PREPARE TRANSACTION`, or with `COPY`.

Every parameter is sent as text with an unspecified type, so PostgreSQL parses
it for the type it infers for that placeholder. Cast a placeholder when the
type is ambiguous, such as `$1::numeric`.

| Go value | Sent as |
| --- | --- |
| `nil` | SQL `NULL` |
| `string` | the text itself; valid UTF-8 without NUL |
| `bool` | `true` or `false` |
| integer types | decimal text |
| `float32`, `float64` | shortest exact text, `NaN`, `Infinity`, or `-Infinity` |
| `json.Number` | the number's exact text |
| `json.RawMessage` | validated JSON text, for a `json` or `jsonb` placeholder |
| `[]byte` | `bytea` hex text, such as `\x0001ff` |
| `time.Time` | RFC 3339 with nanoseconds and its offset |

Other types select `defect`. Flow input decoded from JSON turns numbers into
`float64`, so pass exact decimals and integers beyond 2^53 as strings. To bind
base64 data from JSON, pass the string and decode it in SQL with
`decode($1, 'base64')`.

## Result type mapping

Every column is read as PostgreSQL text and mapped to a JSON value that
survives a JSON round trip without losing precision. Each row is a
`map[string]any` keyed by column name.

| PostgreSQL type | JSON value |
| --- | --- |
| any `NULL` | `null` |
| `bool` | `true` or `false` |
| `int2`, `int4` | number |
| `int8` | decimal string, such as `"9007199254740993"` |
| `float4`, `float8` | number, or the string `"NaN"`, `"Infinity"`, or `"-Infinity"` |
| `numeric` | exact decimal string, such as `"1200.00"`, or `"NaN"` |
| `json`, `jsonb` | the JSON value; `json` keeps its original text |
| `bytea` | standard base64 string |
| `timestamptz` | RFC 3339 in UTC, such as `"2026-01-01T00:00:00.123456Z"` |
| `timestamp` | ISO 8601 without an offset, such as `"2026-01-01T12:34:56.5"` |
| everything else | PostgreSQL's text output as a string |

Everything else includes `text`, `varchar`, `uuid`, `date` (`"2026-02-03"`),
`time`, `interval` (ISO 8601, such as `"P1DT2H"`), arrays (`"{1,2,3}"`),
enums, `inet`, `money`, and extension types. A timestamp outside years 0001
through 9999, or `infinity`, is returned as PostgreSQL's text. Use
`to_jsonb(column)` in the statement to receive an array or composite value as
JSON. `Column` reports each column's name, built-in `TypeName`, and `TypeOID`.

The connector pins these session settings inside every transaction:
`TimeZone = 'UTC'`, `DateStyle = 'ISO, MDY'`, `IntervalStyle = 'iso_8601'`,
`bytea_output = 'hex'`, `extra_float_digits = 3`, and the configured
`statement_timeout`. `TimeZone` also affects SQL such as `current_date` and
timestamps entered without an offset.

## Operations and branches

`query` sends `BEGIN TRANSACTION READ ONLY` and closes the connection without
committing. A write inside the statement fails with SQLSTATE 25006 and selects
`providerRejected`. A read-only transaction is a guard, not a permission
boundary: functions such as `dblink` or advisory locks can still have side
effects. Grant the role only `SELECT` on the tables a Flow reads.

| Branch | Selected when |
| --- | --- |
| `completed` | The statement ran and returned every row, possibly none |
| `truncated` | More rows than `maxRows` or bytes than `maxResponseBytes`; the Result keeps the rows that fit |
| `providerRejected` | Bad credentials, a missing database, table, or column, a syntax, data, or permission error, a write in the read-only transaction, `statementTimeout`, or a server that refuses TLS or fails certificate verification |
| `invalidResponse` | A value that does not match its type's text format |
| `defect` | Invalid input, credentials unavailable, or a placeholder or column-name defect |

`execute` sends `BEGIN`, which keeps the role's default access mode. It
runs the statement and reads any `RETURNING` rows, then sends `COMMIT`.
`MaxRowsAffected` guards against a missing `WHERE` clause. When the statement
affects more rows, or its `RETURNING` rows exceed `maxRows` or
`maxResponseBytes`, the connector closes the connection instead of sending
`COMMIT`. PostgreSQL then rolls the transaction back.

| Branch | Selected when |
| --- | --- |
| `completed` | PostgreSQL acknowledged `COMMIT` |
| `limitExceeded` | `MaxRowsAffected`, `maxRows`, or `maxResponseBytes` was exceeded; nothing was written |
| `providerRejected` | A constraint violation, which names its constraint, or a syntax, data, permission, or connection rejection; nothing was written |
| `uncertain` | The connection failed after `COMMIT` was sent, or the server reported an error other than SQLSTATE class 23 or 40 while committing |
| `invalidResponse` | A `RETURNING` value that does not match its type; nothing was written |
| `defect` | Invalid input, including an `IdempotencyKeyPlaceholder` that is not the last placeholder |

Only a happy path is required. Every other branch is optional, and an unwired
optional branch fails the Flow.

## Retries, uncertainty, and idempotency

Both operations return Retry, which Dex retries with the Step's policy of five
attempts within two minutes, for:

- connection refusal and connect timeouts;
- `too_many_connections` and the rest of SQLSTATE class 53;
- `admin_shutdown` and `cannot_connect_now`;
- serialization failures, deadlocks, and `lock_not_available`;
- a connection lost before `COMMIT` was sent.

PostgreSQL rolls back an open transaction when its connection ends, so a write
that fails before `COMMIT` left nothing behind and is safe to retry.

A connection lost after `COMMIT` was sent is different: the server may have
committed. The connector selects `uncertain` instead of retrying, and the
Result's `Receipt.IdempotencyKey` tells the application which write to look
for. The same applies when a backend is terminated while committing or a
synchronous-replication wait is canceled, because PostgreSQL can report an
error after committing locally.

Dex can also run `execute` more than once for one Step execution. It does so
after a Worker is lost mid-call. It also does so when an async Step outlasts
its local phase of about seven seconds: Dex starts a second dispatch while the
first is still running. The example's live test holds a table lock for nine
seconds and observes both `InsertRefund` dispatches reach PostgreSQL.

Every run of one Step execution uses the same idempotency key, a UUID derived
from the Dex Call ID. Set `IdempotencyKeyPlaceholder` to bind it as the last
placeholder, store it in a unique column, and write with
`ON CONFLICT DO NOTHING`. A second dispatch then waits on the first one's key
and affects zero rows instead of writing twice. The key covers one Step
execution; add a business unique constraint, such as one refund per order, to
deduplicate across Flows.

`execute` keeps the repository's async default because most statements finish
in milliseconds. Sync durability would not remove the need for idempotency,
because a Worker lost after `COMMIT` still runs the Step execution again. Make
every write idempotent, and treat a statement that is not idempotent as a
defect in the Flow.

The runnable example's write, from
[`examples/record-refund/flow/workflow.go`](examples/record-refund/flow/workflow.go):

```go
InsertRefundStatement = `INSERT INTO refund_ledger (order_id, amount_usd, external_reference, idempotency_key)
VALUES ($1, $2::numeric, $3, $4)
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING ` + refundColumns
```

```go
return postgresql.ExecuteStatementInput{
	Statement:                 InsertRefundStatement,
	Parameters:                []any{input.OrderID, input.AmountUSD, externalReference},
	IdempotencyKeyPlaceholder: 4,
	MaxRowsAffected:           &maxRowsAffected,
}
```

It wires `uncertain`, and a replay that returned no row, to a read-back by the
key:

```go
Completed:           sdkgo.GoTo(completeRefundRecording{}),
Uncertain:           sdkgo.GoTo(sdkgo.StepRef[postgresql.ExecuteStatementResult](findRefundByIdempotencyKeyStepType)),
```

When the read-back finds no row, the example fails the Flow for review instead
of inserting again. A `COMMIT` stalled on synchronous replication can still
become visible after the read.

Every call is bounded by `connectTimeout + statementTimeout + 5s` on the
client, so it returns a classified attempt before Dex's 30-second Execute
timeout. Keep that sum under the Step's Execute timeout. Override a Step to
sync durability only when most of its statements run longer than about seven
seconds.

## Security boundary

- The password is a `SecretString` resolved before every call. It never enters
  Flow input, Results, Receipts, Failures, or logs.
- Failures carry the SQLSTATE, its condition name, the phase, and a violated
  constraint's name. They never carry server message text, `DETAIL`, SQL
  text, or parameter values, because PostgreSQL messages can repeat values.
- Receipts carry the Call ID, idempotency key, backend process ID,
  `server_version`, and SQLSTATE.
- Results hold only the rows the statement selected, bounded by `maxRows` and
  `maxResponseBytes`. A wire message larger than `maxResponseBytes` plus
  64 KiB of framing is refused before it is buffered.
- The connector does not log. PostgreSQL's own server log can record statement
  text and, depending on `log_parameter_max_length_on_error`, parameter
  values. Configure the server accordingly.

## Runnable example

[`examples/record-refund`](examples/record-refund/README.md) records one
refund in a ledger at most once. It uses `query` for the duplicate check and
`execute` for the idempotent insert, and reconciles an uncertain `COMMIT`.

## Verification

Run the standalone module checks from this directory:

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

The default suite includes a scripted PostgreSQL wire-protocol server,
`internal/scriptedpostgresql`. It drives the real `pgconn` client through
authentication, TLS refusal, prepared statements, row bounds, server errors,
dropped connections, and every `COMMIT` fault.

With a Dex development server running, the integration suite runs the example
Flow on a real Worker against the scripted server:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./... -count=1
```

The live suite needs a disposable database role that can create tables. It
covers:

- the full type mapping and read-only enforcement;
- idempotent replay, constraint and limit rollbacks, and `statementTimeout`;
- TLS `require` and a failing `verify-full` against a self-signed server;
- a proxy, `internal/commitfaultproxy`, that drops the connection at
  `Execute`, before `COMMIT`, and after `COMMIT`.

It also runs the example Flow on a real Dex Worker:

```bash
POSTGRESQL_CONNECTOR_TEST_HOST=127.0.0.1 POSTGRESQL_CONNECTOR_TEST_PORT=5432 \
POSTGRESQL_CONNECTOR_TEST_DATABASE=dex_connector_test POSTGRESQL_CONNECTOR_TEST_USER=dex_app \
POSTGRESQL_CONNECTOR_TEST_PASSWORD=... POSTGRESQL_CONNECTOR_TEST_SSL_MODE=require \
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=live ./... -count=1
```

## Limitations

- One statement per call. Multi-statement transactions, `COPY`, `LISTEN`, and
  cursors are not supported.
- There is no pool. Each Step pays a TCP, TLS, and authentication handshake.
  Put PgBouncer, RDS Proxy, or a provider pooler in front of a busy database;
  the connector sends only `application_name` and `client_encoding` at
  startup, and sets the rest with `SET LOCAL`.
- A private CA bundle is read from the Worker's `PGSSLROOTCERT`; it is not
  yet a connection field.
- There are no Triggers and no Connector Studio units. Studio setup commands
  are HTTPS `GET` requests, so a table or column picker cannot be declared for
  a database.
