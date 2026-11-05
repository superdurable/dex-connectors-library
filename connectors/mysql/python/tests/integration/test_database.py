# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real MySQL service-container checks for the native Python driver."""

import os
import struct
from dataclasses import replace
from uuid import uuid4

import mysql.connector
import pytest
from dex_connectors.sdk import Call, CallID, ConnectionRef, OperationRef, SecretString

from dex_connectors.mysql import Client, Config, Credentials, ExecuteStatementInput, QueryRowsInput


class _Credentials:
    def __init__(self, password: str) -> None:
        self.password = password

    async def resolve(self, call: Call) -> Credentials:
        return Credentials(SecretString(self.password))


@pytest.fixture
def database() -> tuple[Config, str, str]:
    host = os.getenv("MYSQL_CONNECTOR_TEST_HOST")
    if not host:
        pytest.skip("MYSQL_CONNECTOR_TEST_HOST is required for the CI service-container test")
    password = os.environ["MYSQL_CONNECTOR_TEST_PASSWORD"]
    config = Config(
        host=host,
        database=os.environ["MYSQL_CONNECTOR_TEST_DATABASE"],
        user=os.environ["MYSQL_CONNECTOR_TEST_USER"],
        port=int(os.getenv("MYSQL_CONNECTOR_TEST_PORT", "3306")),
        ssl_mode=os.getenv("MYSQL_CONNECTOR_TEST_SSL_MODE", "required"),
    )
    table = "connector_python_" + uuid4().hex[:20]
    connection = mysql.connector.connect(
        host=config.host,
        port=config.port,
        database=config.database,
        user=config.user,
        password=password,
        ssl_disabled=False,
    )
    cursor = connection.cursor()
    cursor.execute(
        f"CREATE TABLE {table} (id INT PRIMARY KEY, amount DECIMAL(12,2), "
        "dedup_key CHAR(36) UNIQUE, observed_year YEAR, ratio FLOAT, "
        "binary_label VARCHAR(16) COLLATE utf8mb4_bin, binary_note TEXT COLLATE utf8mb4_bin, "
        "recorded_time TIME(6), short_time TIME(2), exact_amount DECIMAL(20,8), "
        "zero_moment DATETIME, payload JSON) ENGINE=InnoDB"
    )
    connection.commit()
    cursor.close()
    connection.close()
    yield config, password, table
    connection = mysql.connector.connect(
        host=config.host,
        port=config.port,
        database=config.database,
        user=config.user,
        password=password,
        ssl_disabled=False,
    )
    cursor = connection.cursor()
    cursor.execute(f"DROP TABLE IF EXISTS {table}")
    connection.commit()
    cursor.close()
    connection.close()


def _call(operation: str) -> Call:
    identifier = CallID(str(uuid4()))
    return Call(
        identifier,
        ConnectionRef("mysql", "live"),
        OperationRef("mysql", operation),
        idempotency_key=identifier,
    )


@pytest.mark.asyncio
async def test_query_and_idempotent_execute_against_real_database(database: tuple[Config, str, str]) -> None:
    config, password, table = database
    client = Client(config, _Credentials(password))
    query = client.query()
    execute = client.execute()
    call = _call("execute")
    statement = (
        f"INSERT INTO {table} (id, amount, dedup_key) VALUES (?, ?, ?) "
        "ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)"
    )
    input = ExecuteStatementInput(statement, (1, "12.50"), 3, 1)
    inserted = await execute.invoke(call, input)
    assert inserted.kind == "branch" and inserted.branch == "completed"
    assert inserted.value is not None and inserted.value.rows_affected == 1
    replay = await execute.invoke(call, input)
    assert replay.kind == "branch" and replay.branch == "completed"
    assert replay.value is not None and replay.value.rows_affected == 0
    assert replay.value.last_insert_id == "1"
    read = await query.invoke(
        _call("query"), QueryRowsInput(f"SELECT id, amount, dedup_key FROM {table} WHERE id = ?", (1,))
    )
    assert read.kind == "branch" and read.branch == "completed"
    assert read.value is not None and len(read.value.rows) == 1
    assert read.value.rows[0]["amount"] == "12.50"
    assert read.value.rows[0]["dedup_key"] == str(call.idempotency_key)
    rejected = await query.invoke(_call("query"), QueryRowsInput("SELECT * FROM missing_python_table"))
    assert rejected.branch == "providerRejected"
    assert rejected.failure is not None and rejected.failure.kind.value == "NOT_FOUND"
    limited = Client(
        Config(
            host=config.host,
            database=config.database,
            user=config.user,
            port=config.port,
            ssl_mode=config.ssl_mode,
            max_rows=1,
        ),
        _Credentials(password),
    )
    await execute.invoke(
        _call("execute"),
        ExecuteStatementInput(
            f"INSERT INTO {table} (id, amount, dedup_key) VALUES (?, ?, ?)", (2, "2.00", str(uuid4()))
        ),
    )
    truncated = await limited.query().invoke(_call("query"), QueryRowsInput(f"SELECT id FROM {table}"))
    assert truncated.branch == "truncated"
    assert truncated.value is not None and len(truncated.value.rows) == 1


@pytest.mark.asyncio
async def test_driver_values_outcome_limits_and_read_only_isolation(
    database: tuple[Config, str, str],
) -> None:
    config, password, table = database
    client = Client(config, _Credentials(password))
    execute, query = client.execute(), client.query()
    for identifier in (1, 2):
        inserted = await execute.invoke(
            _call("execute"),
            ExecuteStatementInput(f"INSERT INTO {table} (id, amount) VALUES (?, ?)", (identifier, "1.00")),
        )
        assert inserted.branch == "completed" and inserted.value is not None
        assert inserted.value.rows_affected == 1
    limited = await execute.invoke(
        _call("execute"),
        ExecuteStatementInput(f"UPDATE {table} SET amount = '2.00'", max_rows_affected=1),
    )
    assert limited.branch == "limitExceeded"
    unchanged = await query.invoke(_call("query"), QueryRowsInput(f"SELECT amount FROM {table} WHERE id = 2"))
    assert unchanged.value is not None and unchanged.value.rows[0]["amount"] == "1.00"
    updated = await execute.invoke(
        _call("execute"),
        ExecuteStatementInput(
            f"UPDATE {table} SET observed_year = ?, ratio = ?, binary_label = ?, "
            "binary_note = ?, recorded_time = ?, payload = ? WHERE id = ?",
            (2024, 0.1, "Abc", "Abc", "-12:34:56.123457", '{"ok":true,"count":2}', 1),
        ),
    )
    assert updated.branch == "completed" and updated.value is not None
    assert updated.value.rows_affected == 1
    values = await query.invoke(
        _call("query"),
        QueryRowsInput(
            "SELECT observed_year, ratio, binary_label, binary_note, recorded_time, payload "
            f"FROM {table} WHERE id = 1"
        ),
    )
    assert values.branch == "completed" and values.value is not None
    row = values.value.rows[0]
    assert row == {
        "observed_year": 2024,
        "ratio": 0.1,
        "binary_label": "Abc",
        "binary_note": "Abc",
        "recorded_time": "-12:34:56.123457",
        "payload": {"ok": True, "count": 2},
    }
    assert {column.name: column.type_name for column in values.value.columns}["binary_label"] == "VARCHAR"
    missing_read = await query.invoke(_call("query"), QueryRowsInput("SELECT ?", ()))
    assert missing_read.branch == "defect" and missing_read.failure is not None
    assert missing_read.failure.kind.value == "VALIDATION"
    missing_write = await execute.invoke(
        _call("execute"), ExecuteStatementInput(f"INSERT INTO {table} (id) VALUES (?)", ())
    )
    assert missing_write.branch == "defect" and missing_write.failure is not None
    assert missing_write.failure.kind.value == "VALIDATION"
    count = await query.invoke(_call("query"), QueryRowsInput(f"SELECT COUNT(*) AS count FROM {table}"))
    assert count.value is not None and count.value.rows[0]["count"] == "2"
    comment = await query.invoke(
        _call("query"), QueryRowsInput(f"SELECT id FROM {table} WHERE id = ? -- don't", (1,))
    )
    assert comment.branch == "completed" and comment.value is not None
    assert comment.value.rows[0]["id"] == 1
    literal = await query.invoke(
        _call("query"), QueryRowsInput("SELECT '100%s off, it\\'s' AS literal, ? AS bound", (7,))
    )
    assert literal.branch == "completed" and literal.value is not None
    assert literal.value.rows[0]["literal"] == "100%s off, it's"
    decimals = await execute.invoke(
        _call("execute"),
        ExecuteStatementInput(
            f"UPDATE {table} SET exact_amount = ?, short_time = ? WHERE id = ?",
            ("0.00000001", "-12:34:56.50", 1),
        ),
    )
    assert decimals.branch == "completed"
    precise = await query.invoke(
        _call("query"), QueryRowsInput(f"SELECT exact_amount, short_time FROM {table} WHERE id = 1")
    )
    assert precise.branch == "completed" and precise.value is not None
    assert precise.value.rows[0] == {"exact_amount": "0.00000001", "short_time": "-12:34:56.50"}
    zero_decimal = await execute.invoke(
        _call("execute"), ExecuteStatementInput(f"UPDATE {table} SET exact_amount = ? WHERE id = ?", ("0", 2))
    )
    assert zero_decimal.branch == "completed"
    zero = await query.invoke(
        _call("query"), QueryRowsInput(f"SELECT exact_amount FROM {table} WHERE id = 2")
    )
    assert zero.value is not None and zero.value.rows[0]["exact_amount"] == "0.00000000"
    connection = mysql.connector.connect(
        host=config.host,
        port=config.port,
        database=config.database,
        user=config.user,
        password=password,
        ssl_disabled=False,
    )
    cursor = connection.cursor()
    cursor.execute("SET SESSION sql_mode = ''")
    cursor.execute(f"UPDATE {table} SET zero_moment = '0000-00-00 00:00:00' WHERE id = 1")
    connection.commit()
    cursor.close()
    connection.close()
    zero_date = await query.invoke(
        _call("query"), QueryRowsInput(f"SELECT zero_moment FROM {table} WHERE id = 1")
    )
    assert zero_date.branch == "completed" and zero_date.value is not None
    assert zero_date.value.rows[0]["zero_moment"] == "0000-00-00 00:00:00"
    edge_float = await execute.invoke(
        _call("execute"), ExecuteStatementInput(f"UPDATE {table} SET ratio = ? WHERE id = ?", (2**-12, 1))
    )
    assert edge_float.branch == "completed"
    float_row = await query.invoke(_call("query"), QueryRowsInput(f"SELECT ratio FROM {table} WHERE id = 1"))
    assert float_row.value is not None and float_row.value.rows[0]["ratio"] == 0.00024414063
    upper_float = struct.unpack("<f", struct.pack("<I", 0x7F7FFD77))[0]
    upper_update = await execute.invoke(
        _call("execute"),
        ExecuteStatementInput(f"UPDATE {table} SET ratio = ? WHERE id = ?", (upper_float, 1)),
    )
    assert upper_update.branch == "completed"
    upper_row = await query.invoke(_call("query"), QueryRowsInput(f"SELECT ratio FROM {table} WHERE id = 1"))
    assert upper_row.value is not None and upper_row.value.rows[0]["ratio"] == 3.402692e38
    negative_millisecond = await query.invoke(
        _call("query"), QueryRowsInput("SELECT CAST('-00:00:00.001' AS TIME(3)) AS negative_time")
    )
    assert negative_millisecond.value is not None
    assert negative_millisecond.value.rows[0]["negative_time"] == "-00:00:00.001"
    long_time = await query.invoke(
        _call("query"), QueryRowsInput("SELECT CAST('599:13:09.742888' AS TIME(6)) AS elapsed_time")
    )
    assert long_time.value is not None and long_time.value.rows[0]["elapsed_time"] == "599:13:09.742888"
    changed_json = await execute.invoke(
        _call("execute"),
        ExecuteStatementInput(
            f"UPDATE {table} SET payload = ? WHERE id = 1",
            ('{"amount": 1.10, "large": 12345678901234567890.123456789}',),
        ),
    )
    assert changed_json.branch == "completed"
    lossy = await query.invoke(_call("query"), QueryRowsInput(f"SELECT payload FROM {table} WHERE id = 1"))
    assert lossy.branch == "invalidResponse" and lossy.failure is not None
    assert "payload" in lossy.failure.message
    parenthesized = await query.invoke(_call("query"), QueryRowsInput("(SELECT 1) UNION (SELECT 2)"))
    assert parenthesized.branch == "completed" and parenthesized.value is not None
    assert len(parenthesized.value.rows) == 2
    commented = await query.invoke(_call("query"), QueryRowsInput("--\nSELECT 1 AS answer"))
    assert commented.branch == "completed"
    multi_statement = await query.invoke(
        _call("query"), QueryRowsInput(f"SELECT 1; UPDATE {table} SET amount = '9.00' WHERE id = 2")
    )
    assert multi_statement.branch != "completed"
    still_unchanged = await query.invoke(
        _call("query"), QueryRowsInput(f"SELECT amount FROM {table} WHERE id = 2")
    )
    assert still_unchanged.value is not None and still_unchanged.value.rows[0]["amount"] == "1.00"


@pytest.mark.asyncio
async def test_tls_and_dns_classification(
    database: tuple[Config, str, str], monkeypatch: pytest.MonkeyPatch
) -> None:
    config, password, table = database
    required = Client(replace(config, ssl_mode="required"), _Credentials(password))
    success = await required.query().invoke(_call("query"), QueryRowsInput(f"SELECT id FROM {table}"))
    assert success.branch == "completed"
    ca_file = os.environ["MYSQL_TLS_CA_FILE"]
    monkeypatch.setenv("MYSQL_SSL_CA", ca_file)
    verified = Client(replace(config, ssl_mode="verify-ca"), _Credentials(password))
    trusted = await verified.query().invoke(_call("query"), QueryRowsInput("SELECT 1"))
    assert trusted.branch == "completed"
    missing = Client(replace(config, host="nonexistent.invalid", ssl_mode="required"), _Credentials(password))
    unresolved = await missing.query().invoke(_call("query"), QueryRowsInput("SELECT 1"))
    assert unresolved.branch == "providerRejected" and unresolved.failure is not None
    assert unresolved.failure.kind.value == "NOT_FOUND"


@pytest.mark.asyncio
async def test_rejected_credentials_and_local_input(database: tuple[Config, str, str]) -> None:
    config, _, table = database
    wrong = Client(config, _Credentials("wrong-password"))
    rejected = await wrong.query().invoke(_call("query"), QueryRowsInput(f"SELECT * FROM {table}"))
    assert rejected.branch == "providerRejected"
    assert rejected.failure is not None and rejected.failure.kind.value == "AUTHENTICATION"
    invalid = await wrong.execute().invoke(_call("execute"), ExecuteStatementInput("CREATE TABLE x (id INT)"))
    assert invalid.branch == "defect"
