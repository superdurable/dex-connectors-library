# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real SQL Server service-container coverage for both native operations."""

import asyncio
import os
from uuid import uuid4

import pytds
from dex_connectors.sdk import Call, CallID, ConnectionRef, IdempotencyKey, OperationRef, SecretString

from dex_connectors.microsoft_sql_server import (
    Client,
    Config,
    Credentials,
    ExecuteStatementInput,
    QueryRowsInput,
)


class CredentialsSource:
    async def resolve(self, _call: Call) -> Credentials:
        return Credentials(SecretString(os.environ["SQLSERVER_CONNECTOR_TEST_PASSWORD"]))


def call(operation: str) -> Call:
    call_id = CallID(str(uuid4()))
    return Call(
        call_id,
        ConnectionRef("microsoft-sql-server", "live-test"),
        OperationRef("microsoft-sql-server", operation),
        IdempotencyKey(str(call_id)),
    )


def live_config(*, max_rows: int = 1000) -> Config:
    return Config(
        os.environ["SQLSERVER_CONNECTOR_TEST_HOST"],
        os.environ["SQLSERVER_CONNECTOR_TEST_DATABASE"],
        os.environ["SQLSERVER_CONNECTOR_TEST_USER"],
        int(os.environ["SQLSERVER_CONNECTOR_TEST_PORT"]),
        os.environ["SQLSERVER_CONNECTOR_TEST_ENCRYPT"],
        max_rows=max_rows,
    )


def test_query_type_mapping_and_row_limit() -> None:
    client = Client(live_config(), CredentialsSource())
    completed = asyncio.run(
        client.query().invoke(
            call("query"), QueryRowsInput("SELECT CAST(@p1 AS bigint) AS exact", ("9007199254740993",))
        )
    )
    assert completed.branch == "completed", completed.failure
    assert completed.value.rows == ({"exact": "9007199254740993"},)
    exact_time = asyncio.run(client.query().invoke(
        call("query"), QueryRowsInput(
            "SELECT CAST('2026-01-01T09:00:00.1234567+02:00' AS datetimeoffset(7)) AS moment"
        )
    ))
    assert exact_time.branch == "invalidResponse", exact_time.failure
    assert "moment" in exact_time.failure.message
    bounded = Client(live_config(max_rows=1), CredentialsSource())
    truncated = asyncio.run(
        bounded.query().invoke(
            call("query"), QueryRowsInput("SELECT CAST(1 AS int) AS n UNION ALL SELECT CAST(2 AS int) AS n")
        )
    )
    assert truncated.branch == "truncated", truncated.failure
    assert len(truncated.value.rows) == 1


def test_execute_commits_and_limits_rollback() -> None:
    table_name = "dex_py_" + uuid4().hex[:20]
    config = live_config()
    administration = pytds.connect(
        server=config.host,
        port=config.port,
        database=config.database,
        user=config.user,
        password=os.environ["SQLSERVER_CONNECTOR_TEST_PASSWORD"],
        autocommit=True,
    )
    try:
        cursor = administration.cursor()
        cursor.execute(
            f"CREATE TABLE dbo.{table_name} (id bigint NOT NULL, call_key nvarchar(36) NOT NULL UNIQUE)"
        )
        client = Client(config, CredentialsSource())
        inserted = asyncio.run(
            client.execute().invoke(
                call("execute"),
                ExecuteStatementInput(
                    f"INSERT INTO dbo.{table_name} (id, call_key) VALUES (@p1, @p2)",
                    (1,),
                    idempotency_key_placeholder=2,
                    max_rows_affected=1,
                ),
            )
        )
        assert inserted.branch == "completed", inserted.failure
        assert inserted.value.rows_affected == 1
        limited = asyncio.run(
            client.execute().invoke(
                call("execute"),
                ExecuteStatementInput(f"UPDATE dbo.{table_name} SET id = @p1", (2,), max_rows_affected=0),
            )
        )
        assert limited.branch == "limitExceeded", limited.failure
        cursor.execute(f"SELECT id FROM dbo.{table_name}")
        assert cursor.fetchone()[0] == 1
    finally:
        cursor = administration.cursor()
        cursor.execute(f"IF OBJECT_ID('dbo.{table_name}', 'U') IS NOT NULL DROP TABLE dbo.{table_name}")
        administration.close()
