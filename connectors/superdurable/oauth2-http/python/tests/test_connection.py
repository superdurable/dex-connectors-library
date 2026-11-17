# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Keep the released new_connection_with_operations call shape working."""

import httpx
import pytest
from dex_connectors.sdk import ConnectionRef

from dex_connectors.superdurable_oauth2_http import Config, new_connection_with_operations
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
