# MySQL record-refund example

This example records one refund in a MySQL or MariaDB ledger at most once, in
a Flow started from Dex Web **Start Flow**:

1. `RecordRefundRequest` validates the order ID and the exact decimal amount.
2. `FindRecordedRefund` uses `query` to read the order's existing refund in a
   read-only transaction. If one exists, the Flow completes with
   `alreadyRecorded` and writes nothing.
3. `InsertRefund` uses `execute` to run an `INSERT ... ON DUPLICATE KEY UPDATE
   refund_id = LAST_INSERT_ID(refund_id)`. The connector binds the Step
   execution's idempotency key as the fourth `?`, and `MaxRowsAffected` is
   one. The Result is also kept in the `mysql-refund-insert-result`
   Attribute.
4. MySQL has no `RETURNING` clause, so both `completed` and `uncertain` go to
   `FindInsertedRefund`, which reads the row back by the idempotency key or
   by the `LastInsertID` the insert reported.
5. `CompleteRefundRecording` completes with:
   - `recorded` when the row carries this Step's key, with `wasReconciled`
     when the insert was uncertain or a replay changed nothing;
   - `alreadyRecorded` when another Flow recorded the order between the
     duplicate check and the insert, because `ON DUPLICATE KEY UPDATE` turned
     the conflict on `order_id` into a no-op and reported that row's ID.

   When an uncertain insert left no row, it fails the Flow for review instead
   of inserting again.

`ON DUPLICATE KEY UPDATE` is the idempotent write: a replayed dispatch of the
same Step execution conflicts on `idempotency_key` and changes nothing, and
another Flow for the same order conflicts on `order_id` and changes nothing.
Assigning `refund_id` to itself reports zero rows affected, and
`LAST_INSERT_ID(refund_id)` reports the existing row, so the read-back always
finds the row that won. The other optional branches, `limitExceeded`,
`providerRejected`, `truncated`, `invalidResponse`, and `defect`, are not
wired and fail the Flow.

## Prepare the database

Create the table as its owner, and grant the Dex account only what the
example needs. `ON DUPLICATE KEY UPDATE` needs `UPDATE` as well as `INSERT`:

```bash
mysql --host=db.example.com --user=app_owner --password --ssl-mode=REQUIRED app < examples/record-refund/schema.sql
mysql --host=db.example.com --user=app_owner --password --ssl-mode=REQUIRED app \
  -e "GRANT SELECT, INSERT, UPDATE ON app.refund_ledger TO 'dex_app'@'%'"
```

The schema needs MySQL 8.0.16 or later, or MariaDB 10.6 or later, for its
`CHECK` constraint.

## Run locally

Run these commands from `connectors/mysql`. Generate strict FDG 2.0:

```bash
mkdir -p /tmp/mysql-refund/graphs /tmp/mysql-refund/release
dexcli visualize ./examples/record-refund/flow/workflow.go \
  --schema-version 2.0 --json --out /tmp/mysql-refund/graphs/record-refund
```

Until the connector is released, build a local release override from the
repository root:

```bash
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/mysql/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/mysql \
  --version v0.21.0 --tag connectors/mysql/v0.21.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/mysql-refund/release/connector-release.json \
  --digest-output /tmp/mysql-refund/release/connector-release.json.sha256
dexcli dev --open=false --flow-rendering-dir /tmp/mysql-refund/graphs \
  --connector-release-override mysql=/tmp/mysql-refund/release
```

Open Dex Web at `http://127.0.0.1:8802`. In **Connections**, save
`mysql / mysql-ledger` with the host, database, user, and password of the
account that received the grant. Keep the defaults unless your server needs
another port, a stricter `sslMode`, or longer timeouts. The connection must
show **Ready** and **Local override**. Dex Web or Superverse Studio writes the
connection to the project configuration. In another terminal, start the Worker
with the `DEX_PROJECT_*` environment that names that configuration, as
[project configuration loading](../../../../sdkgo/projectconfig/README.md#application-loading)
describes:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
go run ./examples/record-refund
```

For `verify-ca` or `verify-identity` against a private CA, such as Amazon
RDS, also set `MYSQL_SSL_CA` to the PEM bundle's path in the Worker's
environment.

In **Start Flow**, choose `MySQLRecordRefund` and the Worker at
`127.0.0.1:8827`. Start it with a unique Flow ID:

```json
{"orderId": "88213", "amountUsd": "250.00", "externalReference": "tkt_5488"}
```

The Flow completes with `status: recorded` and the ledger row, with
`refund_id` as a string, `amount_usd` as the exact string `"250.00"`, and
`recorded_at` in RFC 3339 UTC. Start another Flow with the same `orderId` to
see `alreadyRecorded` without a second row. **Display** shows the request and
outcome Attributes. Confirm the side effect with a bounded read-back:

```bash
mysql --host=db.example.com --user=dex_app --password --ssl-mode=REQUIRED app \
  -e "SELECT refund_id, order_id, amount_usd, idempotency_key FROM refund_ledger WHERE order_id = '88213'"
```

Delete the disposable row afterward as the table owner.

## Test

```bash
GOWORK=off go test -race ./examples/record-refund/...
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/record-refund/... -count=1
```

The integration suite runs the Flow on a real Dex Worker against the scripted
MySQL server. It covers:

- a new refund and a duplicate order;
- another Flow's refund committed between the duplicate check and the insert;
- a `COMMIT` whose reply was dropped, once after it applied and once before;
- a write lost before `COMMIT` and retried with the same key;
- a nine-second `INSERT` that Dex dispatches again after the async local
  phase, which still writes one row;
- invalid input rejected before connecting.

The live suite, `-tags=live` with the `MYSQL_CONNECTOR_TEST_*` variables from
the connector README, creates the table from `schema.sql` and runs the same
Flow against a real server. It covers concurrent Flows for one order, which
both complete with one row, an `INSERT` blocked for nine seconds by
`LOCK TABLES ... READ` and dispatched again on a new connection, and a real
proxy-dropped `COMMIT`.
