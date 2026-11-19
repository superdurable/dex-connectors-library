# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Real PostgreSQL scenarios selected by the CI database service job."""

import os
from dataclasses import replace
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


def _client(max_rows: int = 1000, password: str | None = None) -> Client:
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
        {REFERENCE: Credentials(SecretString(password or os.environ["POSTGRESQL_CONNECTOR_TEST_PASSWORD"]))}
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
    assert create.value.command == "CREATE TABLE"
    try:
        query_client = _client(max_rows=1)
        wrong_password = (
            await _client(password="deliberately-wrong-password")
            .query()
            .invoke(_call("query"), QueryRowsInput("SELECT 1"))
        )
        assert wrong_password.branch == "providerRejected"
        assert wrong_password.failure.kind == FailureKind.AUTHENTICATION
        missing_database = (
            await Client(
                replace(client.config, database="connector_missing_" + uuid4().hex), client.credentials
            )
            .query()
            .invoke(_call("query"), QueryRowsInput("SELECT 1"))
        )
        assert missing_database.branch == "providerRejected"
        assert missing_database.failure.kind == FailureKind.NOT_FOUND

        escaped_table = "connector_python_escaped_" + uuid4().hex
        multi_query = await client.query().invoke(
            _call("query"), QueryRowsInput(f"SELECT 1; COMMIT; CREATE TABLE {escaped_table} (id int)")
        )
        assert multi_query.branch == "providerRejected"
        assert multi_query.receipt.metadata["sqlState"] == "42601"
        absence = await client.query().invoke(
            _call("query"), QueryRowsInput(f"SELECT to_regclass('{escaped_table}') AS name")
        )
        assert absence.branch == "completed" and absence.value.rows == ({"name": None},)

        multi_execute = await client.execute().invoke(
            _call("execute"),
            ExecuteStatementInput(
                f"INSERT INTO {table} (order_id) VALUES ('escaped'); COMMIT; "
                f"INSERT INTO {table} (order_id) VALUES ('escaped-again')"
            ),
        )
        assert multi_execute.branch == "providerRejected"
        assert multi_execute.receipt.metadata["sqlState"] == "42601"
        no_escaped_rows = await client.query().invoke(
            _call("query"), QueryRowsInput(f"SELECT count(*) AS n FROM {table}")
        )
        assert no_escaped_rows.value.rows == ({"n": "0"},)

        comment_query = await client.query().invoke(
            _call("query"), QueryRowsInput("-- only a comment")
        )
        assert comment_query.branch == "completed"
        assert comment_query.value.rows == ()
        comment_execute = await client.execute().invoke(
            _call("execute"), ExecuteStatementInput("-- only a comment")
        )
        assert comment_execute.branch == "completed"
        assert comment_execute.value.command == ""
        assert comment_execute.value.rows_affected == 0

        short_parameters = await client.query().invoke(
            _call("query"), QueryRowsInput("SELECT $1::text, $2::text", ("only-one",))
        )
        assert short_parameters.branch == "defect"
        assert short_parameters.failure.kind == FailureKind.VALIDATION
        extra_parameters = await client.execute().invoke(
            _call("execute"), ExecuteStatementInput(f"UPDATE {table} SET order_id = $1", ("a", "b"))
        )
        assert extra_parameters.branch == "defect"
        assert extra_parameters.failure.kind == FailureKind.VALIDATION
        extra_query_parameters = await client.query().invoke(
            _call("query"), QueryRowsInput("SELECT $1::text AS value", ("a", "b"))
        )
        assert extra_query_parameters.branch == "defect"
        assert extra_query_parameters.failure.kind == FailureKind.VALIDATION
        unused_key_placeholder = await client.execute().invoke(
            _call("execute"),
            ExecuteStatementInput(f"UPDATE {table} SET order_id = $1", ("a",), 2),
        )
        assert unused_key_placeholder.branch == "defect"
        assert unused_key_placeholder.failure.kind == FailureKind.VALIDATION
        duplicate_columns = await client.query().invoke(
            _call("query"), QueryRowsInput("SELECT 1 AS id, 2 AS id")
        )
        assert duplicate_columns.branch == "defect"
        assert duplicate_columns.failure.kind == FailureKind.VALIDATION

        timestamps = await client.query().invoke(
            _call("query"),
            QueryRowsInput(
                "SELECT '2024-01-01 00:00:00.5+00'::timestamptz AS zoned, "
                "'infinity'::timestamptz AS forever, "
                "'0044-03-15 00:00:00 BC'::timestamp AS ancient"
            ),
        )
        assert timestamps.branch == "completed"
        assert timestamps.value.rows == (
            {
                "zoned": "2024-01-01T00:00:00.5Z",
                "forever": "infinity",
                "ancient": "0044-03-15 00:00:00 BC",
            },
        )

        plain_json = await client.query().invoke(
            _call("query"), QueryRowsInput('SELECT \'{"a":true,"b":[null,42]}\'::json AS document')
        )
        assert plain_json.branch == "completed"
        assert plain_json.value.rows == ({"document": {"a": True, "b": [None, 42]}},)
        lossy_json = await client.query().invoke(
            _call("query"), QueryRowsInput("SELECT '{\"a\":1.10}'::json AS document")
        )
        assert lossy_json.branch == "invalidResponse"
        assert 'column "document"' in lossy_json.failure.message
        duplicate_json_keys = await client.query().invoke(
            _call("query"), QueryRowsInput('SELECT \'{"a":1,"a":2}\'::json AS document')
        )
        assert duplicate_json_keys.branch == "invalidResponse"
        assert 'column "document"' in duplicate_json_keys.failure.message
        lossy_jsonb = await client.query().invoke(
            _call("query"),
            QueryRowsInput("SELECT '{\"amount\":12345678901234567890.123456789}'::jsonb AS document"),
        )
        assert lossy_jsonb.branch == "invalidResponse"
        assert 'column "document"' in lossy_jsonb.failure.message

        no_ca_config = replace(client.config, ssl_mode="verify-ca")
        no_ca_client = Client(no_ca_config, client.credentials)
        original_root = os.environ.pop("PGSSLROOTCERT", None)
        try:
            no_ca = await no_ca_client.query().invoke(_call("query"), QueryRowsInput("SELECT 1"))
        finally:
            if original_root is not None:
                os.environ["PGSSLROOTCERT"] = original_root
        assert no_ca.branch == "providerRejected"
        assert no_ca.failure.kind == FailureKind.AUTHENTICATION
        verify_full = (
            await Client(replace(client.config, ssl_mode="verify-full"), client.credentials)
            .query()
            .invoke(_call("query"), QueryRowsInput("SELECT 1"))
        )
        assert verify_full.kind == "branch"
        assert verify_full.branch in ("completed", "providerRejected")
        if verify_full.branch == "providerRejected":
            assert verify_full.failure.kind in (FailureKind.AUTHENTICATION, FailureKind.PROVIDER_REJECTION)

        read_only = await client.query().invoke(
            _call("query"),
            QueryRowsInput(f"INSERT INTO {table} (order_id) VALUES ('forbidden') RETURNING id"),
        )
        assert read_only.branch == "providerRejected"
        assert read_only.failure.kind == FailureKind.AUTHORIZATION
        assert read_only.receipt.metadata["sqlState"] == "25006"

        connection_failure = await client.execute().invoke(
            _call("execute"), ExecuteStatementInput("DO $$ BEGIN RAISE SQLSTATE '08007'; END $$")
        )
        assert connection_failure.kind == "retry"
        assert connection_failure.failure.kind == FailureKind.TRANSPORT

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
        assert first.value.command == "INSERT"
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
        assert dropped.value.command == "DROP TABLE"
