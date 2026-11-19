# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""An accepted Outlook send is never resent, even when its answer breaks."""

from collections.abc import AsyncIterator
from datetime import UTC, datetime, timedelta

import httpx
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    CredentialProvider,
    MutationResult,
    OperationRef,
    SecretString,
    run_mutation,
)

from dex_connectors.microsoft_outlook_mail import Client, Config, Credentials, SendMessageInput


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self) -> None:
        self.attempt = 1
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
        self.heartbeat_value: object = None

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        return self.heartbeat_value is not None

    def get_last_heartbeat_value(self, value_type: object) -> object:
        """Return the last heartbeat exactly as the connector wrote it."""
        return self.heartbeat_value

    async def heartbeat(self, value: object = None) -> None:
        """Record the heartbeat."""
        self.heartbeat_value = value


class StaticCredentials(CredentialProvider[Credentials]):
    """Resolve one delegated access token."""

    async def resolve(self, call: Call) -> Credentials:
        """Return the token."""
        return Credentials(
            client_id="client-id", client_secret=SecretString("secret"),
            access_token=SecretString("access-token"), refresh_token=SecretString("refresh"),
        )


class BrokenAcceptedBody(httpx.AsyncByteStream):
    """A 202 body that breaks after its headers arrived."""

    async def __aiter__(self) -> AsyncIterator[bytes]:
        yield b"{"
        raise httpx.ReadError("connection reset")


async def test_accepted_send_whose_body_breaks_selects_sent_without_resending() -> None:
    sends: list[str] = []

    def respond(request: httpx.Request) -> httpx.Response:
        path = request.url.path
        if request.method == "GET" and path.endswith("/messages"):
            return httpx.Response(200, json={"value": []})
        if request.method == "POST" and path.endswith("/messages"):
            recipients = [{"emailAddress": {"address": "a@example.com"}}]
            return httpx.Response(201, json={"id": "draft1", "isDraft": True, "toRecipients": recipients})
        if request.method == "POST" and path.endswith("/draft1/send"):
            sends.append(path)
            return httpx.Response(202, stream=BrokenAcceptedBody())
        raise AssertionError(f"unexpected request {request.method} {path}")

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as transport:
        client = Client(Config(), StaticCredentials(), transport)
        call = Call(
            CallID("74390522-858d-5ed6-bdf7-04e979ba9e4f"), ConnectionRef("microsoft", "outlook-mail"),
            OperationRef("outlook-mail", "sendMessage"), context=AttemptContext(), max_sends=3,
        )
        result = await run_mutation(
            client.send_message(), call, SendMessageInput(to=["a@example.com"], subject="Test", text="Body")
        )
    assert isinstance(result, MutationResult) and result.branch == "sent"
    assert len(sends) == 1
