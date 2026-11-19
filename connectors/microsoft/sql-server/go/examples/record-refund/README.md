# SQL Server record-refund example

This example records one refund in a SQL Server or Azure SQL ledger at most once, in a Flow started
from Dex Web **Start Flow**:

1. `RecordRefundRequest` validates the order ID and the exact decimal amount.
2. `FindRecordedRefund` uses `query` to read the order's existing refund in a transaction that is
   rolled back. If one exists, the Flow completes with `alreadyRecorded` and writes nothing.
3. `InsertRefund` uses `execute` to run an `INSERT ... OUTPUT ... SELECT ... WHERE NOT EXISTS` whose
   existence check holds `UPDLOCK, HOLDLOCK`. The connector binds the Step execution's idempotency
   key as `@p4`, `MaxRowsAffected` is one, and `ReturnsOutputRows` returns the inserted row. The
   Result is also kept in the `sql-server-refund-insert-result` Attribute. `execute` is
   repeat-safe, so a `COMMIT` with an unknown outcome returns Retry and Dex sends the same `INSERT`
   again with the same key.
4. `completed` goes to `CompleteRefundRecording`, which records `recorded` and the `OUTPUT` row.
   When the insert inserted nothing, it continues to `FindInsertedRefund`, which reads back by the
   idempotency key or the order.
5. `ResolveInsertedRefund` records:
   - `recorded`, with `wasReconciled`, when the row carries this Step's key: a replay whose earlier
     `COMMIT` applied;
   - `alreadyRecorded` when another Flow recorded the order between the duplicate check and a
     completed insert, which then inserted nothing; the Flow completes.

   When the read-back finds no row, it fails the Flow for review instead of inserting again.
6. `AddRefundToDailyTotal` uses `executeNonIdempotent` to run a `MERGE` that adds a new refund to
   today's UTC total in `dbo.refund_daily_totals`. Running it twice would count the refund twice,
   so the connector commits it at most once. An `uncertain` daily total is not wired and fails the
   Flow for review; inspect `dbo.refund_daily_totals` before correcting it by hand.
7. `CompleteDailyTotal` completes with `isAddedToDailyTotal`.

The other optional branches, `limitExceeded`, `providerRejected`, `truncated`, `invalidResponse`, and
`defect`, are not wired and fail the Flow.

## Prepare the database

Create the tables as their owner in the application database, and grant the Dex user only what the
example needs:

```bash
sqlcmd -S myserver.database.windows.net -d app -U app_owner -N -i examples/record-refund/schema.sql
sqlcmd -S myserver.database.windows.net -d app -U app_owner -N -Q "GRANT SELECT, INSERT ON dbo.refund_ledger TO dex_app; GRANT SELECT, INSERT, UPDATE ON dbo.refund_daily_totals TO dex_app"
```

The schema needs SQL Server 2016 or later, Azure SQL Database, or Azure SQL Managed Instance.

## Run locally

Run these commands from `connectors/microsoft/sql-server`. Generate strict FDG 2.0:

```bash
mkdir -p /tmp/sql-server-refund/graphs /tmp/sql-server-refund/release
dexcli visualize ./examples/record-refund/flow/workflow.go \
  --schema-version 2.0 --json --out /tmp/sql-server-refund/graphs/record-refund
```

Until the connector is released, build a local release override from the repository root:

```bash
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/microsoft/sql-server/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/microsoft/sql-server/go \
  --version v0.24.0 --tag connectors/microsoft/sql-server/go/v0.24.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/sql-server-refund/release/connector-release.json \
  --digest-output /tmp/sql-server-refund/release/connector-release.json.sha256
dexcli dev --open=false --flow-rendering-dir /tmp/sql-server-refund/graphs \
  --connector-release-override microsoft-sql-server=/tmp/sql-server-refund/release
```

Open Dex Web at `http://127.0.0.1:8802`. In **Connections**, save
`microsoft-sql-server / sql-server-ledger` with the host, database, user, and password of the user
that received the grant. Keep the defaults unless your server needs another port, `strict`, or
longer timeouts. The connection must show **Ready**. In another terminal, start the Worker. It reads
the `DEX_PROJECT_*` project configuration environment documented in
[`sdkgo/projectconfig`](../../../../../../sdkgo/projectconfig/README.md#application-loading); Dex Web
or Superverse Studio writes that configuration:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
go run ./examples/record-refund
```

For a private CA, also set `SQLSERVER_SSL_CA` to the PEM bundle's path in the Worker's environment.

In **Start Flow**, choose `SQLServerRecordRefund`. Start it with a
unique Flow ID:

```json
{"orderId": "88213", "amountUsd": "250.00", "externalReference": "tkt_5488"}
```

The Flow completes with `status: recorded` and the ledger row, with `refund_id` as a string,
`amount_usd` as the exact string `"250.00"`, `idempotency_key` in lowercase, and `recorded_at` in
UTC without an offset. Start another Flow with the same `orderId` to see `alreadyRecorded` without a
second row. **Display** shows the request and outcome Attributes. Confirm the side effect with a
bounded read-back, then delete the disposable row as the table owner:

```bash
sqlcmd -S myserver.database.windows.net -d app -U dex_app -N \
  -Q "SELECT refund_id, order_id, amount_usd, idempotency_key FROM dbo.refund_ledger WHERE order_id = N'88213'"
```

## Test

```bash
GOWORK=off go test -race ./examples/record-refund/...
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/record-refund/... -count=1
```

The integration suite runs the Flow on a real Dex Worker against the scripted TDS server, whose
ledger makes an `INSERT` wait on a key or order another transaction holds, as `UPDLOCK, HOLDLOCK`
does. It covers:

- a new refund returned through `OUTPUT`, its daily total, and a duplicate order;
- another Flow's refund committed between the duplicate check and the insert;
- an `INSERT` `COMMIT` whose reply was dropped, once after it applied and once before, each
  retried with the same key;
- a daily-total `COMMIT` whose reply was dropped, which selects `uncertain` and is never sent
  again;
- a write lost before `COMMIT` and retried with the same key;
- an `INSERT` committed before its Worker is lost and dispatched again with the
  same key, which still writes one row;
- invalid input rejected before connecting.

The live suite, `-tags=live` with the `SQLSERVER_CONNECTOR_TEST_*` variables from the connector
README, creates the table from `schema.sql`, runs the same Flow against a real server, including two
concurrent Flows for one order, and drops the table. It has not been run.
