# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Credential resolve failures follow one terminal or transient classification."""

from typing import Any, cast
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

from dex_connectors.google_workspace_admin import Client, Config, Credentials, GetUserInput

CALL = Call(
    CallID(str(uuid4())),
    ConnectionRef("google", "workspace"),
    OperationRef("google-workspace-admin", "getUser"),
)
INPUT = GetUserInput(user_key="ada@example.com")


class FailingCredentials:
    """Raise one error on every resolve."""

    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: Call) -> Credentials:
        """Raise the configured error."""
        raise self.error


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("error", "is_terminal"),
    [
        (ReauthorizationRequiredError(), True),
        (ObjectNotFoundError(), True),
        (ConnectionNotSelectedError(), True),
        (CredentialBrokerRejectedError(), True),
        (CredentialBrokerUnavailableError(), False),
        (ValueError("connection is not configured"), False),
        (RuntimeError("unlisted"), False),
    ],
)
async def test_get_user_credential_failures_retry_unless_terminal(
    error: Exception, is_terminal: bool
) -> None:
    """Only reconnection-only failures select defect."""
    client = Client(Config(), cast(Any, FailingCredentials(error)))
    result = await client.get_user().invoke(CALL, INPUT)
    if is_terminal:
        assert result.kind == "branch" and result.branch == "defect"
    else:
        assert result.kind == "retry"


@pytest.mark.asyncio
async def test_unconfigured_static_connection_retries() -> None:
    """A static provider without the connection is transient."""
    client = Client(Config(), StaticCredentialProvider({}))
    assert (await client.get_user().invoke(CALL, INPUT)).kind == "retry"
