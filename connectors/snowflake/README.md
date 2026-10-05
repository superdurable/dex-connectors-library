# Snowflake Connector

> **Verification status: Dex-integrated.** The operations and the runnable example ran on a real
> Dex stack against a scripted stand-in for the Snowflake SQL API; no real Snowflake account was
> used. See [Unverified live behavior](#unverified-live-behavior) and
> [verification status](../../docs/verification-status.md).

This module runs parameterized SQL in a Snowflake warehouse from Dex Steps through the
[Snowflake SQL API](https://docs.snowflake.com/en/developer-guide/sql-api/reference). A warehouse
statement can run for minutes or hours, so no operation waits for one. A Flow composes three
operations around a durable Timer:

- `submitStatement` (`SubmitStatement`) sends one statement with `async=true` and returns its
  statement handle.
- `getStatementResult` (`GetStatementResult`) reads the statement once: still `running`, or
  finished with its column metadata and the bounded rows of one result partition.
- `cancelStatement` (`CancelStatement`) asks Snowflake to cancel a statement that is still queued
  or running.

```text
submitStatement -> Timer -> getStatementResult -running-> Timer -> ... -completed-> rows
                                               \-> cancelStatement after the Flow's wait budget
```

The connector uses the SQL API over HTTPS, not the Go driver, because the API's statement handle
lets a Flow wait durably between short, idempotent requests. Every request goes to
`https://<accountIdentifier>.snowflakecomputing.com/api/v2/statements`.

## Connection setup

Dex Web **Connections** shows these fields. Each description in `connector.yaml` names the
Snowsight page or SQL command that shows the value.

| Field | Secret | Default | Meaning |
| --- | --- | --- | --- |
| `accountIdentifier` | no | none, required | `orgname-account_name`, such as `myorg-analytics`, or a locator such as `xy12345.us-east-2.aws`; optional `.privatelink` suffix |
| `warehouse` | no | the user's `DEFAULT_WAREHOUSE` | Virtual warehouse, case-sensitive as `SHOW WAREHOUSES` lists it |
| `role` | no | the user's `DEFAULT_ROLE` | Role, case-sensitive |
| `database` | no | the user's `DEFAULT_NAMESPACE` | Database for unqualified names |
| `schema` | no | the user's `DEFAULT_NAMESPACE` | Schema for unqualified names |
| `statementTimeoutSeconds` | no | `3600` | The SQL API `timeout` field, 1 through 604800 |
| `maxRows` | no | `1000` | Most rows one `getStatementResult` call returns from one partition |
| `maxResponseBytes` | no | `1048576` | Most JSON-encoded row bytes one call returns |

`New` validates the account identifier strictly. It rejects a value with a scheme, a path, or
`.snowflakecomputing.com`, an organization name with underscores, and the hyphenated URL form of an
account name (`myorg-my-account`), because the JWT needs the account name with its underscores.
Accounts on `snowflakecomputing.cn` are not supported. The host is per account, so Studio setup
commands, which need a fixed host, cannot list warehouses, roles, or databases: the connector has
no pickers, and these fields are typed.

### Authorization methods

| Method | Type | Credential fields | Header sent |
| --- | --- | --- | --- |
| `key-pair` (default, recommended) | `serviceAccount` | `user`, `private_key` | `Authorization: Bearer <JWT>`, `X-Snowflake-Authorization-Token-Type: KEYPAIR_JWT` |
| `programmatic-access-token` | `apiKey` | `programmatic_access_token` | `Authorization: Bearer <token>`, `X-Snowflake-Authorization-Token-Type: PROGRAMMATIC_ACCESS_TOKEN` |

For key-pair authentication the connector signs a fresh RS256 JWT for every request with the
documented claims: `iss` is `ACCOUNT.USER.SHA256:<fingerprint>`, `sub` is `ACCOUNT.USER`, `iat` is
now, and `exp` is 59 minutes later. `ACCOUNT` is the part of `accountIdentifier` before its first
period, upper-cased, so region, cloud, and `.privatelink` segments are dropped. `USER` is the
`user` field upper-cased; Snowflake matches it against the user's `LOGIN_NAME`, not its `NAME`,
when the two differ. The fingerprint is the base64 SHA-256 of the public key's DER encoding, the value
`DESC USER` shows as `RSA_PUBLIC_KEY_FP`. The key must be an unencrypted PKCS #8 or PKCS #1 RSA PEM
key; an encrypted key selects `defect` because the connector has no passphrase.
`sdkgo/oauthtoken.SignJWTBearerAssertion` is not used: it requires an `aud` claim, which Snowflake
does not document. The connector uses `oauthtoken.ParseRSAPrivateKeyPEM` to read the key.

Nothing is refreshed: a JWT is signed per request and a programmatic access token is static, so
`auth.refreshable` is false and the connector stores no token. A programmatic access token expires
after the days chosen when it was generated; afterwards every operation selects
`providerRejected` with `AUTHENTICATION`, and a new token must be saved. OAuth is not offered:
Snowflake's authorization endpoint is per account, and manifest OAuth endpoints are static URLs.

Open the connection at application startup, as
[`examples/account-usage-summary/main.go`](examples/account-usage-summary/main.go) does:

```go
connection, err := snowflake.NewProjectConnection(project, accountusagesummary.ConnectionName)
```

## Statements and bindings

A statement is one SQL statement written as a constant in application code. Values are bound only
through `?` placeholders; the connector never formats a value into SQL text and cannot detect a
statement built by concatenating untrusted input. Before any request the connector counts `?`
outside string literals (`'...'`, with `''` and backslash escapes), `$$...$$` strings, quoted
identifiers, and `--`, `//`, and `/* */` comments, and selects `defect` when the count differs from
the bound parameters. `:1` and `:name` placeholders are not supported.

Every submission sets `MULTI_STATEMENT_COUNT` to `1`, so Snowflake rejects a request with several
statements. The SQL API accepts `BEGIN`, `COMMIT`, `USE`, `ALTER SESSION`, and temporary tables
only in multi-statement requests, and it does not support `PUT` or `GET`.

| Go value | Binding type and value |
| --- | --- |
| `nil` | `TEXT`, JSON `null` |
| `string` | `TEXT`; valid UTF-8 without NUL |
| `bool` | `BOOLEAN`, `true` or `false` |
| integer types | `FIXED`, decimal text |
| finite `float32`, `float64` | `REAL`, shortest exact text |
| `json.Number` | `FIXED` for an integer, otherwise `TEXT` |
| `json.RawMessage` | `TEXT`; wrap the placeholder in `PARSE_JSON(?)` |
| `[]byte` | `BINARY`, hex |
| `time.Time` | `TIMESTAMP_TZ`, epoch nanoseconds and the offset plus 1440 minutes |

Cast a placeholder when the type matters, such as `?::NUMBER(12,2)` or `TO_DATE(?)`.

## Result type mapping

Snowflake's `jsonv2` format returns every value as a string. The connector maps each one to a JSON
value that survives a JSON round trip without losing precision. Each row is a `map[string]any`
keyed by column name; a statement that returns one name twice selects `defect`, so give each
result column a unique alias.

| Snowflake type (`rowType.type`) | JSON value |
| --- | --- |
| any `NULL` | `null` |
| `fixed` with scale 0 and precision 1 through 15 | number |
| other `fixed` (`NUMBER`, including `COUNT(*)`, which is `NUMBER(18,0)`) | exact decimal string, such as `"1200.00"` |
| `real` | number, or `"NaN"`, `"Infinity"`, `"-Infinity"` |
| `decfloat` | decimal string, possibly in scientific notation |
| `text` | string |
| `boolean` | `true` or `false` |
| `binary` | standard base64 string |
| `date` | `"2026-01-13"` |
| `time` | `"23:01:59.5"` |
| `timestamp_ntz` | ISO 8601 without an offset, such as `"2026-01-01T00:00:00.5"` |
| `timestamp_ltz` | RFC 3339 in UTC |
| `timestamp_tz` | RFC 3339 with the value's own offset |
| `variant`, `object`, `array`, `map` | the JSON value, with its original text |
| everything else, such as `geography` | Snowflake's text |

A date or timestamp outside years 0001 through 9999 is returned as Snowflake's text. `Columns`
reports each column's name, lower-case type, precision, scale, length, and nullability.

## Operations and branches

### submitStatement

`POST /api/v2/statements?requestId=<key>&retry=true&async=true` with the statement, its bindings,
the connection's warehouse, role, database, schema, and `timeout`, and `MULTI_STATEMENT_COUNT`.

| Branch | Selected when |
| --- | --- |
| `submitted` | Snowflake answered 202 (or 200) with a statement handle |
| `providerRejected` | 422 (a compilation error, an unknown object, a privilege error), 400, 401, 403, 404, 408, a redirect, or another 4xx; nothing ran |
| `defect` | Invalid input, a placeholder mismatch, or unusable credentials; no request was sent |

429, 500, 502, 503, 504, a dropped connection, and a 2xx without a readable handle return Retry,
after `Retry-After` when Snowflake sends one.

### getStatementResult

`GET /api/v2/statements/<handle>`, with `?partition=<n>` for a later partition.

| Branch | Selected when |
| --- | --- |
| `completed` | 200: the statement finished; the Result holds every row of the partition within the bounds |
| `running` | 202: the statement is queued or running |
| `truncated` | 200, but the partition held more than `maxRows` rows or `maxResponseBytes` of encoded rows; the Result keeps the rows that fit |
| `providerRejected` | 422 for a failed statement, a canceled one (code `000604`, SQLSTATE `57014`), or an unknown handle (`000709`); 408; 401, 403, 404, or another 4xx |
| `invalidResponse` | A malformed body, a body over 256 MiB, a row whose width differs from its columns, or a value that does not match its type |
| `defect` | A handle that is not UUID-shaped, a negative partition, a later partition without `Columns`, or duplicate column names |

Partition 0 carries the column metadata, `PartitionCount`, and `TotalRowCount`. Snowflake sends
later partitions gzip-compressed and without metadata, so `GetStatementResultInput.Columns` must
carry the partition-0 `Columns` to read partition 1 or later. The body is streamed: rows beyond the
bounds are skipped, never buffered.

### cancelStatement

`POST /api/v2/statements/<handle>/cancel?requestId=<key>`.

| Branch | Selected when |
| --- | --- |
| `canceled` | 200: Snowflake accepted the cancellation |
| `providerRejected` | 422, such as `000709` for an unknown handle, or 401, 403, 404, or another 4xx |
| `defect` | A handle that is not UUID-shaped, or unusable credentials |

Only the happy path of each operation is required; every other branch is optional, and an unwired
optional branch fails the Flow. Failures carry the HTTP status, Snowflake's six-digit code, and the
SQLSTATE. Receipts carry the statement handle as `ProviderObjectID` and the code and SQLSTATE as
`code` and `sqlState` metadata.

## Retries, duplicates, and durability

Every operation keeps the repository's async Execute durability with a 30-second Execute timeout
and five attempts within two minutes. Each HTTP request is bounded by 20 seconds. No request waits
for a statement: `async=true` returns as soon as Snowflake queues it.

Dex can dispatch one Step execution more than once: after a Worker is lost, and when an async Step
outlasts its local phase of about seven seconds while the first dispatch is still in flight.
`submitStatement` relies on Snowflake's request ID for both, without a heartbeat checkpoint. Its
idempotency key is the connector Call ID, a UUID that every attempt of one Step execution shares,
and it is sent as Snowflake's `requestId` with `retry=true` on every attempt. Snowflake documents
that a resubmission with the same `requestId` and `retry=true` does not execute the statement again
if it has already executed successfully, so the operation returns Retry, never `uncertain`, after a
lost response. Snowflake does not document a resubmission that arrives while the first statement
is still queued or running, which is the usual case for a duplicate dispatch of an async submit;
this connector assumes it returns the same statement, and that is unverified against the live API.
A statement that failed may run again on a resubmission. The example's integration test answers the
first submission after nine seconds; Dex dispatches the Step again, both requests carry the same
`requestId`, and the scripted API runs the statement once. Snowflake notes that `retry=true` adds a
statement-history lookup to each submission.

`getStatementResult` is a read, and canceling twice is harmless, so both retry freely. The
idempotency key covers one Step execution; a Flow that must not run the same business statement
twice across Flows needs its own guard, such as a `MERGE` keyed by a business ID.

## Security boundary

- `private_key` and `programmatic_access_token` are `SecretString` values resolved before every
  request. They and the JWTs signed from them never enter Flow input, Results, Receipts,
  Failures, or logs.
- Failures and Receipts carry only the HTTP status, Snowflake's code, SQLSTATE, and statement
  handle, each checked against its format. Snowflake's message text is never decoded, because it
  can repeat SQL text and values.
- Results hold only the rows the statement selected, bounded by `maxRows` and `maxResponseBytes`.
- The HTTP client never follows a redirect, so a credential is never replayed to another host.
- The connector does not log. Snowflake's query history records statement text; bound values are
  not part of the text.

## Runnable example

[`examples/account-usage-summary`](examples/account-usage-summary/README.md) submits a long
aggregate over a usage table, waits on a durable Timer between status reads, records a failed
statement's code and SQLSTATE, and cancels the statement when its wait budget runs out.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./... -count=1
```

The default suite drives every operation against `internal/fakesnowflake`, a scripted SQL API that
verifies each RS256 JWT's signature, `iss` fingerprint, `sub`, and lifetime, deduplicates by
`requestId` with `retry=true`, gzip-compresses later partitions, and puts the word `SENTINEL` in
every message so tests prove no provider text reaches a Result. The integration suite runs the
example Flow on a real Dex Worker against the same API.

## Unverified live behavior

No real Snowflake account was used. These behaviors follow Snowflake's documentation and are
unverified against the live API:

- whether `retry=true` with a known `requestId` returns the same handle while that statement is
  still running, not only after it succeeded, and that `retry=true` on a first submission
  executes normally;
- the JWT account segment for locator and `.privatelink` identifiers, and whether the hyphen form
  of an account name with underscores is accepted in the host;
- the `jsonv2` text of `real` special values, `time`, `timestamp_tz`, and semi-structured columns,
  and whether a 2xx partition response is a bare row array or an object with `data`;
- the `TIMESTAMP_TZ` binding's offset encoding and a `null` binding value;
- the response to canceling a statement that already finished, and Snowflake's codes for a
  failed, canceled, or timed-out statement on `GET`;
- `MULTI_STATEMENT_COUNT` in upper case in the request's `parameters`.

## Limitations

- One statement per request; multi-statement transactions, `PUT`, and `GET` are not supported.
- No Triggers. A change-polling Trigger would query a watermark column, such as
  `WHERE UPDATED_AT > ?`, and needs a durable per-binding cursor to resume from after a restart;
  `sdkgo` has no such cursor yet, so the Trigger is deferred. Snowflake streams and tasks have no
  push channel the connector could receive.
- No Studio pickers: setup commands need a fixed HTTPS host, and every Snowflake account has its
  own host.
- OAuth and workload identity federation are not offered.
