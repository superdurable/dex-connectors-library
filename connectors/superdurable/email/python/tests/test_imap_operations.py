# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""IMAP read and mutation paths mirror the Go mailtest scenarios."""

from typing import Any, cast

import dex
import pytest
from dex_connectors.sdk import Call, CallID, ConnectionRef, IdempotencyKey, OperationRef

from dex_connectors.superdurable_email import (
    GetMessageInput,
    MessageReference,
    MoveMessageInput,
    SearchMessagesInput,
    SetFlagsInput,
)

from .test_send_message import Context, make_client

_MESSAGE = (
    b"From: Jane <jane@example.com>\r\n"
    b"To: agent@example.com\r\n"
    b"Subject: Test message\r\n"
    b"Message-ID: <original@example.com>\r\n"
    b"Date: Tue, 03 Nov 2026 18:00:00 +0000\r\n"
    b"Content-Type: text/plain; charset=utf-8\r\n"
    b"\r\nHello from IMAP\r\n"
)


class IMAPServer:
    """Supply one UID and record read-only versus write commands."""

    capabilities: tuple[str, ...] = ("IMAP4REV1", "MOVE", "UIDPLUS")

    def __init__(self) -> None:
        self.commands: list[tuple[str, tuple[str | bytes, ...]]] = []
        self.mailbox = "INBOX"
        self.seen = False
        self.has_source = True

    def select(self, mailbox: str, readonly: bool) -> tuple[str, list[bytes]]:
        """Select one mailbox with stable UIDVALIDITY."""
        self.mailbox = mailbox
        self.commands.append(("EXAMINE" if readonly else "SELECT", (mailbox,)))
        return "OK", [b"1"]

    def response(self, name: str) -> tuple[str, list[bytes]]:
        """Return UIDVALIDITY for selected mailbox."""
        return name, [b"42"]

    def uid(self, command: str, *arguments: str | bytes) -> tuple[str, list[Any]]:
        """Return SEARCH, FETCH, MOVE, and STORE responses."""
        self.commands.append((command, arguments))
        if command == "SEARCH":
            return "OK", [b"97"]
        if command == "FETCH":
            if not self.has_source:
                return "OK", [None]
            flags = b"\\Seen" if self.seen else b""
            if "BODY.PEEK[TEXT]" in str(arguments[1]):
                return "OK", [(b"1 (UID 97 BODY[TEXT] {17}", b"Hello from IMAP\r\n")]
            if "BODY.PEEK[HEADER]" in str(arguments[1]):
                headers = _MESSAGE.split(b"\r\n\r\n", 1)[0] + b"\r\n\r\n"
                metadata = (
                    b"1 (UID 97 FLAGS (" + flags + b') INTERNALDATE "03-Nov-2026 18:01:00 +0000" '
                    b'RFC822.SIZE 222 BODYSTRUCTURE ("TEXT" "PLAIN" ("CHARSET" "utf-8") '
                    b'NIL NIL "7BIT" 17 1 NIL NIL NIL NIL) BODY[HEADER] {' + str(len(headers)).encode() + b"}"
                )
                return "OK", [(metadata, headers)]
            return "OK", [b"1 (UID 97 FLAGS (" + flags + b"))"]
        if command == "STORE":
            self.seen = "+FLAGS" in str(arguments[1])
            return "OK", [b"ok"]
        if command == "MOVE":
            self.has_source = False
            return "OK", [b"[COPYUID 42 97 98]"]
        if command == "COPY":
            return "OK", [b"[COPYUID 42 97 98]"]
        if command == "EXPUNGE":
            self.has_source = False
            return "OK", [b"expunged"]
        raise AssertionError(command)

    def logout(self) -> tuple[str, list[bytes]]:
        """Close the fake session."""
        return "BYE", [b"done"]


def make_call(operation: str) -> Call:
    """Use one stable logical email connection."""
    identifier = CallID("00000000-0000-4000-8000-000000000001")
    return Call(
        identifier,
        ConnectionRef("email", "mail"),
        OperationRef("email", operation),
        IdempotencyKey(identifier),
        context=cast(dex.AsyncContext, Context()),
    )


@pytest.mark.asyncio
async def test_search_returns_newest_summary_without_marking_seen(monkeypatch: pytest.MonkeyPatch) -> None:
    """Search reads UID envelopes and preserves unread state."""
    client = make_client()
    server = IMAPServer()
    monkeypatch.setattr(client, "open_imap", lambda *_: server)
    attempt = await client.search_messages().invoke(make_call("searchMessages"), SearchMessagesInput())
    assert attempt.branch == "searched"
    assert attempt.value is not None and attempt.value.total_matched == 1
    assert attempt.value.messages[0].message_id == "original@example.com"
    assert attempt.value.messages[0].is_seen is False
    assert any(
        "BODY.PEEK[HEADER]" in str(arguments[1])
        for command, arguments in server.commands
        if command == "FETCH"
    )


@pytest.mark.asyncio
async def test_search_uses_utf8_charset_for_non_ascii_subject(monkeypatch: pytest.MonkeyPatch) -> None:
    """A non-ASCII filter is sent as UTF-8 rather than failing in imaplib's ASCII encoder."""
    client = make_client()
    server = IMAPServer()
    monkeypatch.setattr(client, "open_imap", lambda *_: server)
    attempt = await client.search_messages().invoke(
        make_call("searchMessages"), SearchMessagesInput(subject_contains="Rückzahlung")
    )
    assert attempt.branch == "searched"
    search_arguments = next(arguments for command, arguments in server.commands if command == "SEARCH")
    assert search_arguments[:2] == ("CHARSET", "UTF-8")
    assert any(
        isinstance(argument, bytes) and b"R\xc3\xbcckzahlung" in argument for argument in search_arguments
    )


@pytest.mark.asyncio
async def test_get_message_rejects_changed_uid_validity(monkeypatch: pytest.MonkeyPatch) -> None:
    """A stale UIDVALIDITY selects notFound before FETCH."""
    client = make_client()
    server = IMAPServer()
    monkeypatch.setattr(client, "open_imap", lambda *_: server)
    attempt = await client.get_message().invoke(
        make_call("getMessage"), GetMessageInput(MessageReference("INBOX", 41, 97))
    )
    assert attempt.branch == "notFound"
    assert not any(command == "FETCH" for command, _ in server.commands)


@pytest.mark.asyncio
async def test_set_flags_writes_only_missing_absolute_value(monkeypatch: pytest.MonkeyPatch) -> None:
    """A repeated flag update is read-only after the first write."""
    client = make_client()
    server = IMAPServer()
    monkeypatch.setattr(client, "open_imap", lambda *_: server)
    input = SetFlagsInput(MessageReference("INBOX", 42, 97), is_seen=True)
    first = await client.set_flags().invoke(make_call("setFlags"), input)
    second = await client.set_flags().invoke(make_call("setFlags"), input)
    assert first.branch == second.branch == "updated"
    assert second.value is not None and second.value.was_already_applied
    assert sum(command == "STORE" for command, _ in server.commands) == 1


@pytest.mark.asyncio
async def test_move_uses_uid_move_and_reports_copy_uid(monkeypatch: pytest.MonkeyPatch) -> None:
    """MOVE returns the destination UID and the exact Message-ID receipt."""
    client = make_client()
    server = IMAPServer()
    monkeypatch.setattr(client, "open_imap", lambda *_: server)
    attempt = await client.move_message().invoke(
        make_call("moveMessage"),
        MoveMessageInput(MessageReference("INBOX", 42, 97), "Archive", "original@example.com"),
    )
    assert attempt.branch == "moved"
    assert attempt.value is not None and attempt.value.destination.uid == 98
    assert attempt.receipt is not None and attempt.receipt.provider_object_id == "original@example.com"


@pytest.mark.asyncio
async def test_move_falls_back_to_uidplus_without_plain_expunge(monkeypatch: pytest.MonkeyPatch) -> None:
    """Without MOVE, COPY, STORE, and UID EXPUNGE affect only the chosen UID."""
    client = make_client()
    server = IMAPServer()
    server.capabilities = ("IMAP4REV1", "UIDPLUS")
    monkeypatch.setattr(client, "open_imap", lambda *_: server)
    attempt = await client.move_message().invoke(
        make_call("moveMessage"),
        MoveMessageInput(MessageReference("INBOX", 42, 97), "Archive", "original@example.com"),
    )
    assert attempt.branch == "moved"
    assert [command for command, _ in server.commands if command in ("COPY", "STORE", "EXPUNGE")] == [
        "COPY",
        "STORE",
        "EXPUNGE",
    ]
    assert ("EXPUNGE", ("97",)) in server.commands


@pytest.mark.asyncio
async def test_move_lost_reply_finds_already_moved_message(monkeypatch: pytest.MonkeyPatch) -> None:
    """A lost MOVE answer retries and finds the exact Message-ID in Archive."""
    import imaplib

    class RecoveringIMAP(IMAPServer):
        def __init__(self) -> None:
            super().__init__()
            self.destination_present = False

        def uid(self, command: str, *arguments: str | bytes) -> tuple[str, list[Any]]:
            if command == "MOVE":
                self.has_source = False
                self.destination_present = True
                raise imaplib.IMAP4.abort("lost after move")
            if command == "SEARCH" and self.mailbox == "Archive":
                return "OK", [b"98"]
            if command == "FETCH" and self.mailbox == "Archive" and self.destination_present:
                headers = _MESSAGE.split(b"\r\n\r\n", 1)[0] + b"\r\n\r\n"
                metadata = (
                    b'1 (UID 98 FLAGS () INTERNALDATE "03-Nov-2026 18:01:00 +0000" '
                    b'RFC822.SIZE 222 BODYSTRUCTURE ("TEXT" "PLAIN" ("CHARSET" "utf-8") '
                    b'NIL NIL "7BIT" 17 1 NIL NIL NIL NIL) BODY[HEADER] {' + str(len(headers)).encode() + b"}"
                )
                return "OK", [(metadata, headers)]
            return super().uid(command, *arguments)

    client = make_client()
    server = RecoveringIMAP()
    monkeypatch.setattr(client, "open_imap", lambda *_: server)
    input_value = MoveMessageInput(MessageReference("INBOX", 42, 97), "Archive", "original@example.com")
    first = await client.move_message().invoke(make_call("moveMessage"), input_value)
    second = await client.move_message().invoke(make_call("moveMessage"), input_value)
    assert first.kind == "retry"
    assert second.branch == "moved"
    assert second.value is not None and second.value.was_already_moved
    assert second.value.destination.uid == 98
