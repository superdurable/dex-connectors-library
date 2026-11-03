# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real SQL Server service-container coverage for both native operations."""

import asyncio
import os
from dataclasses import replace
from uuid import uuid4

import pytds
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    FailureKind,
    IdempotencyKey,
    OperationRef,
    SecretString,
)

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


def administration_connection(config: Config, *, autocommit: bool) -> pytds.Connection:
    ca_file = (
        os.environ["SQLSERVER_TLS_CA_FILE"] if config.encrypt == "mandatory" else None
    )
    return pytds.connect(
        server=config.host,
        port=config.port,
        database=config.database,
        user=config.user,
        password=os.environ["SQLSERVER_CONNECTOR_TEST_PASSWORD"],
        cafile=ca_file,
        validate_host=False,
        autocommit=autocommit,
    )


def test_administration_tls_connection_verifies_ca_without_legacy_host_validator(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("SQLSERVER_TLS_CA_FILE", "/test/ca.pem")
    monkeypatch.setenv("SQLSERVER_CONNECTOR_TEST_PASSWORD", "test-only-password")
    observed: dict[str, object] = {}

    def connect(**settings: object) -> object:
        observed.update(settings)
        return object()

    monkeypatch.setattr(pytds, "connect", connect)
    config = Config("127.0.0.1", "master", "sa", encrypt="mandatory")
    administration_connection(config, autocommit=True)
    assert observed["cafile"] == "/test/ca.pem"
    assert observed["validate_host"] is False


def test_query_type_mapping_and_row_limit() -> None:
    client = Client(live_config(), CredentialsSource())
    completed = asyncio.run(
        client.query().invoke(
            call("query"), QueryRowsInput("SELECT CAST(@p1 AS bigint) AS exact", ("9007199254740993",))
        )
    )
    assert completed.branch == "completed", completed.failure
    assert completed.value.rows == ({"exact": "9007199254740993"},)
    exact_time = asyncio.run(
        client.query().invoke(
            call("query"),
            QueryRowsInput("SELECT CAST('2026-01-01T09:00:00.1234567+02:00' AS datetimeoffset(7)) AS moment"),
        )
    )
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
    numeric = asyncio.run(
        client.query().invoke(
            call("query"),
            QueryRowsInput(
                "SELECT CAST(1.50 AS decimal(12,2)) AS decimal_value, "
                "CAST(12.5000 AS money) AS money_value, "
                "CAST(-1.2345 AS smallmoney) AS small_money, "
                "CAST(0.00 AS decimal(12,2)) AS zero_value, "
                "CAST(0.1 AS real) AS real_value, "
                "CAST('6f1c2a9e-0000-4000-8000-000000000001' AS uniqueidentifier) AS guid_value, "
                "CAST('2026-01-01T09:00:00.123' AS datetime) AS date_time, "
                "CAST('2026-01-01' AS date) AS date_value, "
                "CAST('12:34:56.123456' AS time(6)) AS time_value"
            ),
        )
    )
    assert numeric.branch == "completed", numeric.failure
    row = numeric.value.rows[0]
    assert row["decimal_value"] == "1.50"
    assert row["money_value"] == "12.5000"
    assert row["small_money"] == "-1.2345"
    assert row["zero_value"] == "0.00"
    assert row["real_value"] == 0.1
    assert row["guid_value"] == "6f1c2a9e-0000-4000-8000-000000000001"
    assert row["date_value"] == "2026-01-01"
    assert row["time_value"] == "12:34:56.123456"
    assert row["date_time"].startswith("2026-01-01T09:00:00.123")
    assert numeric.receipt.metadata["processId"]
    assert numeric.receipt.metadata["serverVersion"]


def test_execute_commits_and_limits_rollback() -> None:
    table_name = "dex_py_" + uuid4().hex[:20]
    config = live_config()
    administration = administration_connection(config, autocommit=True)
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
        cursor.execute(f"SELECT id, call_key FROM dbo.{table_name}")
        stored_id, stored_key = cursor.fetchone()
        assert stored_id == 1
        duplicate = asyncio.run(
            client.execute().invoke(
                call("execute"),
                ExecuteStatementInput(
                    f"INSERT INTO dbo.{table_name} (id, call_key) VALUES (@p1, @p2)",
                    (3, stored_key),
                ),
            )
        )
        assert duplicate.branch == "providerRejected", duplicate.failure
    finally:
        cursor = administration.cursor()
        cursor.execute(f"IF OBJECT_ID('dbo.{table_name}', 'U') IS NOT NULL DROP TABLE dbo.{table_name}")
        administration.close()


def test_trigger_done_counts_are_summed_before_commit() -> None:
    table_name = "dex_py_" + uuid4().hex[:20]
    audit_name = "dex_py_" + uuid4().hex[:20]
    config = live_config()
    administration = administration_connection(config, autocommit=True)
    try:
        cursor = administration.cursor()
        cursor.execute(f"CREATE TABLE dbo.{table_name} (id int NOT NULL)")
        cursor.execute(f"CREATE TABLE dbo.{audit_name} (id int NOT NULL)")
        cursor.execute(
            f"CREATE TRIGGER dex_trg_{table_name} ON dbo.{table_name} AFTER INSERT AS "
            f"INSERT INTO dbo.{audit_name} (id) SELECT id FROM inserted"
        )
        client = Client(config, CredentialsSource())
        limited = asyncio.run(
            client.execute().invoke(
                call("execute"),
                ExecuteStatementInput(
                    f"INSERT INTO dbo.{table_name} (id) VALUES (@p1)",
                    (1,),
                    max_rows_affected=1,
                ),
            )
        )
        assert limited.branch == "limitExceeded", limited.failure
        cursor.execute(f"SELECT COUNT(*) FROM dbo.{table_name}")
        assert cursor.fetchone()[0] == 0
        completed = asyncio.run(
            client.execute().invoke(
                call("execute"),
                ExecuteStatementInput(
                    f"INSERT INTO dbo.{table_name} (id) VALUES (@p1)",
                    (2,),
                    max_rows_affected=2,
                ),
            )
        )
        assert completed.branch == "completed", completed.failure
        assert completed.value.rows_affected == 2
    finally:
        cursor = administration.cursor()
        cursor.execute(f"IF OBJECT_ID('dbo.{table_name}', 'U') IS NOT NULL DROP TABLE dbo.{table_name}")
        cursor.execute(f"IF OBJECT_ID('dbo.{audit_name}', 'U') IS NOT NULL DROP TABLE dbo.{audit_name}")
        administration.close()


def test_statement_timeout_under_a_held_lock() -> None:
    table_name = "dex_py_" + uuid4().hex[:20]
    config = live_config()
    administration = administration_connection(config, autocommit=False)
    try:
        cursor = administration.cursor()
        cursor.execute(f"CREATE TABLE dbo.{table_name} (id int NOT NULL)")
        administration.commit()
        cursor.execute(f"INSERT INTO dbo.{table_name} (id) VALUES (1)")
        administration.commit()
        cursor.execute(f"UPDATE dbo.{table_name} SET id = 2")
        bounded = Client(replace(config, statement_timeout_seconds=0.05), CredentialsSource())
        outcome = asyncio.run(
            bounded.query().invoke(
                call("query"), QueryRowsInput(f"SELECT id FROM dbo.{table_name} WITH (UPDLOCK)")
            )
        )
        assert outcome.branch == "providerRejected" or outcome.kind == "retry", outcome.failure
        assert outcome.failure is not None
        assert outcome.failure.kind in (FailureKind.AVAILABILITY, FailureKind.CONFLICT)
    finally:
        administration.rollback()
        cursor = administration.cursor()
        cursor.execute(f"DROP TABLE IF EXISTS dbo.{table_name}")
        administration.commit()
        administration.close()


def test_mandatory_tls_against_service_certificate(monkeypatch: pytest.MonkeyPatch) -> None:
    encryption = os.environ["SQLSERVER_CONNECTOR_TEST_ENCRYPT"]
    if encryption == "disable":
        assert "SQLSERVER_TLS_HOST" not in os.environ
        assert "SQLSERVER_TLS_CA_FILE" not in os.environ
        return
    assert encryption == "mandatory"
    tls_host = os.environ["SQLSERVER_TLS_HOST"]
    tls_ca = os.environ["SQLSERVER_TLS_CA_FILE"]
    assert tls_host and os.path.isfile(tls_ca)
    monkeypatch.setenv("SQLSERVER_SSL_CA", tls_ca)
    config = replace(live_config(), host=tls_host, encrypt="mandatory")
    client = Client(config, CredentialsSource())
    completed = asyncio.run(client.query().invoke(call("query"), QueryRowsInput("SELECT 1 AS n")))
    assert completed.branch == "completed", completed.failure
    wrong_host = Client(replace(config, host="127.0.0.2"), CredentialsSource())
    rejected = asyncio.run(wrong_host.query().invoke(call("query"), QueryRowsInput("SELECT 1 AS n")))
    assert rejected.branch == "providerRejected", rejected.failure
    assert rejected.failure.kind == FailureKind.AUTHENTICATION
