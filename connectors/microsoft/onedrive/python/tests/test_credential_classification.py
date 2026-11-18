# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Every OneDrive operation classifies credential failures the same way."""

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    OperationRef,
    ReauthorizationRequiredError,
    SecretString,
    StaticCredentialProvider,
)
from dex_connectors.sdk.projectconfig.broker import (
    ConnectionNotSelectedError,
    CredentialBrokerRejectedError,
    CredentialBrokerUnavailableError,
    RejectedCredentialNotRefreshedError,
)
from dex_connectors.sdk.projectconfig.storage import ObjectNotFoundError

from dex_connectors.microsoft_onedrive import Client, Config, CreateFolderInput, Credentials, GetFileInput

CONNECTION = ConnectionRef("microsoft", "onedrive-files")
TERMINAL_ERRORS: tuple[Exception, ...] = (
    ReauthorizationRequiredError(),
    ObjectNotFoundError(),
    ConnectionNotSelectedError(),
    CredentialBrokerRejectedError(),
)
TRANSIENT_ERRORS: tuple[Exception, ...] = (
    CredentialBrokerUnavailableError(),
    RejectedCredentialNotRefreshedError(),
    ValueError("connection is not configured"),
    TimeoutError(),
    RuntimeError("unlisted"),
)


class FailingCredentials:
    """A credential provider that always fails with one error."""

    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: Call) -> Credentials:
        """Raise the configured error."""
        raise self.error


def _respond(request: httpx.Request) -> httpx.Response:
    raise AssertionError("no request is expected")


async def _kinds(credentials: object) -> tuple[str, str]:
    async with httpx.AsyncClient(transport=httpx.MockTransport(_respond)) as http_client:
        client = Client(Config(endpoint="http://127.0.0.1:12345"), credentials, http_client)  # type: ignore[arg-type]
        read_call = Call(CallID("read-call"), CONNECTION, OperationRef("microsoft-onedrive", "getFile"))
        read = await client.get_file().invoke(read_call, GetFileInput(item_id="item-1"))
        write_operation = OperationRef("microsoft-onedrive", "createFolder")
        write_call = Call(CallID("write-call"), CONNECTION, write_operation)
        written = await client.create_folder().invoke(write_call, CreateFolderInput(name="Dex copies"))
    return (read.branch or read.kind, written.branch or written.kind)


@pytest.mark.asyncio
@pytest.mark.parametrize("error", TERMINAL_ERRORS + TRANSIENT_ERRORS)
async def test_every_operation_classifies_credential_failures_the_same_way(error: Exception) -> None:
    """Only the four terminal classes select defect; every other failure retries without a request."""
    expected = "defect" if error in TERMINAL_ERRORS else "retry"
    assert await _kinds(FailingCredentials(error)) == (expected, expected)


@pytest.mark.asyncio
async def test_invalid_credential_material_and_an_unconfigured_connection() -> None:
    """A header-unsafe token is a defect; a static connection that is not configured retries."""
    unsafe = StaticCredentialProvider({CONNECTION: Credentials(access_token=SecretString("bad\ntoken"))})
    assert await _kinds(unsafe) == ("defect", "defect")
    other = ConnectionRef("microsoft", "other")
    unconfigured = StaticCredentialProvider({other: Credentials(access_token=SecretString("token"))})
    assert await _kinds(unconfigured) == ("retry", "retry")
