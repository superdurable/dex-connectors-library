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
from dex_connectors.posthog._generated.connection import CredentialMaterialError

CONNECTION = sdk.ConnectionRef("posthog", "posthog")


def _sdk_decoding_failure() -> ValueError:
    """Build the SDK project provider's wrapper around a malformed stored credential."""
    try:
        try:
            raise CredentialMaterialError("PostHog credential project_token is required")
        except CredentialMaterialError:
            raise ValueError("project credential decoding failed") from None
    except ValueError as error:
        return error


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
        (CredentialMaterialError("PostHog credential project_token is required"), "branch"),
        (_sdk_decoding_failure(), "branch"),
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


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("error", "kind"),
    [
        (CredentialBrokerUnavailableError("broker down"), "retry"),
        (RuntimeError("credential store hiccup"), "retry"),
        (sdk.ReauthorizationRequiredError(), "branch"),
        (CredentialBrokerRejectedError(), "branch"),
        (_sdk_decoding_failure(), "branch"),
    ],
)
async def test_list_insights_classifies_credential_failures(error: Exception, kind: str) -> None:
    def respond(request: httpx.Request) -> httpx.Response:
        raise AssertionError("no request may be sent")

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as http_client:
        client = posthog.Client(posthog.Config(), _FailingCredentials(error), http_client)
        call = sdk.Call(
            sdk.CallID("2c9e4b1a-6d3f-5e8a-9b0c-7d1e2f3a4b5c"),
            CONNECTION,
            sdk.OperationRef("posthog", "listInsights"),
        )
        attempt = await client.list_insights().invoke(call, posthog.ListInsightsInput(42))
        assert attempt.kind == kind
        if kind == "branch":
            assert attempt.branch == "defect"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("status", "kind"), [(302, "retry"), (409, "retry"), (425, "retry"), (422, "branch")]
)
async def test_capture_retries_unknown_statuses(status: int, kind: str) -> None:
    transport = httpx.MockTransport(lambda _: httpx.Response(status))
    async with httpx.AsyncClient(transport=transport) as http_client:
        client = posthog.Client(
            posthog.Config(),
            sdk.StaticCredentialProvider(
                {CONNECTION: posthog.Credentials(sdk.SecretString("phc_token"), sdk.SecretString(""))}
            ),
            http_client,
        )
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
            assert attempt.branch == "providerRejected"
