# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Repeat-safe Calendly writes retry an answer they cannot read in full."""

from collections.abc import Callable
from datetime import UTC, datetime, timedelta

import dex_connectors.sdk as sdk
import httpx
import pytest

import dex_connectors.calendly as calendly

CONNECTION = sdk.ConnectionRef("calendly", "scheduling")
CALL_ID = sdk.CallID("0f1c2d3e-4b5a-5c6d-8e7f-901a2b3c4d5e")
EVENT_URI = "https://api.calendly.com/scheduled_events/EVENT0001"
ORGANIZATION_URI = "https://api.calendly.com/organizations/ORG00001"
USER_URI = "https://api.calendly.com/users/USER0001"
OVERSIZED_ERROR_BODY = b"x" * (64 * 1024 + 1)


class AttemptContext:
    """Provide one Execute attempt without a heartbeat."""

    def __init__(self) -> None:
        self.attempt = 1
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)

    def has_last_heartbeat_value(self) -> bool:
        """Report that no earlier attempt recorded a heartbeat."""
        return False

    async def heartbeat(self, value: object = None) -> None:
        """Accept a heartbeat that these safe writes never record."""


async def run(
    operation_name: str,
    input_value: object,
    respond: Callable[[httpx.Request], httpx.Response],
    config: calendly.Config | None = None,
) -> sdk.MutationResult[object] | sdk.RetryError:
    """Run one attempt of a repeat-safe Mutation against a scripted Calendly."""
    credentials = sdk.StaticCredentialProvider(
        {
            CONNECTION: calendly.Credentials(
                access_token=sdk.SecretString("inert-token"),
                webhook_signing_key=sdk.SecretString("inert-signing-key"),
            )
        }
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as transport:
        client = calendly.Client(config or calendly.Config(), credentials, transport)
        operation = getattr(client, operation_name)()
        call = sdk.Call(
            CALL_ID, CONNECTION, sdk.OperationRef("calendly", operation.definition().operation.operation_id),
            context=AttemptContext(),
        )
        try:
            return await sdk.run_mutation(operation, call, input_value)
        except sdk.RetryError as error:
            return error


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [404, 422])
async def test_cancellation_refusal_with_oversized_body_retries(status: int) -> None:
    result = await run(
        "cancel_scheduled_event",
        calendly.CancelScheduledEventInput(EVENT_URI),
        lambda _: httpx.Response(status, content=OVERSIZED_ERROR_BODY),
    )
    assert isinstance(result, sdk.RetryError)


@pytest.mark.asyncio
@pytest.mark.parametrize("answer", ["malformed", "oversized"])
async def test_cancellation_read_back_it_cannot_read_retries(answer: str) -> None:
    def respond(request: httpx.Request) -> httpx.Response:
        if request.method == "POST":
            return httpx.Response(403, json={"title": "Permission Denied"})
        if answer == "malformed":
            return httpx.Response(200, content=b"not-json")
        return httpx.Response(200, content=b"{" + b" " * 256 + b"}")

    result = await run(
        "cancel_scheduled_event",
        calendly.CancelScheduledEventInput(EVENT_URI),
        respond,
        calendly.Config(max_response_bytes=128),
    )
    assert isinstance(result, sdk.RetryError)


@pytest.mark.asyncio
async def test_subscription_refusal_with_oversized_body_retries() -> None:
    def respond(request: httpx.Request) -> httpx.Response:
        if request.method == "GET":
            return httpx.Response(200, json={"collection": [], "pagination": {"next_page_token": None}})
        return httpx.Response(400, content=OVERSIZED_ERROR_BODY)

    result = await run(
        "create_webhook_subscription",
        calendly.CreateWebhookSubscriptionInput(
            "https://hooks.example.com/webhooks/calendly",
            user_uri=USER_URI,
            organization_uri=ORGANIZATION_URI,
        ),
        respond,
    )
    assert isinstance(result, sdk.RetryError)
