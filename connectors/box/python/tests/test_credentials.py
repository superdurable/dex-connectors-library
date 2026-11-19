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

import dex_connectors.box as box
from dex_connectors.box._generated.connection import CredentialMaterialError, decode_credentials

CONNECTION = sdk.ConnectionRef("box", "box")


class _FailingCredentials:
    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: sdk.Call) -> box.Credentials:
        raise self.error


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


class _LaterAttemptContext:
    attempt = 2


def _upload_call(context: object = None) -> sdk.Call:
    return sdk.Call(
        sdk.CallID("4f4c58d0-1c39-5e8f-9b1a-3f6a1d2c7e10"),
        CONNECTION,
        sdk.OperationRef("box", "uploadFile"),
        context=context,  # type: ignore[arg-type]
    )


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "error",
    [sdk.ReauthorizationRequiredError(), _sdk_decoding_failure(), CredentialBrokerUnavailableError("x")],
)
async def test_later_upload_attempt_retries_every_credential_failure(error: Exception) -> None:
    """A later attempt may follow an applied upload, so even a terminal failure retries."""
    def respond(request: httpx.Request) -> httpx.Response:
        raise AssertionError("no request may be sent")

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as http_client:
        client = box.Client(box.Config(base_url="http://127.0.0.1"), _FailingCredentials(error), http_client)
        attempt = await client.upload_file().invoke(
            _upload_call(_LaterAttemptContext()), box.UploadFileInput("12345", "SGVsbG8=")
        )
    assert attempt.kind == "retry"


_OLD_METADATA = b'{"id":"12345","type":"file","name":"example.txt","sha1":"' + b"0" * 40 + b'","size":5}'


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("metadata_status", "metadata_body", "upload_status", "upload_body", "kind", "posts", "gets"),
    [
        (200, b'{"id":"12345"}', 0, b"", "retry", 0, 1),
        (409, b'{"code":"conflict"}', 0, b"", "retry", 0, 1),
        (403, _OLD_METADATA, 0, b"", "retry", 0, 1),
        (403, b'{"code":"forbidden"}', 0, b"", "branch", 0, 1),
        (200, _OLD_METADATA, 409, b'{"type":"error","code":"conflict"}', "retry", 1, 2),
        (200, _OLD_METADATA, 400, b'{"entries":[' + _OLD_METADATA + b"]}", "retry", 1, 2),
        (200, _OLD_METADATA, 403, b'{"type":"error","code":"access_denied"}', "branch", 1, 1),
    ],
)
async def test_upload_selects_a_branch_only_for_a_definite_answer(
    metadata_status: int,
    metadata_body: bytes,
    upload_status: int,
    upload_body: bytes,
    kind: str,
    posts: int,
    gets: int,
) -> None:
    """Unknown pre-write reads retry, and an unknown upload answer reads back once before retrying."""
    counts = {"GET": 0, "POST": 0}

    def respond(request: httpx.Request) -> httpx.Response:
        counts[request.method] += 1
        if request.method == "POST":
            return httpx.Response(upload_status, content=upload_body)
        return httpx.Response(metadata_status if counts["GET"] == 1 else 200, content=metadata_body)

    credentials = sdk.StaticCredentialProvider(
        {
            CONNECTION: box.Credentials(
                oauth_client_id="client-id",
                oauth_client_secret=sdk.SecretString("client-secret"),
                access_token=sdk.SecretString("test-token"),
                refresh_token=sdk.SecretString("refresh-token"),
            )
        }
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as http_client:
        config = box.Config(base_url="http://127.0.0.1", upload_base_url="http://127.0.0.1")
        client = box.Client(config, credentials, http_client)
        attempt = await client.upload_file().invoke(_upload_call(), box.UploadFileInput("12345", "SGVsbG8="))
    assert attempt.kind == kind
    assert counts == {"GET": gets, "POST": posts}


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
        (CredentialMaterialError("Box credential access_token is required"), "branch"),
        (_sdk_decoding_failure(), "branch"),
    ],
)
async def test_download_classifies_credential_failures(error: Exception, kind: str) -> None:
    def respond(request: httpx.Request) -> httpx.Response:
        raise AssertionError("no request may be sent")

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as http_client:
        client = box.Client(box.Config(base_url="http://127.0.0.1"), _FailingCredentials(error), http_client)
        call = sdk.Call(
            sdk.CallID("4f4c58d0-1c39-5e8f-9b1a-3f6a1d2c7e10"),
            CONNECTION,
            sdk.OperationRef("box", "downloadFile"),
        )
        attempt = await client.download_file().invoke(call, box.DownloadFileInput("12345"))
        assert attempt.kind == kind
        if kind == "branch":
            assert attempt.branch == "defect"
