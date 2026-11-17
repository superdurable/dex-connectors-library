# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Keep the released new_connection_with_operations call shape working."""

import pytest
from dex_connectors.sdk import Call, ConnectionRef, SecretString

from dex_connectors.microsoft_sql_server import Client, Config, Credentials, new_connection_with_operations
from dex_connectors.microsoft_sql_server._generated.definitions import CONNECTOR_ID


class _Credentials:
    async def resolve(self, call: Call) -> Credentials:
        return Credentials(SecretString("inert-password"))


def _client() -> Client:
    return Client(Config("db.example.com", "app", "dex_app"), _Credentials())


def test_released_positional_order_still_binds_query_and_execute() -> None:
    client = _client()
    reference = ConnectionRef(CONNECTOR_ID, "primary")
    connection = new_connection_with_operations(client.query(), client.execute(), reference)

    assert connection.reference.name == "primary"
    assert connection.execute() is not None
    with pytest.raises(ValueError, match="without the executeNonIdempotent operation"):
        connection.execute_non_idempotent()


def test_keyword_execute_non_idempotent_binds_the_new_operation() -> None:
    client = _client()
    execute_non_idempotent = client.execute_non_idempotent()
    connection = new_connection_with_operations(
        client.query(),
        client.execute(),
        ConnectionRef(CONNECTOR_ID, "primary"),
        execute_non_idempotent=execute_non_idempotent,
    )

    assert connection.execute_non_idempotent() is execute_non_idempotent
