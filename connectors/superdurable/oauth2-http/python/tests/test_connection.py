# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Keep the released new_connection_with_operations call shape working."""

import httpx
import pytest
from dex_connectors.sdk import ConnectionRef

from dex_connectors.superdurable_oauth2_http import (
    Config,
    Connection,
    PostNonIdempotentInput,
    new_connection,
    new_connection_with_operations,
    new_post_non_idempotent_step,
)
from dex_connectors.superdurable_oauth2_http._generated.definitions import CONNECTOR_ID
from dex_connectors.superdurable_oauth2_http.client import Client


def _client() -> Client:
    return Client(
        Config(
            authorization_endpoint="http://127.0.0.1/auth",
            token_endpoint="http://127.0.0.1/token",
            scopes=("profile",),
            api_base_url="http://127.0.0.1/v1",
        ),
        object(),  # type: ignore[arg-type]
        httpx.AsyncClient(),
    )


def test_released_positional_order_still_binds_send_request() -> None:
    client = _client()
    send_request = client.send_request()
    connection = new_connection_with_operations(send_request, ConnectionRef(CONNECTOR_ID, "primary"))

    assert connection.send_request() is send_request
    with pytest.raises(ValueError, match="without the postNonIdempotent operation"):
        connection.post_non_idempotent()


def test_keyword_post_non_idempotent_binds_the_new_operation() -> None:
    client = _client()
    post_non_idempotent = client.post_non_idempotent()
    connection = new_connection_with_operations(
        client.send_request(),
        ConnectionRef(CONNECTOR_ID, "primary"),
        post_non_idempotent=post_non_idempotent,
    )

    assert connection.post_non_idempotent() is post_non_idempotent


def test_released_positional_connection_constructor_still_binds_send_request() -> None:
    client = _client()
    send_request = client.send_request()
    connection = Connection(ConnectionRef(CONNECTOR_ID, "primary"), send_request, client)

    assert connection.send_request() is send_request
    with pytest.raises(ValueError, match="without the postNonIdempotent operation"):
        connection.post_non_idempotent()


def test_connection_constructor_accepts_keyword_post_non_idempotent() -> None:
    client = _client()
    post_non_idempotent = client.post_non_idempotent()
    connection = Connection(
        ConnectionRef(CONNECTOR_ID, "primary"),
        client.send_request(),
        client,
        post_non_idempotent=post_non_idempotent,
    )

    assert connection.post_non_idempotent() is post_non_idempotent


def test_post_non_idempotent_step_on_an_unbound_connection_fails_when_built() -> None:
    reference = ConnectionRef(CONNECTOR_ID, "primary")
    connection = new_connection_with_operations(_client().send_request(), reference)

    with pytest.raises(ValueError, match="without the postNonIdempotent operation"):
        new_post_non_idempotent_step(
            step_type="PostOrder",
            step_input_type=dict,
            annotations={"groupId": "orders", "groupLabel": "Orders", "explanation": "Post one order."},
            connection=connection,
            connection_name="primary",
            map_to_operation_input=lambda _input: PostNonIdempotentInput(path="/orders"),
            completed=None,  # type: ignore[arg-type]
        )


def test_new_connection_binds_both_client_operations() -> None:
    client = _client()
    connection = new_connection(client, ConnectionRef(CONNECTOR_ID, "primary"))

    assert isinstance(connection, Connection)
    assert connection.post_non_idempotent().definition().operation.operation_id == "postNonIdempotent"
