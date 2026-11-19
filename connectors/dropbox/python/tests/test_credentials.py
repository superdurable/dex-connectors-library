# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Credential read failures are terminal only for the reconnect set."""

import dex_connectors.sdk as sdk
import httpx
import pytest
from dex_connectors.sdk.projectconfig.broker import (
    ConnectionNotSelectedError,
    CredentialBrokerRejectedError,
    CredentialBrokerUnavailableError,
)
from dex_connectors.sdk.projectconfig.storage import ObjectNotFoundError

import dex_connectors.dropbox as dropbox
from dex_connectors.dropbox._generated.connection import CredentialMaterialError, decode_credentials

CONNECTION = sdk.ConnectionRef("dropbox", "dropbox")


class _FailingCredentials:
    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: sdk.Call) -> dropbox.Credentials:
        raise self.error


class _LaterAttemptContext:
    attempt = 2


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


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("error", "kind"),
    [
        (CredentialBrokerUnavailableError("broker down"), "retry"),
        (ValueError("connection is not configured"), "retry"),
        (RuntimeError("credential store hiccup"), "retry"),
        (sdk.ReauthorizationRequiredError(), "branch"),
        (ObjectNotFoundError(), "branch"),
        (ConnectionNotSelectedError(), "branch"),
        (CredentialBrokerRejectedError(), "branch"),
        (CredentialMaterialError("Dropbox credential access_token is required"), "branch"),
        (_sdk_decoding_failure(), "branch"),
    ],
)
async def test_upload_classifies_credential_failures(error: Exception, kind: str) -> None:
    def respond(request: httpx.Request) -> httpx.Response:
        raise AssertionError("no request may be sent")

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as http_client:
        client = dropbox.Client(dropbox.Config(), _FailingCredentials(error), http_client)
        call = sdk.Call(
            sdk.CallID("6d3b1f0e-7a2c-5b9d-8e4f-1a2b3c4d5e6f"),
            CONNECTION,
            sdk.OperationRef("dropbox", "uploadFile"),
        )
        attempt = await client.upload_file().invoke(
            call, dropbox.UploadFileInput("/Projects/example.txt", "SGVsbG8=")
        )
        assert attempt.kind == kind
        if kind == "branch":
            assert attempt.branch == "defect"
        later_call = sdk.Call(
            sdk.CallID("6d3b1f0e-7a2c-5b9d-8e4f-1a2b3c4d5e6f"),
            CONNECTION,
            sdk.OperationRef("dropbox", "uploadFile"),
            context=_LaterAttemptContext(),  # type: ignore[arg-type]
        )
        later = await client.upload_file().invoke(
            later_call, dropbox.UploadFileInput("/Projects/example.txt", "SGVsbG8=")
        )
        assert later.kind == "retry", "a later attempt may follow a stored overwrite"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("status", "body", "kind"),
    [
        (403, b'{"error_summary":"access_denied/..."}', "branch"),
        (409, b'{"error_summary":"path/insufficient_space/..","error":{".tag":"path"}}', "branch"),
        (409, b"not json", "retry"),
        (412, b'{"error_summary":"x"}', "retry"),
        (400, b'{"id":"id:example","name":"example.txt","path_display":"/Projects/example.txt"}', "retry"),
        (200, b'{"echo":"test-token"}', "retry"),
        (200, b'{"id":"id:example"}', "retry"),
    ],
)
async def test_upload_selects_provider_rejected_only_for_a_definite_answer(
    status: int, body: bytes, kind: str
) -> None:
    """An unlisted status, a refusal carrying a file, or an unusable 2xx body repeats the overwrite."""
    stored = dropbox.Credentials(
        oauth_client_id="client-id",
        oauth_client_secret=sdk.SecretString("client-secret"),
        access_token=sdk.SecretString("test-token"),
        refresh_token=sdk.SecretString("refresh-token"),
    )
    credentials = sdk.StaticCredentialProvider({CONNECTION: stored})
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(lambda _: httpx.Response(status, content=body))
    ) as http_client:
        client = dropbox.Client(dropbox.Config(), credentials, http_client)
        call = sdk.Call(
            sdk.CallID("6d3b1f0e-7a2c-5b9d-8e4f-1a2b3c4d5e6f"),
            CONNECTION,
            sdk.OperationRef("dropbox", "uploadFile"),
        )
        attempt = await client.upload_file().invoke(
            call, dropbox.UploadFileInput("/Projects/example.txt", "SGVsbG8=")
        )
    assert attempt.kind == kind
    if kind == "branch":
        assert attempt.branch == "providerRejected"
