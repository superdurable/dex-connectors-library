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

CONNECTION = sdk.ConnectionRef("box", "box")


class _FailingCredentials:
    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: sdk.Call) -> box.Credentials:
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
