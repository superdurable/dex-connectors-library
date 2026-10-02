# SQL Server record-refund example

This example records one refund in a SQL Server or Azure SQL ledger at most once, in a Flow started
from Dex Web **Start Flow**:

1. `RecordRefundRequest` validates the order ID and the exact decimal amount.
2. `FindRecordedRefund` uses `query` to read the order's existing refund in a transaction that is
   rolled back. If one exists, the Flow completes with `alreadyRecorded` and writes nothing.
3. `InsertRefund` uses `execute` to run an `INSERT ... OUTPUT ... SELECT ... WHERE NOT EXISTS` whose
   existence check holds `UPDLOCK, HOLDLOCK`. The connector binds the Step execution's idempotency
   key as `@p4`, `MaxRowsAffected` is one, and `ReturnsOutputRows` returns the inserted row. The
   Result is also kept in the `sql-server-refund-insert-result` Attribute.
4. Both `completed` and `uncertain` go to `CompleteRefundRecording`, which completes with
   `recorded` and the `OUTPUT` row. When the insert was uncertain or inserted nothing, it continues
   to `FindInsertedRefund`, which reads back by the idempotency key or the order.
5. `ResolveInsertedRefund` completes with:
   - `recorded`, with `wasReconciled`, when the row carries this Step's key: a replay or an
     uncertain `COMMIT` that applied;
   - `alreadyRecorded` when another Flow recorded the order between the duplicate check and a
     completed insert, which then inserted nothing.

   When an uncertain insert left no row, it fails the Flow for review instead of inserting again.

The other optional branches, `limitExceeded`, `providerRejected`, `truncated`, `invalidResponse`, and
`defect`, are not wired and fail the Flow.

## Prepare the database

Create the table as its owner in the application database, and grant the Dex user only what the
example needs:

```bash
sqlcmd -S myserver.database.windows.net -d app -U app_owner -N -i examples/record-refund/schema.sql
sqlcmd -S myserver.database.windows.net -d app -U app_owner -N -Q "GRANT SELECT, INSERT ON dbo.refund_ledger TO dex_app"
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
  --module-path github.com/superdurable/dex-connectors-library/connectors/microsoft/sql-server \
  --version v0.1.0 --tag connectors/microsoft/sql-server/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/sql-server-refund/release/connector-release.json \
  --digest-output /tmp/sql-server-refund/release/connector-release.json.sha256
dexcli dev --open=false --flow-rendering-dir /tmp/sql-server-refund/graphs \
  --connector-config-dir /tmp/sql-server-refund/connections \
  --connector-release-override microsoft-sql-server=/tmp/sql-server-refund/release
```

Open Dex Web at `http://127.0.0.1:8802`. In **Connections**, save
`microsoft-sql-server / sql-server-ledger` with the host, database, user, and password of the user
that received the grant. Keep the defaults unless your server needs another port, `strict`, or
longer timeouts. The connection must show **Ready**. In another terminal, start the Worker:

```bash
DEX_CONNECTOR_CONFIG_FILE=/tmp/sql-server-refund/connections/connections.json \
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
go run ./examples/record-refund
```

For a private CA, also set `SQLSERVER_SSL_CA` to the PEM bundle's path in the Worker's environment.

In **Start Flow**, choose `SQLServerRecordRefund` and the Worker at `127.0.0.1:8828`. Start it with a
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

- a new refund returned through `OUTPUT`, and a duplicate order;
- another Flow's refund committed between the duplicate check and the insert;
- a `COMMIT` whose reply was dropped, once after it applied and once before;
- a write lost before `COMMIT` and retried with the same key;
- a nine-second `INSERT` that Dex dispatches again after the async local phase, which still writes
  one row;
- invalid input rejected before connecting.

The live suite, `-tags=live` with the `SQLSERVER_CONNECTOR_TEST_*` variables from the connector
README, creates the table from `schema.sql`, runs the same Flow against a real server, including two
concurrent Flows for one order, and drops the table. It has not been run.
