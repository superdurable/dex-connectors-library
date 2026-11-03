# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real MySQL service-container checks for the native Python driver."""

import os
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
        ssl_disabled=config.ssl_mode == "disabled",
    )
    cursor = connection.cursor()
    cursor.execute(
        f"CREATE TABLE {table} (id INT PRIMARY KEY, amount DECIMAL(12,2), "
        "dedup_key CHAR(36) UNIQUE) ENGINE=InnoDB"
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
        ssl_disabled=config.ssl_mode == "disabled",
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
async def test_rejected_credentials_and_local_input(database: tuple[Config, str, str]) -> None:
    config, _, table = database
    wrong = Client(config, _Credentials("wrong-password"))
    rejected = await wrong.query().invoke(_call("query"), QueryRowsInput(f"SELECT * FROM {table}"))
    assert rejected.branch == "providerRejected"
    assert rejected.failure is not None and rejected.failure.kind.value == "AUTHENTICATION"
    invalid = await wrong.execute().invoke(_call("execute"), ExecuteStatementInput("CREATE TABLE x (id INT)"))
    assert invalid.branch == "defect"
