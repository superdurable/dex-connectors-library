# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""A fake Gmail API for Trigger tests: an inbox listed newest first, metadata reads, and failures."""

import asyncio
import base64
import json
from dataclasses import dataclass, field
from typing import Any

import httpx
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerLogger,
    TriggerLogLevel,
    TriggerLogValue,
)

from dex_connectors.google_gmail import Config, Connection, Credentials, GmailProvider, new_connection

# Split so push protection never mistakes the inert fixture token for a credential.
ACCESS_TOKEN = "SENTINEL-" + "ACCESS"
CONNECTION_REFERENCE = ConnectionRef("google", "gmail-inbox")
ENDPOINT = "https://gmail.example/gmail/v1"


@dataclass
class FakeGmailMessage:
    """One message the fake lists and reads."""

    id: str
    thread_id: str
    sender: str = "Ada <ada@example.com>"
    subject: str = "Order question"
    snippet: str = "Please check order 1042"
    in_reply_to: str = ""
    references: str = ""
    internal_date: str = "1767312000000"


def root_message(message_id: str, thread_id: str = "", **overrides: Any) -> FakeGmailMessage:
    """A root message from Ada."""
    return FakeGmailMessage(message_id, thread_id or message_id, **overrides)


def reply_message(message_id: str, thread_id: str, **overrides: Any) -> FakeGmailMessage:
    """A reply in thread_id."""
    values: dict[str, Any] = {
        "subject": "Re: Order question",
        "snippet": "Approved, thanks",
        "in_reply_to": f"<{thread_id}@mail.example.com>",
        **overrides,
    }
    return FakeGmailMessage(message_id, thread_id, **values)


@dataclass
class FakeGmail:
    """Fake Gmail API bound to an httpx MockTransport."""

    inbox: list[FakeGmailMessage] = field(default_factory=list)
    requests: list[httpx.Request] = field(default_factory=list)
    list_failures: list[int] = field(default_factory=list)
    read_failures: dict[str, int] = field(default_factory=dict)
    list_count: int = 0
    sent: list[str] = field(default_factory=list)
    full_read_gates: dict[str, asyncio.Event] = field(default_factory=dict)

    def receive(self, message: FakeGmailMessage) -> None:
        """Add a message as the newest inbox entry."""
        self.inbox.insert(0, message)

    def reads(self) -> list[str]:
        """Message IDs of the message reads, in order."""
        return [
            request.url.path.rsplit("/", 1)[-1]
            for request in self.requests
            if request.method == "GET" and request.url.path != "/gmail/v1/users/me/messages"
        ]

    async def handle(self, request: httpx.Request) -> httpx.Response:
        """Answer one Gmail API request."""
        self.requests.append(request)
        path = request.url.path
        if path == "/gmail/v1/users/me/messages/send" and request.method == "POST":
            body = json.loads(request.content)
            raw = body["raw"]
            self.sent.append(base64.urlsafe_b64decode(raw + "=" * (-len(raw) % 4)).decode())
            thread_id = body.get("threadId") or f"sent-thread-{len(self.sent)}"
            return httpx.Response(200, json={"id": f"sent-{len(self.sent)}", "threadId": thread_id})
        if path == "/gmail/v1/users/me/messages":
            if request.url.params.get("q", "").startswith("in:sent"):
                return httpx.Response(200, json={})
            self.list_count += 1
            if self.list_failures:
                query = request.url.params.get("q", "")
                return httpx.Response(
                    self.list_failures.pop(0), json={"error": {"message": f"{query} failed"}}
                )
            page = self.inbox[: int(request.url.params["maxResults"])]
            listed = [{"id": message.id, "threadId": message.thread_id} for message in page]
            return httpx.Response(200, json={"messages": listed} if listed else {})
        message_id = path.rsplit("/", 1)[-1]
        if request.url.params.get("format") == "full" and message_id in self.full_read_gates:
            await self.full_read_gates[message_id].wait()
        failure = self.read_failures.pop(message_id, None)
        if failure is not None:
            return httpx.Response(failure, json={})
        message = next((candidate for candidate in self.inbox if candidate.id == message_id), None)
        if message is None:
            return httpx.Response(404, json={})
        headers = [
            {"name": "From", "value": message.sender},
            {"name": "Subject", "value": message.subject},
            {"name": "Message-ID", "value": f"<{message.id}@mail.example.com>"},
        ]
        if message.in_reply_to:
            headers.append({"name": "In-Reply-To", "value": message.in_reply_to})
        if message.references:
            headers.append({"name": "References", "value": message.references})
        text = base64.urlsafe_b64encode(message.snippet.encode()).decode().rstrip("=")
        return httpx.Response(
            200,
            json={
                "id": message.id,
                "threadId": message.thread_id,
                "snippet": message.snippet,
                "internalDate": message.internal_date,
                "labelIds": ["INBOX"],
                "payload": {"mimeType": "text/plain", "headers": headers, "body": {"data": text}},
            },
        )


@dataclass(frozen=True)
class RecordedTriggerLog:
    """One record a RecordingLogger received."""

    level: TriggerLogLevel
    message: str
    attributes: dict[str, TriggerLogValue]


@dataclass
class RecordingLogger(TriggerLogger):
    """Records every Trigger record at every level."""

    records: list[RecordedTriggerLog] = field(default_factory=list)

    def is_enabled_for(self, level: TriggerLogLevel) -> bool:
        return True

    def log(self, level: TriggerLogLevel, message: str, attributes: Any) -> None:
        self.records.append(RecordedTriggerLog(level, message, dict(attributes)))

    def find(self, message: str, **attributes: TriggerLogValue) -> list[RecordedTriggerLog]:
        """Records with this message whose attributes include attributes."""
        return [
            record
            for record in self.records
            if record.message == message
            and all(record.attributes.get(key) == value for key, value in attributes.items())
        ]


def fake_provider(
    gmail: FakeGmail, http_client: httpx.AsyncClient, logger: TriggerLogger | None = None
) -> GmailProvider:
    """A Gmail provider on the fake with static credentials and a one-second poll interval."""
    credentials = StaticCredentialProvider(
        {CONNECTION_REFERENCE: Credentials(SecretString(ACCESS_TOKEN), "owner@example.com")}
    )
    return GmailProvider(
        Config(endpoint=ENDPOINT, poll_interval_seconds=1),
        credentials,
        http_client=http_client,
        logger=logger,
    )


def fake_connection(
    gmail: FakeGmail, http_client: httpx.AsyncClient, logger: TriggerLogger | None = None
) -> Connection:
    """A Gmail connection on the fake."""
    return new_connection(fake_provider(gmail, http_client, logger), CONNECTION_REFERENCE)


def fake_http_client(gmail: FakeGmail) -> httpx.AsyncClient:
    """An httpx client that answers through the fake."""
    return httpx.AsyncClient(transport=httpx.MockTransport(gmail.handle))
