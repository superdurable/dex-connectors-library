# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check Notion credential and local transport classifications."""

import socket

import pytest
from dex_connectors.sdk import Call, CallID, ConnectionRef, FailureKind, OperationRef
from dex_connectors.sdk.projectconfig.broker import CredentialBrokerUnavailableError

from dex_connectors.notion import Client, Config, Credentials
from dex_connectors.notion.client import classify_read, classify_write


class FailingCredentials:
    """Resolve one explicit test failure without exposing a token."""

    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: Call) -> Credentials:
        """Raise the injected credential failure."""
        raise self.error


def _call() -> Call:
    return Call(
        CallID("5ecde15b-017b-5681-83c4-dcbd11fe5e99"),
        ConnectionRef("notion", "notion-test"),
        OperationRef("notion", "search"),
    )


@pytest.mark.asyncio
async def test_credential_failures_distinguish_broker_outage_from_terminal_rejection() -> None:
    """Only a known transient broker outage authorizes a retry."""
    transient = Client(Config(), FailingCredentials(CredentialBrokerUnavailableError("broker down")))
    terminal = Client(Config(), FailingCredentials(ValueError("invalid token")))
    unrelated_network_error = Client(Config(), FailingCredentials(ConnectionError("unknown source")))
    try:
        _, transient_failure, is_transient = await transient.resolve_credentials(_call(), "search")
        assert is_transient and transient_failure is not None
        assert transient_failure.kind == FailureKind.AUTHENTICATION
        _, terminal_failure, is_transient = await terminal.resolve_credentials(_call(), "search")
        assert not is_transient and terminal_failure is not None
        assert terminal_failure.kind == FailureKind.AUTHENTICATION
        _, network_failure, is_transient = await unrelated_network_error.resolve_credentials(
            _call(), "search"
        )
        assert not is_transient and network_failure is not None
    finally:
        await transient.aclose()
        await terminal.aclose()
        await unrelated_network_error.aclose()


@pytest.mark.asyncio
async def test_local_connection_refusal_is_retryable_before_send() -> None:
    """A refused loopback dial cannot have reached Notion."""
    with socket.socket() as endpoint:
        endpoint.bind(("127.0.0.1", 0))
        port = endpoint.getsockname()[1]
    client = Client(Config(endpoint=f"http://127.0.0.1:{port}"), FailingCredentials(ValueError()))
    try:
        response = await client.request("ntn_test_token_value", "GET", "/search")
        assert response.error is not None and response.error.provably_not_sent
        assert classify_read("search", response).outcome == "retry"
        assert classify_write("createPage", response, False).outcome == "retry"
    finally:
        await client.aclose()
