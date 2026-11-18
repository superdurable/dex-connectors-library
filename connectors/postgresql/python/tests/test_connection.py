# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Keep the released Connection and new_connection_with_operations call shapes working."""

import pytest
from dex_connectors.sdk import Call, ConnectionRef, SecretString

from dex_connectors.postgresql import (
    Client,
    Config,
    Connection,
    Credentials,
    new_connection,
    new_connection_with_operations,
)
from dex_connectors.postgresql._generated.definitions import CONNECTOR_ID


class _Credentials:
    async def resolve(self, call: Call) -> Credentials:
        return Credentials(SecretString("inert-password"))


def _client() -> Client:
    return Client(Config("db.example.com", "app", "dex_app"), _Credentials())


def test_released_positional_connection_constructor_still_binds_query_and_execute() -> None:
    client = _client()
    connection = Connection(ConnectionRef(CONNECTOR_ID, "primary"), client.query(), client.execute())

    assert connection.execute() is not None
    with pytest.raises(ValueError, match="executeNonIdempotent"):
        connection.execute_non_idempotent()


def test_released_new_connection_with_operations_order_still_works() -> None:
    client = _client()
    connection = new_connection_with_operations(
        client.query(), client.execute(), ConnectionRef(CONNECTOR_ID, "primary")
    )

    assert connection.reference.name == "primary"


def test_new_connection_binds_every_client_operation() -> None:
    connection = new_connection(_client(), ConnectionRef(CONNECTOR_ID, "primary"))

    assert connection.execute_non_idempotent().definition().operation.operation_id == "executeNonIdempotent"
