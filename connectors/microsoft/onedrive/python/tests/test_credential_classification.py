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

from dex_connectors.microsoft_onedrive import (
    Client,
    Config,
    CreateFolderInput,
    Credentials,
    GetFileInput,
    UploadFileInput,
)
from dex_connectors.microsoft_onedrive._generated.connection import (
    CredentialMaterialError,
    decode_credentials,
)

CONNECTION = ConnectionRef("microsoft", "onedrive-files")


def _sdk_decoding_failure() -> ValueError:
    """Reproduce the SDK refresh provider, which replaces the decoder error with a plain ValueError."""
    try:
        try:
            decode_credentials({"access_token": 7})
        except ValueError:
            raise ValueError("project credential decoding failed") from None
    except ValueError as error:
        return error
    raise AssertionError("malformed material decoded")


TERMINAL_ERRORS: tuple[Exception, ...] = (
    ReauthorizationRequiredError(),
    ObjectNotFoundError(),
    ConnectionNotSelectedError(),
    CredentialBrokerRejectedError(),
    CredentialMaterialError("Microsoft OneDrive credential access_token is required"),
    _sdk_decoding_failure(),
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


class _LaterAttemptContext:
    attempt = 2


@pytest.mark.asyncio
@pytest.mark.parametrize("error", TERMINAL_ERRORS)
async def test_a_later_write_attempt_retries_a_terminal_credential_failure(error: Exception) -> None:
    """A later attempt may follow an applied write, so defect would misreport it."""
    async with httpx.AsyncClient(transport=httpx.MockTransport(_respond)) as http_client:
        config = Config(endpoint="http://127.0.0.1:12345")
        client = Client(config, FailingCredentials(error), http_client)  # type: ignore[arg-type]
        operation = OperationRef("microsoft-onedrive", "createFolder")
        call = Call(CallID("write-call"), CONNECTION, operation, context=_LaterAttemptContext())  # type: ignore[arg-type]
        written = await client.create_folder().invoke(call, CreateFolderInput(name="Dex copies"))
    assert written.kind == "retry"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("status", "body", "expected"),
    [
        (403, b'{"error":{"code":"accessDenied"}}', "providerRejected"),
        (507, b'{"error":{"code":"quotaLimitReached"}}', "retry"),
        (412, b'{"error":{"code":"resourceModified"}}', "retry"),
        (409, b'{"error":{"code":"otherConflict"}}', "retry"),
        (403, b"x" * 70000, "retry"),
    ],
)
async def test_writes_select_provider_rejected_only_for_a_definite_answer(
    status: int, body: bytes, expected: str
) -> None:
    """An unlisted status, another conflict, or an unreadable body repeats the safe write."""
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(lambda _: httpx.Response(status, content=body))
    ) as http_client:
        credentials = StaticCredentialProvider({CONNECTION: Credentials(access_token=SecretString("token"))})
        config = Config(endpoint="http://127.0.0.1:12345", max_response_bytes=1024)
        client = Client(config, credentials, http_client)
        folder_operation = OperationRef("microsoft-onedrive", "createFolder")
        folder_call = Call(CallID("write-call"), CONNECTION, folder_operation)
        folder = await client.create_folder().invoke(folder_call, CreateFolderInput(name="Dex copies"))
        upload_operation = OperationRef("microsoft-onedrive", "uploadFile")
        upload_call = Call(CallID("write-call"), CONNECTION, upload_operation)
        upload_input = UploadFileInput(
            name="a.txt", conflict_behavior="replace", mime_type="text/plain", text_content="x"
        )
        upload = await client.upload_file().invoke(upload_call, upload_input)
    assert (folder.branch or folder.kind) == expected
    assert (upload.branch or upload.kind) == expected
