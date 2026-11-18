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

from dex_connectors.google_cloud_storage import Client, Config, Credentials, UploadObjectInput
from dex_connectors.google_cloud_storage._generated.connection import (
    CredentialMaterialError,
    decode_credentials,
)

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
        (CredentialMaterialError("stored credential is malformed"), "credential"),
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


class MalformedMaterialStore:
    """Serve stored material whose fields the connector does not declare."""

    scope = Scope("project-1", "live")

    def _shared_connection(self, key: ConnectionKey) -> None:
        return None

    async def read_credential_material(self, key: ConnectionKey) -> tuple[CredentialMaterial, Any]:
        """Return malformed material through the SDK's trusted read."""
        material = CredentialMaterial({"unexpected": "value"}, None, "google-oauth")
        return material, SimpleNamespace(expires_at=None)


@pytest.mark.asyncio
async def test_malformed_stored_credentials_are_terminal_behind_the_sdk_wrapper() -> None:
    """The released SDK provider hides the decoder error; the upload still selects defect."""
    key = ConnectionKey("google-cloud-storage", "google-cloud-storage")
    provider = CredentialProvider(cast(Any, MalformedMaterialStore()), key, decode_credentials)
    client = Client(Config(), provider)
    assert await client.resolve_token(CALL) == ("", "credential")
    upload = UploadObjectInput(
        bucket="example-bucket", name="a.txt", content_base64="aGVsbG8=", content_type="text/plain"
    )
    result = await client.upload_object().invoke(CALL, upload)
    assert result.kind == "branch" and result.branch == "defect"
    await client.aclose()


@pytest.mark.asyncio
async def test_unconfigured_static_connection_is_transient() -> None:
    """A static provider without the connection retries instead of selecting defect."""
    client = Client(Config(), StaticCredentialProvider({}))
    assert await client.resolve_token(CALL) == ("", "refresh")
    await client.aclose()
