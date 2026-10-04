# PostgreSQL record-refund example

This example records one refund in a PostgreSQL ledger at most once, in a
Flow started from Dex Web **Start Flow**:

1. `RecordRefundRequest` validates the order ID and the exact decimal amount.
2. `FindRecordedRefund` uses `query` to read the order's existing refund in a
   read-only transaction. If one exists, the Flow completes with
   `alreadyRecorded` and writes nothing.
3. `InsertRefund` uses `execute` to run an `INSERT ... ON CONFLICT
   (idempotency_key) DO NOTHING ... RETURNING`. The connector binds the Step
   execution's idempotency key as `$4`, and `MaxRowsAffected` is one.
4. `CompleteRefundRecording` completes with `recorded` and the returned row.
5. When the insert selects `uncertain`, or a replay of the same Step
   execution inserted nothing, `FindRefundByIdempotencyKey` reads the row back
   by that key. `CompleteReconciledRefund` completes with `wasReconciled` when
   the row exists. Otherwise it fails the Flow for review instead of inserting
   again.

The ledger's `order_id` unique constraint admits one refund per order across
Flows. A second Flow that races past the duplicate check fails at
`InsertRefund` with `providerRejected` (SQLSTATE 23505). The other optional
branches, `limitExceeded`, `truncated`, `invalidResponse`, and `defect`, are
not wired and fail the Flow.

## Prepare the database

Create the table as its owner, and grant the Dex role only what the example
needs:

```bash
psql "host=db.example.com dbname=app user=app_owner sslmode=require" -f examples/record-refund/schema.sql
psql "host=db.example.com dbname=app user=app_owner sslmode=require" \
  -c "GRANT SELECT, INSERT ON refund_ledger TO dex_app"
```

## Run locally

Run these commands from `connectors/postgresql`. Generate strict FDG 2.0:

```bash
mkdir -p /tmp/postgresql-refund/graphs /tmp/postgresql-refund/release
dexcli visualize ./examples/record-refund/flow/workflow.go \
  --schema-version 2.0 --json --out /tmp/postgresql-refund/graphs/record-refund
```

Until the connector is released, build a local release override from the
repository root:

```bash
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/postgresql/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/postgresql \
  --version v0.21.0 --tag connectors/postgresql/v0.21.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/postgresql-refund/release/connector-release.json \
  --digest-output /tmp/postgresql-refund/release/connector-release.json.sha256
dexcli dev --open=false --flow-rendering-dir /tmp/postgresql-refund/graphs \
  --connector-release-override postgresql=/tmp/postgresql-refund/release
```

Open Dex Web at `http://127.0.0.1:8802`. In **Connections**, save
`postgresql / postgresql-ledger` with the host, database, user, and password
of the role that received the grant. Keep the defaults unless your server needs
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

In **Start Flow**, choose `PostgreSQLRecordRefund` and the Worker at
`127.0.0.1:8826`. Start it with a unique Flow ID:

```json
{"orderId": "88213", "amountUsd": "250.00", "externalReference": "tkt_5488"}
```

The Flow completes with `status: recorded` and the ledger row, with
`refund_id` as a string, `amount_usd` as the exact string `"250.00"`, and
`recorded_at` in RFC 3339 UTC. Start another Flow with the same `orderId` to
see `alreadyRecorded` without a second row. **Display** shows the request and
outcome Attributes. Confirm the side effect with a bounded read-back:

```bash
psql "host=db.example.com dbname=app user=dex_app sslmode=require" \
  -c "SELECT refund_id, order_id, amount_usd, idempotency_key FROM refund_ledger WHERE order_id = '88213'"
```

Delete the disposable row afterward as the table owner.

## Test

```bash
GOWORK=off go test -race ./examples/record-refund/...
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/record-refund/... -count=1
```

The integration suite runs the Flow on a real Dex Worker against the scripted
PostgreSQL server. It covers:

- a new refund and a duplicate order;
- a `COMMIT` whose reply was dropped, once after it applied and once before;
- a write lost before `COMMIT` and retried with the same key;
- an eight-second `INSERT` that Dex dispatches again after the async local
  phase, which still writes one row;
- invalid input rejected before connecting.

The live suite, `-tags=live` with the `POSTGRESQL_CONNECTOR_TEST_*` variables
from the connector README, runs the same Flow against a real database. It
covers concurrent Flows for one order, an `INSERT` blocked for nine seconds by
a table lock and dispatched twice, and a real proxy-dropped `COMMIT`.
