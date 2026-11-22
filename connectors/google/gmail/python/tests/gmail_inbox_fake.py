# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""A fake Gmail API: an inbox listed newest first, Sent Mail as Gmail keeps it, reads, and failures."""

import asyncio
import base64
import json
from dataclasses import dataclass, field
from email.parser import HeaderParser
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
class FakeSentCopy:
    """One Sent Mail message as Gmail keeps it."""

    id: str
    thread_id: str
    # Stored header names and values, in order.
    headers: list[tuple[str, str]]


def gmail_message_id(message_id: str) -> str:
    """The Message-Id Gmail puts on a Sent copy in place of the client's."""
    return f"<CAGmail-{message_id}@mail.gmail.com>"


def dex_sent_copy(
    message_id: str,
    thread_id: str,
    dex_message_id: str,
    *,
    recipient: str = "<ada@example.com>",
    call_id: str = "",
) -> FakeSentCopy:
    """A Sent copy of a connector send: Gmail's own Message-Id, with the X-Dex headers kept verbatim."""
    headers = [("To", recipient), ("Message-Id", gmail_message_id(message_id))]
    if dex_message_id:
        headers.append(("X-Dex-Message-ID", dex_message_id))
    if call_id:
        headers.append(("X-Dex-Call-ID", call_id))
    return FakeSentCopy(message_id, thread_id, headers)


@dataclass
class FakeSentMail:
    """Sent Mail as real Gmail serves it: no custom-header search, so candidates are read by metadata."""

    # Newest first, as Gmail lists them.
    copies: list[FakeSentCopy] = field(default_factory=list)
    # Status a candidate header read returns instead of the copy, by message ID.
    header_read_failures: dict[str, int] = field(default_factory=dict)
    searches: list[str] = field(default_factory=list)
    header_reads: list[str] = field(default_factory=list)

    def accept(self, raw_mime: str, message_id: str, thread_id: str) -> None:
        """Keep a posted message as Gmail does: its own Message-Id replaces the client's Message-ID."""
        parsed = HeaderParser().parsestr(raw_mime)
        headers = [(name, value) for name, value in parsed.items() if name.lower() != "message-id"]
        headers.append(("Message-Id", gmail_message_id(message_id)))
        self.copies.insert(0, FakeSentCopy(message_id, thread_id, headers))

    def accept_post(self, request: httpx.Request) -> httpx.Response:
        """Answer messages.send by keeping its Sent copy and naming it."""
        body = json.loads(request.content)
        raw = body["raw"]
        message_id = f"sent-{len(self.copies) + 1}"
        thread_id = body.get("threadId") or f"thread-{message_id}"
        self.accept(base64.urlsafe_b64decode(raw + "=" * (-len(raw) % 4)).decode(), message_id, thread_id)
        return httpx.Response(200, json={"id": message_id, "threadId": thread_id})

    def answer(self, request: httpx.Request) -> httpx.Response | None:
        """Answer a Sent Mail search or a Sent copy's metadata read, and None for any other request."""
        if request.method != "GET":
            return None
        if request.url.path.endswith("/users/me/messages"):
            query = request.url.params.get("q", "")
            return self._search(request, query) if query.startswith("in:sent") else None
        message_id = request.url.path.rsplit("/", 1)[-1]
        copy = next((candidate for candidate in self.copies if candidate.id == message_id), None)
        if copy is None or request.url.params.get("format") != "metadata":
            return None
        self.header_reads.append(message_id)
        if message_id in self.header_read_failures:
            return httpx.Response(self.header_read_failures[message_id], json={"error": {"code": 0}})
        # Gmail matches metadataHeaders names case-insensitively and keeps the stored spelling.
        requested = {name.lower() for name in request.url.params.get_list("metadataHeaders")}
        headers = [
            {"name": name, "value": value} for name, value in copy.headers if name.lower() in requested
        ]
        return httpx.Response(200, json={
            "id": copy.id,
            "threadId": copy.thread_id,
            "labelIds": ["SENT"],
            "payload": {"mimeType": "text/plain", "headers": headers},
        })

    def _search(self, request: httpx.Request, query: str) -> httpx.Response:
        self.searches.append(query)
        recipient = query.partition(" to:")[2].lower()
        matching = [
            copy
            for copy in self.copies
            if not recipient
            or any(name.lower() == "to" and recipient in value.lower() for name, value in copy.headers)
        ]
        listed = matching[: int(request.url.params.get("maxResults", "100"))]
        if not listed:
            return httpx.Response(200, json={"resultSizeEstimate": 0})
        return httpx.Response(200, json={
            "messages": [{"id": copy.id, "threadId": copy.thread_id} for copy in listed],
            "resultSizeEstimate": len(listed),
        })


@dataclass
class FakeGmail:
    """Fake Gmail API bound to an httpx MockTransport."""

    inbox: list[FakeGmailMessage] = field(default_factory=list)
    requests: list[httpx.Request] = field(default_factory=list)
    list_failures: list[int] = field(default_factory=list)
    read_failures: dict[str, int] = field(default_factory=dict)
    list_count: int = 0
    sent: list[str] = field(default_factory=list)
    sent_mail: FakeSentMail = field(default_factory=FakeSentMail)
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
            self.sent_mail.accept(self.sent[-1], f"sent-{len(self.sent)}", thread_id)
            return httpx.Response(200, json={"id": f"sent-{len(self.sent)}", "threadId": thread_id})
        sent_mail_answer = self.sent_mail.answer(request)
        if sent_mail_answer is not None:
            return sent_mail_answer
        if path == "/gmail/v1/users/me/messages":
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
