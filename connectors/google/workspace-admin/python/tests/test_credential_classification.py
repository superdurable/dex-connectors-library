# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Credential resolve failures follow one terminal or transient classification."""

from types import SimpleNamespace
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
from dex_connectors.sdk.projectconfig import ConnectionKey, CredentialMaterial, Scope
from dex_connectors.sdk.projectconfig.broker import (
    ConnectionNotSelectedError,
    CredentialBrokerRejectedError,
    CredentialBrokerUnavailableError,
)
from dex_connectors.sdk.projectconfig.provider import CredentialProvider
from dex_connectors.sdk.projectconfig.storage import ObjectNotFoundError

from dex_connectors.google_workspace_admin import Client, Config, CreateUserInput, Credentials, GetUserInput
from dex_connectors.google_workspace_admin._generated.connection import (
    CredentialMaterialError,
    decode_credentials,
)

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
        (CredentialMaterialError("stored credential is malformed"), True),
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


class MalformedMaterialStore:
    """Serve stored material whose fields the connector does not declare."""

    scope = Scope("project-1", "live")

    def _shared_connection(self, key: ConnectionKey) -> None:
        return None

    async def read_credential_material(self, key: ConnectionKey) -> tuple[CredentialMaterial, Any]:
        """Return malformed material through the SDK's trusted read."""
        material = CredentialMaterial({"unexpected": "value"}, None, "google-oauth")
        return material, SimpleNamespace(expires_at=None)


def malformed_project_credentials() -> CredentialProvider[Credentials]:
    """Build the released SDK provider, which wraps the generated decoder's error."""
    key = ConnectionKey("google-workspace-admin", "workspace")
    return CredentialProvider(cast(Any, MalformedMaterialStore()), key, decode_credentials)


@pytest.mark.asyncio
async def test_malformed_stored_credentials_are_terminal_behind_the_sdk_wrapper() -> None:
    """Malformed material selects defect instead of retrying forever."""
    client = Client(Config(), malformed_project_credentials())
    read = await client.get_user().invoke(CALL, INPUT)
    assert read.kind == "branch" and read.branch == "defect"
    create_call = Call(CALL.id, CALL.connection, OperationRef("google-workspace-admin", "createUser"))
    create_input = CreateUserInput(primary_email="ada@example.com", given_name="Ada", family_name="Lovelace")
    created = await client.create_user().invoke(create_call, create_input)
    assert created.kind == "branch" and created.branch == "defect"


@pytest.mark.asyncio
async def test_unconfigured_static_connection_retries() -> None:
    """A static provider without the connection is transient."""
    client = Client(Config(), StaticCredentialProvider({}))
    assert (await client.get_user().invoke(CALL, INPUT)).kind == "retry"
