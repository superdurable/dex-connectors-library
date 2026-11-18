# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Keep the released new_connection_with_operations call shape working."""

import pytest
from dex_connectors.sdk import Call, ConnectionRef, SecretString

from dex_connectors.mysql import (
    Client,
    Config,
    Connection,
    Credentials,
    new_connection,
    new_connection_with_operations,
)


class _Credentials:
    async def resolve(self, call: Call) -> Credentials:
        return Credentials(SecretString("inert-password"))


def _client() -> Client:
    return Client(Config("localhost", "app", "dex_app", ssl_mode="disabled"), _Credentials())


def test_released_positional_order_still_binds_query_and_execute() -> None:
    client = _client()
    reference = ConnectionRef("mysql", "primary")
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
        ConnectionRef("mysql", "primary"),
        execute_non_idempotent=execute_non_idempotent,
    )

    assert connection.execute_non_idempotent() is execute_non_idempotent


def test_released_positional_connection_constructor_still_binds_client() -> None:
    client = _client()
    connection = Connection(ConnectionRef("mysql", "primary"), client.query(), client.execute(), client)

    assert connection.execute() is not None
    with pytest.raises(ValueError, match="without the executeNonIdempotent operation"):
        connection.execute_non_idempotent()


def test_connection_constructor_accepts_keyword_execute_non_idempotent() -> None:
    client = _client()
    execute_non_idempotent = client.execute_non_idempotent()
    connection = Connection(
        ConnectionRef("mysql", "primary"),
        client.query(),
        client.execute(),
        client,
        execute_non_idempotent=execute_non_idempotent,
    )

    assert connection.execute_non_idempotent() is execute_non_idempotent


def test_new_connection_binds_every_client_operation() -> None:
    connection = new_connection(_client(), ConnectionRef("mysql", "primary"))

    assert connection.execute_non_idempotent().definition().operation.operation_id == "executeNonIdempotent"
