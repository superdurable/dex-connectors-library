# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Reply threading and MIME text extraction mirror the Go mailtest cases."""

import imaplib
from datetime import UTC, datetime
from typing import cast

import pytest

from dex_connectors.superdurable_email import (
    EmailAddress,
    Message,
    MessageReference,
    ReplyToMessageInput,
    operations,
)

from .test_send_message import Context, SMTPServer, make_client


@pytest.mark.asyncio
async def test_reply_all_threads_and_omits_own_sender(monkeypatch: pytest.MonkeyPatch) -> None:
    """A reply honors Reply-To, excludes the sender, and preserves References."""
    client = make_client()
    server = SMTPServer()
    monkeypatch.setattr(client, "open_smtp", lambda *_: server)
    source = Message(
        reference=MessageReference("INBOX", 42, 97),
        message_id="original@example.com",
        from_=EmailAddress("Jane", "jane@example.com"),
        reply_to=(EmailAddress("Support", "reply@example.com"),),
        to=(EmailAddress("", "agent@example.com"), EmailAddress("", "other@example.com")),
        cc=(EmailAddress("", "copy@example.com"),),
        subject="Refund",
        received_at=datetime.now(UTC),
        references=("earlier@example.com",),
        text_source="none",
    )
    monkeypatch.setattr(operations, "_read_message", lambda *_: source)
    from typing import cast

    import dex
    from dex_connectors.sdk import Call, CallID, ConnectionRef, IdempotencyKey, OperationRef

    identifier = CallID("00000000-0000-4000-8000-000000000003")
    context = Context()
    call = Call(
        identifier,
        ConnectionRef("email", "mail"),
        OperationRef("email", "replyToMessage"),
        IdempotencyKey(identifier),
        context=cast(dex.AsyncContext, context),
    )
    attempt = await client.reply_to_message().invoke(
        call, ReplyToMessageInput(MessageReference("INBOX", 42, 97), "Thanks for contacting us", True)
    )
    assert attempt.branch == "sent"
    assert attempt.value is not None
    assert attempt.value.recipients == ("reply@example.com", "other@example.com", "copy@example.com")
    assert attempt.value.in_reply_to == "original@example.com"
    assert attempt.value.references == ("earlier@example.com", "original@example.com")
    assert b"In-Reply-To: <original@example.com>" in server.content
    assert b"agent@example.com" in server.content  # The sender remains in From only.


def test_html_fallback_extracts_visible_text_and_attachment_names(monkeypatch: pytest.MonkeyPatch) -> None:
    """GetMessage uses visible HTML when plain text is absent and lists attachments."""
    body = (
        b"From: Jane <jane@example.com>\r\n"
        b"Subject: <Invoice>\r\n"
        b"Date: Tue, 03 Nov 2026 18:00:00 +0000\r\n"
        b"MIME-Version: 1.0\r\n"
        b"Content-Type: multipart/mixed; boundary=outer\r\n\r\n"
        b"--outer\r\nContent-Type: text/html; charset=utf-8\r\n\r\n"
        b"<html><head><title>hidden</title></head><body><p>Hello <b>Jane</b></p>"
        b"<script>bad()</script><div>Invoice ready</div></body></html>\r\n"
        b"--outer\r\nContent-Type: application/pdf\r\n"
        b"Content-Disposition: attachment; filename=invoice.pdf\r\n\r\nPDF\r\n--outer--\r\n"
    )
    metadata = (
        b'1 FETCH (UID 97 FLAGS () INTERNALDATE "03-Nov-2026 18:01:00 +0000" '
        b"RFC822.SIZE "
        + str(len(body)).encode()
        + b' BODYSTRUCTURE (("TEXT" "HTML" ("CHARSET" "utf-8") NIL NIL "7BIT" 100 1 NIL NIL NIL NIL) '
        b'("APPLICATION" "PDF" ("NAME" "invoice.pdf") NIL NIL "7BIT" 3 NIL '
        b'("ATTACHMENT" ("FILENAME" "invoice.pdf")) NIL NIL) "MIXED" '
        b'("BOUNDARY" "outer") NIL NIL NIL))'
    )
    html_text = body.split(b"<html>", 1)[1].split(b"</html>", 1)[0]
    monkeypatch.setattr(operations, "_fetch_text", lambda *_: b"<html>" + html_text + b"</html>")
    headers = body.split(b"\r\n\r\n", 1)[0] + b"\r\n\r\n"
    message = operations._parse_message(
        MessageReference("INBOX", 42, 97), metadata, headers, True, cast(imaplib.IMAP4, object())
    )
    assert message.text_source == "html"
    assert "Hello Jane" in message.text
    assert "Invoice ready" in message.text
    assert "hidden" not in message.text and "bad()" not in message.text
    assert message.attachments[0].file_name == "invoice.pdf"
    assert message.attachments[0].part == "2"
    assert message.attachments[0].encoded_size_bytes == 3
    assert message.has_attachments
