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

CONNECTION = sdk.ConnectionRef("dropbox", "dropbox")


class _FailingCredentials:
    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: sdk.Call) -> dropbox.Credentials:
        raise self.error


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
