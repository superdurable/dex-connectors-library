# MySQL Python connector

`dex-connector-mysql` exposes the `query` and `execute` operations from the MySQL connector manifest to Python Dex Flows. It uses a native MySQL prepared-statement driver and opens one connection per operation attempt, so a rotated password takes effect on the next attempt. MySQL 8.0+ and MariaDB 10.6+ are the target servers. The connector package is `dex_connectors.mysql`.

## Install

```sh
uv add dex-connector-mysql==0.1.0
```

The package requires `dex-connectors-sdk==0.1.0`, `dex-python-sdk>=1.28.0`, and `mysql-connector-python>=9.0`.

## Configure a connection

Create a MySQL account with only the table permissions the Flow needs. Dex Web **Connections** accepts the server `host`, `port`, `database`, `user`, `sslMode`, `connectTimeout`, `statementTimeout`, `maxRows`, `maxResponseBytes`, and secret `password` described in [the connector README](../README.md#connection-setup). The manifest's authorization guide links to MySQL account creation and names the cloud console paths for managed databases. `new_project_connection` loads ordinary settings and resolves the password for every operation attempt. Keep connections and credentials outside Flow state.

The default `sslMode` requires TLS. `disabled` is accepted only on loopback or a local Unix socket. Set `MYSQL_SSL_CA` on the Worker when a private CA must be trusted. A read runs in a read-only transaction. A write uses server-side prepared parameters and commits only after returned rows and `maxRowsAffected` pass the configured limits.

Result decoding preserves MySQL `FLOAT`, `YEAR`, signed `TIME`, and text with binary collations. JSON values whose numbers cannot survive Python's JSON codec exactly return `invalidResponse` with the column name instead of a rounded value. `rowsAffected`, `lastInsertId`, and `warningCount` come from the statement before its prepared cursor closes.

## Run the example

[`examples/record_refund/flow.py`](examples/record_refund/flow.py) defines `RecordRefundFlow` with static `ConnectionName` `mysql-ledger`. It checks for an existing order with `query`, then uses `execute` for an idempotent insert. A possibly committed write is read back by its Step idempotency key. The runnable [`worker.py`](examples/record_refund/worker.py) contains the application registration:

```python
connection = new_project_connection(project, CONNECTION_NAME)
registry = dex.Registry([RecordRefundFlow(connection)], allow_async_handlers=True)
```

Create the ledger table from [`schema.sql`](examples/record_refund/schema.sql) as the table owner and grant the Dex account `SELECT`, `INSERT`, and `UPDATE` on that table. Save `mysql / mysql-ledger` in Dex Web, then run from this package directory:

```sh
uv run python -m examples.record_refund.worker
```

Start `MySQLPythonRecordRefund` with a unique Flow ID and input such as `{"orderId":"88213","amountUsd":"250.00","externalReference":"ticket-5488"}`. The Flow returns `recorded` or `alreadyRecorded`. The Flow wires `completed` for both operations and `uncertain` for the write because an unknown COMMIT needs read-back; unwired optional branches fail the Flow. The checked-in source passes strict FDG 2.0 visualization with a Python interpreter from this package environment:

```sh
dexcli visualize examples/record_refund/flow.py --python .venv/bin/python --schema-version 2.0 --json --out /tmp/mysql-python-refund
```

## Verification

From the package directory, run `uv sync --locked --group dev`, `uv run pytest tests`, and `uv run ruff check src tests examples`. `tests/integration/test_database.py` uses `MYSQL_CONNECTOR_TEST_HOST`, `PORT`, `DATABASE`, `USER`, `PASSWORD`, and `SSL_MODE` to exercise a real database in the CI service container; it skips when the variables are absent. The Flow's Dex Worker and Client test runs against `DEX_FLOW_SERVICE_ADDRESS` with a connector mock:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:23701 uv run pytest examples/record_refund/integration_test.py
```

Live provider authorization, managed MySQL services, and a real database through the Dex Worker remain unverified locally.
