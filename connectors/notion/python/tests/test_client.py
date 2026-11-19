# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check Notion credential and local transport classifications."""

import socket

import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    FailureKind,
    OperationRef,
    ReauthorizationRequiredError,
)
from dex_connectors.sdk.projectconfig import (
    ConnectionNotSelectedError,
    CredentialBrokerRejectedError,
    CredentialBrokerUnavailableError,
    ObjectNotFoundError,
)

import dex_connectors.notion._generated.connection as generated_connection
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
@pytest.mark.parametrize(
    ("error", "expected_transient"),
    [
        (CredentialBrokerUnavailableError("broker down"), True),
        (ValueError("connection is not configured"), True),
        (ConnectionError("unknown source"), True),
        (ReauthorizationRequiredError(), False),
        (ObjectNotFoundError(), False),
        (ConnectionNotSelectedError(), False),
        (CredentialBrokerRejectedError(), False),
        (generated_connection.CredentialMaterialError("stored credential is malformed"), False),
    ],
)
async def test_credential_failures_are_terminal_only_for_the_reconnect_set(
    error: Exception, expected_transient: bool
) -> None:
    """Only reauthorization, an absent or unselected connection, and a broker refusal are terminal."""
    client = Client(Config(), FailingCredentials(error))
    try:
        _, credential_failure, is_transient = await client.resolve_credentials(_call(), "search")
        assert is_transient == expected_transient and credential_failure is not None
        assert credential_failure.kind == FailureKind.AUTHENTICATION
    finally:
        await client.aclose()


class MalformedProject:
    """Serve stored credential material that fails the generated decoder."""

    async def resolve_credential(self, key: object, call_id: str) -> dict[str, object]:
        """Return material with an undeclared field."""
        return {"unexpected_field": "value"}


@pytest.mark.asyncio
async def test_malformed_stored_credentials_from_the_project_source_are_terminal() -> None:
    """The generated project credential source rejects malformed material, and the client never retries it."""
    source = generated_connection._ProjectCredentialSource(MalformedProject(), object())  # type: ignore[arg-type]
    client = Client(Config(), source)  # type: ignore[arg-type]
    try:
        _, credential_failure, is_transient = await client.resolve_credentials(_call(), "search")
        assert not is_transient and credential_failure is not None
    finally:
        await client.aclose()


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
