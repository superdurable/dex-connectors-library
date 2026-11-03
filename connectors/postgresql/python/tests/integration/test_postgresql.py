# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real PostgreSQL scenarios selected by the CI database service job."""

import os
from uuid import uuid4

import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    FailureKind,
    IdempotencyKey,
    OperationRef,
    SecretString,
    StaticCredentialProvider,
)

from dex_connectors.postgresql import Client, Config, Credentials, ExecuteStatementInput, QueryRowsInput

REFERENCE = ConnectionRef("postgresql", "live")


def _call(operation: str, identifier: str | None = None) -> Call:
    call_id = CallID(identifier or str(uuid4()))
    return Call(call_id, REFERENCE, OperationRef("postgresql", operation), IdempotencyKey(call_id))


def _client(max_rows: int = 1000) -> Client:
    host = os.environ.get("POSTGRESQL_CONNECTOR_TEST_HOST")
    if not host:
        pytest.skip("real PostgreSQL service is configured in CI only")
    config = Config(
        host=host,
        port=int(os.environ.get("POSTGRESQL_CONNECTOR_TEST_PORT", "5432")),
        database=os.environ["POSTGRESQL_CONNECTOR_TEST_DATABASE"],
        user=os.environ["POSTGRESQL_CONNECTOR_TEST_USER"],
        ssl_mode=os.environ.get("POSTGRESQL_CONNECTOR_TEST_SSL_MODE", "require"),
        max_rows=max_rows,
    )
    credentials = StaticCredentialProvider(
        {REFERENCE: Credentials(SecretString(os.environ["POSTGRESQL_CONNECTOR_TEST_PASSWORD"]))}
    )
    return Client(config, credentials)


@pytest.mark.asyncio
async def test_real_database_query_write_limits_and_idempotency() -> None:
    """Mirror the Go live tests on a real transaction and native wire protocol."""
    client = _client()
    table = "connector_python_live_" + uuid4().hex
    create = await client.execute().invoke(
        _call("execute"),
        ExecuteStatementInput(
            f"CREATE TABLE {table} (id bigserial PRIMARY KEY, order_id text UNIQUE, "
            "amount numeric(12,2), key uuid UNIQUE)"
        ),
    )
    assert create.branch == "completed"
    try:
        query_client = _client(max_rows=1)
        read_only = await client.query().invoke(
            _call("query"),
            QueryRowsInput(f"INSERT INTO {table} (order_id) VALUES ('forbidden') RETURNING id"),
        )
        assert read_only.branch == "providerRejected"
        assert read_only.failure.kind == FailureKind.AUTHORIZATION
        assert read_only.receipt.metadata["sqlState"] == "25006"

        call = _call("execute")
        insert = ExecuteStatementInput(
            f"INSERT INTO {table} (order_id, amount, key) VALUES ($1, $2::numeric, $3) "
            "ON CONFLICT (key) DO NOTHING RETURNING id, order_id, amount, key",
            ("order-1", "250.00"),
            3,
            1,
        )
        first = await client.execute().invoke(call, insert)
        assert first.branch == "completed" and first.value.rows_affected == 1
        assert first.value.rows[0]["amount"] == "250.00"
        assert first.value.rows[0]["key"] == str(call.id)
        assert isinstance(first.value.rows[0]["id"], str)
        replay = await client.execute().invoke(call, insert)
        assert replay.branch == "completed" and replay.value.rows_affected == 0

        limited = await client.execute().invoke(
            _call("execute"),
            ExecuteStatementInput(f"UPDATE {table} SET amount = 1 WHERE order_id = $1", ("order-1",), 0, 0),
        )
        assert limited.branch == "limitExceeded"
        amount = await client.query().invoke(
            _call("query"), QueryRowsInput(f"SELECT amount FROM {table} WHERE order_id = $1", ("order-1",))
        )
        assert amount.branch == "completed" and amount.value.rows[0]["amount"] == "250.00"

        truncated = await query_client.query().invoke(
            _call("query"), QueryRowsInput("SELECT n FROM generate_series(1, 3) AS n ORDER BY n")
        )
        assert truncated.branch == "truncated" and truncated.value.rows == ({"n": 1},)

        syntax = await client.query().invoke(_call("query"), QueryRowsInput("SELEC 1"))
        assert syntax.branch == "providerRejected" and syntax.receipt.metadata["sqlState"] == "42601"
    finally:
        dropped = await client.execute().invoke(
            _call("execute"), ExecuteStatementInput(f"DROP TABLE IF EXISTS {table}")
        )
        assert dropped.branch == "completed"
