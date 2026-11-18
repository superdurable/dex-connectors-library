# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Credential resolve failures follow one terminal or transient classification."""

from uuid import uuid4

import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    OperationRef,
    ReauthorizationRequiredError,
    StaticCredentialProvider,
)
from dex_connectors.sdk.projectconfig.broker import (
    ConnectionNotSelectedError,
    CredentialBrokerRejectedError,
    CredentialBrokerUnavailableError,
)
from dex_connectors.sdk.projectconfig.storage import ObjectNotFoundError

from dex_connectors.google_cloud_storage import Client, Config, Credentials

CALL = Call(
    CallID(str(uuid4())),
    ConnectionRef("google-cloud-storage", "google-cloud-storage"),
    OperationRef("google-cloud-storage", "uploadObject"),
)


class FailingCredentials:
    """Raise one error on every resolve."""

    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: Call) -> Credentials:
        """Raise the configured error."""
        raise self.error


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("error", "expected"),
    [
        (ReauthorizationRequiredError(), "credential"),
        (ObjectNotFoundError(), "credential"),
        (ConnectionNotSelectedError(), "credential"),
        (CredentialBrokerRejectedError(), "credential"),
        (CredentialBrokerUnavailableError(), "refresh"),
        (ValueError("connection is not configured"), "refresh"),
        (RuntimeError("unlisted"), "refresh"),
    ],
)
async def test_resolve_failures_are_terminal_only_for_reconnection(error: Exception, expected: str) -> None:
    """Only reconnection-only failures are terminal; the rest retry."""
    client = Client(Config(), FailingCredentials(error))  # type: ignore[arg-type]
    assert await client.resolve_token(CALL) == ("", expected)
    await client.aclose()


@pytest.mark.asyncio
async def test_unconfigured_static_connection_is_transient() -> None:
    """A static provider without the connection retries instead of selecting defect."""
    client = Client(Config(), StaticCredentialProvider({}))
    assert await client.resolve_token(CALL) == ("", "refresh")
    await client.aclose()
