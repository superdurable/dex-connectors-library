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

import dex_connectors.posthog as posthog

CONNECTION = sdk.ConnectionRef("posthog", "posthog")


class _FailingCredentials:
    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: sdk.Call) -> posthog.Credentials:
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
async def test_capture_classifies_credential_failures(error: Exception, kind: str) -> None:
    def respond(request: httpx.Request) -> httpx.Response:
        raise AssertionError("no request may be sent")

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as http_client:
        client = posthog.Client(posthog.Config(), _FailingCredentials(error), http_client)
        call = sdk.Call(
            sdk.CallID("2c9e4b1a-6d3f-5e8a-9b0c-7d1e2f3a4b5c"),
            CONNECTION,
            sdk.OperationRef("posthog", "captureEvent"),
            sdk.IdempotencyKey("2c9e4b1a-6d3f-5e8a-9b0c-7d1e2f3a4b5c"),
        )
        attempt = await client.capture_event().invoke(
            call, posthog.CaptureEventInput("Signed Up", "member-1", "2026-10-29T00:00:00Z")
        )
        assert attempt.kind == kind
        if kind == "branch":
            assert attempt.branch == "defect"
