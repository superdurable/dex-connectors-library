# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Email operation invocation and Dex attempt classification."""

import asyncio
import base64
import email
import imaplib
import quopri
import re
import smtplib
from dataclasses import replace
from datetime import UTC, datetime
from email.headerregistry import Address
from email.message import EmailMessage
from email.message import Message as ParsedMessage
from email.policy import SMTP, default
from email.utils import getaddresses, parsedate_to_datetime
from html.parser import HTMLParser
from typing import Any

from dex_connectors.sdk import (
    Call,
    CallID,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    QueryAttempt,
    QueryDefinition,
    Receipt,
)

from ._generated.definitions import (
    GET_MESSAGE_DEFINITION,
    MOVE_MESSAGE_DEFINITION,
    REPLY_TO_MESSAGE_DEFINITION,
    SEARCH_MESSAGES_DEFINITION,
    SEND_MESSAGE_DEFINITION,
    SET_FLAGS_DEFINITION,
)
from .client import Client, MailFailure, classify_mail_error, is_bare_email_address
from .imap_bodystructure import _BodyPart, parse_bodystructure
from .models import (
    Attachment,
    EmailAddress,
    GetMessageInput,
    Message,
    MessageFlags,
    MessageReference,
    MessageSummary,
    MovedMessage,
    MoveMessageInput,
    ReplyToMessageInput,
    SearchMessagesInput,
    SearchMessagesOutput,
    SendMessageInput,
    SentMessage,
    SetFlagsInput,
)

_MAX_TEXT = 512 << 10
_MAX_READ_TEXT = 64 << 10
_MAX_ENCODED_TEXT = 1 << 20


class _VisibleHTMLText(HTMLParser):
    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self.runs: list[str] = []
        self.skipped_depth = 0

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        if tag in ("script", "style", "head", "title"):
            self.skipped_depth += 1
        elif tag in ("br", "p", "div", "li", "tr", "h1", "h2", "h3", "h4", "h5", "h6", "blockquote", "table"):
            self.runs.append("\n")

    def handle_endtag(self, tag: str) -> None:
        if tag in ("script", "style", "head", "title"):
            self.skipped_depth = max(0, self.skipped_depth - 1)
        elif tag in ("p", "div", "li", "tr", "h1", "h2", "h3", "h4", "h5", "h6", "blockquote", "table"):
            self.runs.append("\n")

    def handle_data(self, data: str) -> None:
        if self.skipped_depth:
            return
        words = data.split()
        if not words:
            if data:
                self.runs.append(" ")
            return
        if data[0].isspace():
            self.runs.append(" ")
        self.runs.append(" ".join(words))
        if data[-1].isspace():
            self.runs.append(" ")

    def text(self) -> str:
        lines = [line.strip() for line in "".join(self.runs).split("\n")]
        result: list[str] = []
        for line in lines:
            if line or (result and result[-1]):
                result.append(line)
        return "\n".join(result).strip()


def _failure(kind: FailureKind, operation: str, message: str) -> Failure:
    return Failure(kind, "email", operation, message)


def _receipt(object_id: str = "") -> Receipt:
    return Receipt(provider_object_id=object_id)


def _query_failure(error: MailFailure, missing_branch: bool = False) -> QueryAttempt[Any]:
    if error.outcome == "retry":
        return QueryAttempt.retry(error.failure)
    branch = (
        ("notFound" if missing_branch else "providerRejected")
        if error.outcome == "not_found"
        else ("invalidResponse" if error.outcome == "invalid" else "providerRejected")
    )
    return QueryAttempt.branch_result(branch, None, _receipt(), error.failure)


def _mutation_failure(error: MailFailure, missing_branch: bool = False) -> MutationAttempt[Any]:
    if error.outcome in ("retry", "invalid"):
        return MutationAttempt.retry(error.failure)
    branch = "notFound" if missing_branch and error.outcome == "not_found" else "providerRejected"
    return MutationAttempt.branch_result(branch, None, _receipt(), error.failure)


def _valid_mailbox(value: str) -> bool:
    return bool(
        value
        and len(value.encode()) <= 255
        and value.strip() == value
        and not any(
            ord(character) < 32 or ord(character) == 127 or character in "*%\u2028\u2029"
            for character in value
        )
    )


def _valid_reference(reference: MessageReference) -> bool:
    return (
        _valid_mailbox(reference.mailbox)
        and 0 < reference.uid_validity <= 0xFFFFFFFF
        and 0 < reference.uid <= 0xFFFFFFFF
    )


def _imap_close(session: imaplib.IMAP4) -> None:
    try:
        session.logout()
    except Exception:
        # LOGOUT cannot change the outcome of a completed operation.
        pass


def _imap_select(
    session: imaplib.IMAP4, mailbox: str, read_only: bool, expected_uid_validity: int, operation: str
) -> int:
    try:
        status, data = session.select(mailbox, readonly=read_only)
        if status != "OK":
            raise imaplib.IMAP4.error(f"{status} {data!r}")
        _, response = session.response("UIDVALIDITY")
        if not response or not response[0] or not response[0].isdigit():
            raise MailFailure(
                "invalid",
                _failure(
                    FailureKind.PROTOCOL,
                    operation,
                    "the IMAP server selected the mailbox without reporting UIDVALIDITY",
                ),
            )
        uid_validity = int(response[0])
        if expected_uid_validity and expected_uid_validity != uid_validity:
            raise MailFailure(
                "not_found",
                _failure(
                    FailureKind.NOT_FOUND,
                    operation,
                    "the mailbox's UIDVALIDITY changed, so the UID no longer identifies the message",
                ),
            )
        return uid_validity
    except MailFailure:
        raise
    except Exception as error:
        raise classify_mail_error(error, operation, "IMAP", "SELECT") from error


def _fetch(session: imaplib.IMAP4, uid: int, operation: str) -> tuple[bytes, bytes] | None:
    try:
        status, data = session.uid(
            "FETCH", str(uid), "(UID FLAGS INTERNALDATE RFC822.SIZE BODYSTRUCTURE BODY.PEEK[HEADER])"
        )
        if status != "OK":
            raise imaplib.IMAP4.error(f"{status} {data!r}")
        for item in data:
            if isinstance(item, tuple) and len(item) == 2 and isinstance(item[1], bytes):
                return item
        return None
    except Exception as error:
        raise classify_mail_error(error, operation, "IMAP", "FETCH") from error


def _fetch_text(session: imaplib.IMAP4, uid: int, part: _BodyPart, operation: str) -> bytes:
    section = ".".join(str(index) for index in part.path) if part.path else "TEXT"
    try:
        status, data = session.uid("FETCH", str(uid), f"(UID BODY.PEEK[{section}]<0.{_MAX_ENCODED_TEXT}>)")
        if status != "OK":
            raise imaplib.IMAP4.error(f"{status} {data!r}")
        for item in data:
            if isinstance(item, tuple) and len(item) == 2 and isinstance(item[1], bytes):
                return item[1]
        raise MailFailure(
            "not_found",
            _failure(FailureKind.NOT_FOUND, operation, "the message was removed while it was read"),
        )
    except MailFailure:
        raise
    except Exception as error:
        raise classify_mail_error(error, operation, "IMAP", "FETCH") from error


def _parse_date(value: str | None) -> datetime:
    if not value:
        return datetime.min.replace(tzinfo=UTC)
    try:
        parsed = parsedate_to_datetime(value)
        return parsed.astimezone(UTC) if parsed.tzinfo else parsed.replace(tzinfo=UTC)
    except (ValueError, TypeError, OverflowError):
        return datetime.min.replace(tzinfo=UTC)


def _addresses(value: str | None) -> tuple[EmailAddress, ...]:
    if not value:
        return ()
    return tuple(EmailAddress(name[:512], address) for name, address in getaddresses([value])[:50])


def _message_id(value: str | None) -> str:
    if not value:
        return ""
    match = re.search(r"<([^<>\s]+@[^<>\s]+)>", value)
    return match.group(1) if match else ""


def _decode_text_part(encoded: bytes, part: _BodyPart) -> tuple[str, bool]:
    encoded = encoded[:_MAX_ENCODED_TEXT]
    if part.transfer_encoding == "base64":
        decoded_bytes = base64.b64decode(encoded, validate=False)
    elif part.transfer_encoding == "quoted-printable":
        decoded_bytes = quopri.decodestring(encoded)
    else:
        decoded_bytes = encoded
    try:
        text = decoded_bytes.decode(part.charset or "us-ascii", errors="replace")
    except LookupError:
        text = decoded_bytes.decode("utf-8", errors="replace")
    if part.media_type == "text/html":
        extractor = _VisibleHTMLText()
        extractor.feed(text)
        text = extractor.text()
    text = text.replace("\r\n", "\n")
    rendered = text.encode("utf-8")
    is_truncated = part.encoded_size > len(encoded) or len(rendered) > _MAX_READ_TEXT
    return rendered[:_MAX_READ_TEXT].decode("utf-8", errors="ignore"), is_truncated


def _parse_message(
    reference: MessageReference,
    metadata: bytes,
    headers: bytes,
    include_text: bool,
    session: imaplib.IMAP4 | None = None,
    operation: str = "getMessage",
) -> Message:
    parsed = email.message_from_bytes(headers, policy=default)
    if not isinstance(parsed, ParsedMessage):
        raise ValueError("invalid RFC 5322 headers")
    uid_match = re.search(rb"\bUID (\d+)", metadata)
    flags_match = re.search(rb"FLAGS \(([^)]*)\)", metadata)
    size_match = re.search(rb"RFC822.SIZE (\d+)", metadata)
    date_match = re.search(rb'INTERNALDATE "([^"]+)"', metadata)
    if (
        not uid_match
        or int(uid_match.group(1)) != reference.uid
        or not flags_match
        or not size_match
        or not date_match
    ):
        raise ValueError("missing or mismatched IMAP FETCH metadata")
    parts = parse_bodystructure(metadata)
    flags = flags_match.group(1).upper().split()
    received_at = _parse_date(date_match.group(1).decode("ascii", errors="replace"))
    if received_at == datetime.min.replace(tzinfo=UTC):
        raise ValueError("invalid IMAP internal date")
    from_addresses = _addresses(parsed.get("From"))
    reply_to = _addresses(parsed.get("Reply-To")) or from_addresses
    attachment_parts = tuple(part for part in parts if part.is_attachment)
    attachments = tuple(
        Attachment(
            ".".join(str(index) for index in part.path),
            part.filename.encode()[:255].decode(errors="ignore"),
            part.media_type,
            part.encoded_size,
            part.is_inline,
        )
        for part in attachment_parts[:50]
    )
    text_part = next(
        (part for part in parts if part.media_type == "text/plain" and not part.is_attachment), None
    )
    if text_part is None:
        text_part = next(
            (part for part in parts if part.media_type == "text/html" and not part.is_attachment), None
        )
    text_source = "none" if text_part is None else "html" if text_part.media_type == "text/html" else "plain"
    text = ""
    is_truncated = False
    if include_text and text_part is not None:
        if session is None:
            raise ValueError("an IMAP session is required to read the text part")
        text, is_truncated = _decode_text_part(
            _fetch_text(session, reference.uid, text_part, operation), text_part
        )
    references = tuple(re.findall(r"<([^<>\s]+@[^<>\s]+)>", parsed.get("References", ""))[-20:])
    return Message(
        reference=reference,
        message_id=_message_id(parsed.get("Message-ID")),
        in_reply_to=_message_id(parsed.get("In-Reply-To")),
        from_=from_addresses[0] if from_addresses else EmailAddress(),
        reply_to=reply_to,
        to=_addresses(parsed.get("To")),
        cc=_addresses(parsed.get("Cc")),
        subject=str(parsed.get("Subject", "")).encode()[:998].decode(errors="ignore"),
        sent_at=_parse_date(parsed.get("Date")),
        received_at=received_at,
        is_seen=b"\\SEEN" in flags,
        is_flagged=b"\\FLAGGED" in flags,
        is_answered=b"\\ANSWERED" in flags,
        size_bytes=int(size_match.group(1)),
        has_attachments=bool(attachment_parts),
        references=references,
        text=text,
        text_source=text_source,
        is_text_truncated=is_truncated,
        attachments=attachments,
        has_more_attachments=len(attachment_parts) > 50,
    )


def _read_message(
    client: Client, credentials: Any, reference: MessageReference, operation: str, include_text: bool
) -> Message:
    session = client.open_imap(credentials, operation)
    try:
        _imap_select(session, reference.mailbox, True, reference.uid_validity, operation)
        fetched = _fetch(session, reference.uid, operation)
        if fetched is None:
            raise MailFailure(
                "not_found",
                _failure(FailureKind.NOT_FOUND, operation, "no message in the mailbox has the UID"),
            )
        try:
            return _parse_message(reference, fetched[0], fetched[1], include_text, session, operation)
        except (ValueError, UnicodeError) as error:
            raise MailFailure(
                "invalid",
                _failure(FailureKind.PROTOCOL, operation, "the IMAP server returned an invalid message"),
            ) from error
    finally:
        _imap_close(session)


class SearchMessagesOperation:
    """Search one IMAP mailbox without changing message flags."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated search definition."""
        return SEARCH_MESSAGES_DEFINITION

    async def invoke(self, call: Call, input: SearchMessagesInput) -> QueryAttempt[SearchMessagesOutput]:
        """Validate, search, and summarize newest matching UIDs."""
        operation = "searchMessages"
        mailbox = input.mailbox or "INBOX"
        limit = input.limit or 10
        if not _valid_mailbox(mailbox) or limit < 1 or limit > 50:
            return QueryAttempt.branch_result(
                "defect",
                SearchMessagesOutput(),
                _receipt(),
                _failure(FailureKind.VALIDATION, operation, "invalid mailbox or limit"),
            )
        for value in (input.from_, input.to, input.subject_contains):
            if value and (
                value.strip() != value
                or len(value.encode()) > 256
                or any(ord(c) < 32 or ord(c) == 127 for c in value)
            ):
                return QueryAttempt.branch_result(
                    "defect",
                    SearchMessagesOutput(),
                    _receipt(),
                    _failure(FailureKind.VALIDATION, operation, "invalid search text"),
                )
        try:
            since = datetime.strptime(input.since_date, "%Y-%m-%d") if input.since_date else None
            before = datetime.strptime(input.before_date, "%Y-%m-%d") if input.before_date else None
            if (
                (since and before and before <= since)
                or (input.older_than_uid and (not input.uid_validity or input.older_than_uid <= 1))
                or (input.uid_validity and not input.older_than_uid)
            ):
                raise ValueError("invalid search page")
        except ValueError:
            return QueryAttempt.branch_result(
                "defect",
                SearchMessagesOutput(),
                _receipt(),
                _failure(FailureKind.VALIDATION, operation, "invalid search date or page"),
            )
        try:
            credentials = await self.client.resolve_credentials(call, operation)
            return await asyncio.to_thread(self._search, credentials, input, mailbox, limit)
        except MailFailure as error:
            return _query_failure(error)

    def _search(
        self, credentials: Any, input: SearchMessagesInput, mailbox: str, limit: int
    ) -> QueryAttempt[SearchMessagesOutput]:
        operation = "searchMessages"
        session = self.client.open_imap(credentials, operation)
        try:
            validity = _imap_select(session, mailbox, True, 0, operation)
            if input.uid_validity and input.uid_validity != validity:
                return QueryAttempt.branch_result(
                    "providerRejected",
                    SearchMessagesOutput(),
                    _receipt(),
                    _failure(FailureKind.CONFLICT, operation, "the mailbox's UIDVALIDITY changed"),
                )
            criteria: list[str] = []
            for header, value in (
                ("From", input.from_),
                ("To", input.to),
                ("Subject", input.subject_contains),
            ):
                if value:
                    criteria.extend(
                        ("HEADER", header, '"' + value.replace("\\", "\\\\").replace('"', '\\"') + '"')
                    )
            if input.since_date:
                criteria.extend(
                    ("SINCE", datetime.strptime(input.since_date, "%Y-%m-%d").strftime("%d-%b-%Y"))
                )
            if input.before_date:
                criteria.extend(
                    ("BEFORE", datetime.strptime(input.before_date, "%Y-%m-%d").strftime("%d-%b-%Y"))
                )
            if input.is_unseen:
                criteria.append("UNSEEN")
            if input.older_than_uid:
                criteria.extend(("UID", f"1:{input.older_than_uid - 1}"))
            try:
                if any(ord(character) > 127 for value in criteria for character in value):
                    search_arguments = ("CHARSET", "UTF-8", *(value.encode("utf-8") for value in criteria))
                else:
                    search_arguments = tuple(criteria or ["ALL"])
                status, data = session.uid("SEARCH", *search_arguments)  # type: ignore[arg-type]
                if status != "OK":
                    raise imaplib.IMAP4.error(f"{status} {data!r}")
                if not data or data[0] is None:
                    raise ValueError("SEARCH returned no UID set")
                uids = sorted({int(uid) for uid in data[0].split()}, reverse=True)
            except Exception as error:
                raise classify_mail_error(error, operation, "IMAP", "SEARCH") from error
            messages: list[MessageSummary] = []
            for uid in uids[:limit]:
                fetched = _fetch(session, uid, operation)
                if fetched is None:
                    continue
                try:
                    message = _parse_message(
                        MessageReference(mailbox, validity, uid), fetched[0], fetched[1], False
                    )
                except (ValueError, UnicodeError) as error:
                    raise MailFailure(
                        "invalid",
                        _failure(
                            FailureKind.PROTOCOL, operation, "the IMAP server returned an invalid message"
                        ),
                    ) from error
                messages.append(
                    MessageSummary(
                        reference=message.reference,
                        message_id=message.message_id,
                        in_reply_to=message.in_reply_to,
                        from_=message.from_,
                        reply_to=message.reply_to,
                        to=message.to,
                        cc=message.cc,
                        subject=message.subject,
                        sent_at=message.sent_at,
                        received_at=message.received_at,
                        is_seen=message.is_seen,
                        is_flagged=message.is_flagged,
                        is_answered=message.is_answered,
                        size_bytes=message.size_bytes,
                        has_attachments=message.has_attachments,
                    )
                )
            has_more = len(uids) > limit
            return QueryAttempt.branch_result(
                "searched",
                SearchMessagesOutput(
                    mailbox,
                    validity,
                    tuple(messages),
                    len(uids),
                    has_more,
                    uids[limit - 1] if has_more else 0,
                ),
                _receipt(),
            )
        finally:
            _imap_close(session)


class GetMessageOperation:
    """Read one IMAP message without setting Seen."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated get definition."""
        return GET_MESSAGE_DEFINITION

    async def invoke(self, call: Call, input: GetMessageInput) -> QueryAttempt[Message]:
        """Fetch the referenced UID if UIDVALIDITY still matches."""
        operation = "getMessage"
        if not _valid_reference(input.message):
            return QueryAttempt.branch_result(
                "defect",
                Message(text_source="none"),
                _receipt(),
                _failure(FailureKind.VALIDATION, operation, "invalid message reference"),
            )
        try:
            credentials = await self.client.resolve_credentials(call, operation)
            message = await asyncio.to_thread(
                _read_message, self.client, credentials, input.message, operation, True
            )
            return QueryAttempt.branch_result("found", message, _receipt(message.message_id))
        except MailFailure as error:
            return _query_failure(error, True)


def _message_identifier(key: IdempotencyKey, sender: str) -> str:
    if not key or len(key) > 128 or not re.fullmatch(r"[A-Za-z0-9-]+", key):
        raise ValueError("the idempotency key cannot form a Message-ID")
    return "dex-" + key.lower() + "@" + sender.rsplit("@", 1)[1].lower()


def _validate_text(text: str) -> bool:
    return bool(
        text.strip()
        and len(text.encode()) <= _MAX_TEXT
        and not any(
            (ord(character) < 32 and character not in "\r\n\t")
            or ord(character) == 127
            or character in "\u2028\u2029"
            for character in text
        )
    )


def _validate_recipients(recipients: tuple[str, ...]) -> bool:
    return len(recipients) <= 50 and all(is_bare_email_address(value) for value in recipients)


def _unique_recipients(*groups: tuple[str, ...]) -> tuple[str, ...]:
    found: set[str] = set()
    result: list[str] = []
    for group in groups:
        for address in group:
            if address.lower() not in found:
                found.add(address.lower())
                result.append(address)
    return tuple(result)


def _render_message(
    sender: str,
    sender_name: str,
    to: tuple[str, ...],
    cc: tuple[str, ...],
    reply_to: str,
    subject: str,
    text: str,
    identifier: str,
    in_reply_to: str = "",
    references: tuple[str, ...] = (),
) -> tuple[bytes, datetime]:
    message = EmailMessage(policy=SMTP)
    message["From"] = Address(display_name=sender_name, addr_spec=sender)
    message["To"] = ", ".join(to)
    if cc:
        message["Cc"] = ", ".join(cc)
    if reply_to:
        message["Reply-To"] = reply_to
    message["Subject"] = subject
    message["Message-ID"] = f"<{identifier}>"
    if in_reply_to:
        message["In-Reply-To"] = f"<{in_reply_to}>"
    if references:
        message["References"] = " ".join(f"<{reference}>" for reference in references)
    now = datetime.now(UTC).replace(microsecond=0)
    message["Date"] = email.utils.format_datetime(now)
    message.set_content(text, subtype="plain", charset="utf-8", cte="quoted-printable")
    return message.as_bytes(), now


def _smtp_submit(
    client: Client, credentials: Any, sender: str, recipients: tuple[str, ...], content: bytes, operation: str
) -> None:
    session = client.open_smtp(credentials, operation)
    try:
        size_limit = session.esmtp_features.get("size", "")
        if size_limit.isdigit() and int(size_limit) > 0 and len(content) > int(size_limit):
            raise MailFailure(
                "rejected",
                _failure(
                    FailureKind.PROVIDER_REJECTION,
                    operation,
                    "the message is larger than the SMTP server's SIZE limit",
                ),
            )
        try:
            mail_options = [f"SIZE={len(content)}"] if "size" in session.esmtp_features else []
            code, _ = session.mail(sender, options=mail_options)
            if code != 250:
                raise smtplib.SMTPSenderRefused(code, b"", sender)
            for recipient in recipients:
                code, _ = session.rcpt(recipient)
                if code not in (250, 251):
                    raise smtplib.SMTPRecipientsRefused({recipient: (code, b"")})
            session.putcmd("DATA")
            code, _ = session.getreply()
            if code != 354:
                raise smtplib.SMTPDataError(code, b"")
            # The final dot is sent in the same write. A lost answer after this point is uncertain.
            normalized = content.replace(b"\r\n", b"\n").replace(b"\r", b"\n").replace(b"\n", b"\r\n")
            stuffed = b"\r\n".join(
                b"." + line if line.startswith(b".") else line for line in normalized.split(b"\r\n")
            )
            try:
                session.send(stuffed.rstrip(b"\r\n") + b"\r\n")
            except Exception as error:
                raise MailFailure(
                    "retry",
                    _failure(
                        FailureKind.TRANSPORT,
                        operation,
                        "the SMTP connection was lost before the message was complete",
                    ),
                ) from error
            try:
                session.send(b".\r\n")
                code, _ = session.getreply()
            except Exception as error:
                raise MailFailure(
                    "uncertain",
                    _failure(
                        FailureKind.TRANSPORT,
                        operation,
                        "the message was submitted but the SMTP server's answer did not arrive",
                    ),
                ) from error
            if code != 250:
                raise smtplib.SMTPDataError(code, b"")
        except MailFailure:
            raise
        except Exception as error:
            raise classify_mail_error(error, operation, "SMTP", "submission") from error
    finally:
        try:
            session.quit()
        except Exception:
            # QUIT is courtesy after the submission outcome has been decided.
            session.close()


async def _has_submission_claim(call: Call) -> bool:
    context = call.context
    if context is None:
        return True
    try:
        return context.has_last_heartbeat_value()
    except Exception:
        return True


async def _claim_submission(call: Call, operation: str) -> MutationAttempt[SentMessage] | None:
    context = call.context
    if context is None:
        return MutationAttempt.retry(
            _failure(
                FailureKind.AVAILABILITY,
                operation,
                "Dex did not record the submission checkpoint; nothing was submitted",
            )
        )
    try:
        await context.heartbeat({"emailSubmittedCallId": str(call.id)})
        return None
    except Exception:
        return MutationAttempt.retry(
            _failure(
                FailureKind.AVAILABILITY,
                operation,
                "Dex did not record the submission checkpoint; nothing was submitted",
            )
        )


async def _release_submission(call: Call) -> None:
    if call.context is not None:
        try:
            await call.context.heartbeat()
        except Exception:
            # A retained marker safely prevents a later retry from resubmitting.
            pass


async def _submit_attempt(
    client: Client,
    call: Call,
    credentials: Any,
    sender: str,
    recipients: tuple[str, ...],
    content: bytes,
    sent: SentMessage,
    operation: str,
) -> MutationAttempt[SentMessage]:
    claim = await _claim_submission(call, operation)
    if claim is not None:
        return claim
    try:
        await asyncio.to_thread(_smtp_submit, client, credentials, sender, recipients, content, operation)
        return MutationAttempt.branch_result("sent", sent, _receipt(sent.message_id))
    except MailFailure as error:
        if error.outcome == "uncertain":
            return MutationAttempt.uncertain(sent, error.failure, _receipt(sent.message_id))
        await _release_submission(call)
        if error.outcome == "retry":
            return MutationAttempt.retry(error.failure)
        return MutationAttempt.branch_result("providerRejected", SentMessage(), _receipt(), error.failure)


class SendMessageOperation:
    """Send one text message with a Dex heartbeat claim before SMTP DATA."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated send definition."""
        return SEND_MESSAGE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: SendMessageInput) -> IdempotencyKey:
        """Use the stable Call ID to form Message-ID."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: SendMessageInput) -> MutationAttempt[SentMessage]:
        """Validate recipients, claim one submission, and classify the SMTP answer."""
        operation = "sendMessage"
        if (
            not input.to
            or sum(map(len, (input.to, input.cc, input.bcc))) > 50
            or not all(_validate_recipients(group) for group in (input.to, input.cc, input.bcc))
            or (input.reply_to and not is_bare_email_address(input.reply_to))
            or len(input.subject.encode()) > 998
            or any(c in input.subject for c in "\r\n\u2028\u2029")
            or not _validate_text(input.text)
        ):
            return MutationAttempt.branch_result(
                "defect",
                SentMessage(),
                _receipt(),
                _failure(FailureKind.VALIDATION, operation, "invalid recipients, subject, or text"),
            )
        try:
            credentials = await self.client.resolve_credentials(call, operation)
            sender = self.client.sender(credentials, operation)
            identifier = _message_identifier(call.idempotency_key, sender)
            recipients = _unique_recipients(input.to, input.cc, input.bcc)
            content, now = _render_message(
                sender,
                self.client.config.from_name,
                input.to,
                input.cc,
                input.reply_to,
                input.subject,
                input.text,
                identifier,
            )
            sent = SentMessage(identifier, sender, recipients, input.subject, sent_at=now)
            if await _has_submission_claim(call):
                return MutationAttempt.uncertain(
                    replace(sent, sent_at=datetime.min.replace(tzinfo=UTC)),
                    _failure(
                        FailureKind.TRANSPORT, operation, "an earlier attempt may have submitted the message"
                    ),
                    _receipt(identifier),
                )
            return await _submit_attempt(
                self.client, call, credentials, sender, recipients, content, sent, operation
            )
        except MailFailure as error:
            return MutationAttempt.branch_result("defect", SentMessage(), _receipt(), error.failure)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", SentMessage(), _receipt(), _failure(FailureKind.VALIDATION, operation, str(error))
            )


class ReplyToMessageOperation:
    """Read one source message and submit a threaded SMTP reply once."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated reply definition."""
        return REPLY_TO_MESSAGE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: ReplyToMessageInput) -> IdempotencyKey:
        """Use the stable Call ID to form Message-ID."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: ReplyToMessageInput) -> MutationAttempt[SentMessage]:
        """Preserve threading and refuse to re-send a claimed submission."""
        operation = "replyToMessage"
        if (
            not _valid_reference(input.message)
            or not _validate_text(input.text)
            or not _validate_recipients(input.cc)
        ):
            return MutationAttempt.branch_result(
                "defect",
                SentMessage(),
                _receipt(),
                _failure(FailureKind.VALIDATION, operation, "invalid message reference, text, or recipients"),
            )
        try:
            credentials = await self.client.resolve_credentials(call, operation)
            sender = self.client.sender(credentials, operation)
            identifier = _message_identifier(call.idempotency_key, sender)
            if await _has_submission_claim(call):
                sent = SentMessage(identifier, sender)
                return MutationAttempt.uncertain(
                    sent,
                    _failure(
                        FailureKind.TRANSPORT, operation, "an earlier attempt may have submitted the message"
                    ),
                    _receipt(identifier),
                )
            source = await asyncio.to_thread(
                _read_message, self.client, credentials, input.message, operation, False
            )
            primary = source.reply_to or (source.from_,)
            to = _unique_recipients(
                tuple(
                    address.address
                    for address in primary
                    if is_bare_email_address(address.address) and address.address.lower() != sender.lower()
                )
            )
            if not to:
                return MutationAttempt.branch_result(
                    "invalidResponse",
                    SentMessage(),
                    _receipt(),
                    _failure(
                        FailureKind.PROTOCOL, operation, "the source message has no usable reply address"
                    ),
                )
            cc = (
                tuple(
                    address.address
                    for address in (*source.to, *source.cc)
                    if is_bare_email_address(address.address)
                )
                if input.is_reply_all
                else ()
            )
            cc = _unique_recipients(cc, input.cc)
            cc = tuple(
                address
                for address in cc
                if address.lower() != sender.lower() and address.lower() not in {item.lower() for item in to}
            )
            if len(to) + len(cc) > 50:
                return MutationAttempt.branch_result(
                    "invalidResponse",
                    SentMessage(),
                    _receipt(),
                    _failure(FailureKind.PROTOCOL, operation, "the reply has too many recipients"),
                )
            trimmed_subject = source.subject.strip()
            subject = (
                trimmed_subject if trimmed_subject.lower().startswith("re:") else "Re: " + trimmed_subject
            )
            subject = subject.encode()[:998].decode(errors="ignore")
            references = (
                (*source.references, source.message_id) if source.message_id else source.references
            )[-20:]
            content, now = _render_message(
                sender,
                self.client.config.from_name,
                to,
                cc,
                "",
                subject,
                input.text,
                identifier,
                source.message_id,
                references,
            )
            recipients = _unique_recipients(to, cc)
            sent = SentMessage(identifier, sender, recipients, subject, source.message_id, references, now)
            return await _submit_attempt(
                self.client, call, credentials, sender, recipients, content, sent, operation
            )
        except MailFailure as error:
            if error.outcome == "not_found":
                return MutationAttempt.branch_result("notFound", SentMessage(), _receipt(), error.failure)
            if error.outcome == "retry":
                return MutationAttempt.retry(error.failure)
            return MutationAttempt.branch_result(
                "invalidResponse"
                if error.outcome == "invalid"
                else "providerRejected"
                if error.outcome == "rejected"
                else "defect",
                SentMessage(),
                _receipt(),
                error.failure,
            )
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", SentMessage(), _receipt(), _failure(FailureKind.VALIDATION, operation, str(error))
            )


def _uid_command(
    session: imaplib.IMAP4, command: str, operation: str, *arguments: str
) -> tuple[str, list[Any]]:
    try:
        status, data = session.uid(command, *arguments)
        if status != "OK":
            raise imaplib.IMAP4.error(f"{status} {data!r}")
        return status, data
    except Exception as error:
        raise classify_mail_error(error, operation, "IMAP", command) from error


def _read_flags(session: imaplib.IMAP4, reference: MessageReference, operation: str) -> MessageFlags:
    _, data = _uid_command(session, "FETCH", operation, str(reference.uid), "(UID FLAGS)")
    for item in data:
        metadata = item[0] if isinstance(item, tuple) else item
        if isinstance(metadata, bytes):
            match = re.search(rb"FLAGS \(([^)]*)\)", metadata)
            if match:
                flags = match.group(1).upper().split()
                return MessageFlags(
                    reference, b"\\SEEN" in flags, b"\\FLAGGED" in flags, b"\\ANSWERED" in flags
                )
    raise MailFailure(
        "not_found", _failure(FailureKind.NOT_FOUND, operation, "no message in the mailbox has the UID")
    )


class SetFlagsOperation:
    """Set requested IMAP flags to absolute values and read them back."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated set flags definition."""
        return SET_FLAGS_DEFINITION

    def idempotency_key(self, call_id: CallID, input: SetFlagsInput) -> IdempotencyKey:
        """Use the stable Call ID for the repeatable absolute update."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: SetFlagsInput) -> MutationAttempt[MessageFlags]:
        """Write only changed flags and confirm the server retained them."""
        operation = "setFlags"
        if not _valid_reference(input.message) or all(
            value is None for value in (input.is_seen, input.is_flagged, input.is_answered)
        ):
            return MutationAttempt.branch_result(
                "defect",
                MessageFlags(),
                _receipt(),
                _failure(FailureKind.VALIDATION, operation, "invalid message reference or no flag change"),
            )
        try:
            credentials = await self.client.resolve_credentials(call, operation)
            return await asyncio.to_thread(self._update, credentials, input)
        except MailFailure as error:
            return _mutation_failure(error, True)

    def _update(self, credentials: Any, input: SetFlagsInput) -> MutationAttempt[MessageFlags]:
        operation = "setFlags"
        session = self.client.open_imap(credentials, operation)
        try:
            _imap_select(session, input.message.mailbox, False, input.message.uid_validity, operation)
            current = _read_flags(session, input.message, operation)
            changes = (
                (input.is_seen, current.is_seen, "\\Seen"),
                (input.is_flagged, current.is_flagged, "\\Flagged"),
                (input.is_answered, current.is_answered, "\\Answered"),
            )
            add = tuple(flag for requested, present, flag in changes if requested is True and not present)
            remove = tuple(flag for requested, present, flag in changes if requested is False and present)
            if not add and not remove:
                return MutationAttempt.branch_result(
                    "updated", replace(current, was_already_applied=True), _receipt()
                )
            if add:
                _uid_command(
                    session,
                    "STORE",
                    operation,
                    str(input.message.uid),
                    "+FLAGS.SILENT",
                    "(" + " ".join(add) + ")",
                )
            if remove:
                _uid_command(
                    session,
                    "STORE",
                    operation,
                    str(input.message.uid),
                    "-FLAGS.SILENT",
                    "(" + " ".join(remove) + ")",
                )
            updated = _read_flags(session, input.message, operation)
            if any(
                requested is not None and requested != present
                for requested, present, _ in (
                    (input.is_seen, updated.is_seen, ""),
                    (input.is_flagged, updated.is_flagged, ""),
                    (input.is_answered, updated.is_answered, ""),
                )
            ):
                return MutationAttempt.branch_result(
                    "providerRejected",
                    updated,
                    _receipt(),
                    _failure(
                        FailureKind.PROVIDER_REJECTION,
                        operation,
                        "the IMAP server accepted STORE but did not keep the requested flags",
                    ),
                )
            return MutationAttempt.branch_result("updated", updated, _receipt())
        finally:
            _imap_close(session)


class MoveMessageOperation:
    """Move one UID or find an earlier move by exact Message-ID."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated move definition."""
        return MOVE_MESSAGE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: MoveMessageInput) -> IdempotencyKey:
        """Use the stable Call ID for a repeatable move."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: MoveMessageInput) -> MutationAttempt[MovedMessage]:
        """Move by UID and recognize an already moved message."""
        operation = "moveMessage"
        if (
            not _valid_reference(input.message)
            or not _valid_mailbox(input.destination_mailbox)
            or (
                input.message.mailbox.casefold() == input.destination_mailbox.casefold()
                if input.message.mailbox.casefold() == "inbox"
                else input.message.mailbox == input.destination_mailbox
            )
            or (
                input.message_id
                and (
                    len(input.message_id) > 250
                    or "@" not in input.message_id
                    or any(
                        ord(character) <= 32 or ord(character) >= 127 or character in "<>"
                        for character in input.message_id
                    )
                )
            )
        ):
            return MutationAttempt.branch_result(
                "defect",
                MovedMessage(),
                _receipt(),
                _failure(FailureKind.VALIDATION, operation, "invalid source, destination, or Message-ID"),
            )
        try:
            credentials = await self.client.resolve_credentials(call, operation)
            return await asyncio.to_thread(self._move, credentials, input)
        except MailFailure as error:
            return _mutation_failure(error, True)

    def _move(self, credentials: Any, input: MoveMessageInput) -> MutationAttempt[MovedMessage]:
        operation = "moveMessage"
        session = self.client.open_imap(credentials, operation)
        try:
            capabilities = {cap.upper() for cap in session.capabilities}
            if "MOVE" not in capabilities and "UIDPLUS" not in capabilities:
                return MutationAttempt.branch_result(
                    "providerRejected",
                    MovedMessage(),
                    _receipt(),
                    _failure(
                        FailureKind.PROVIDER_REJECTION,
                        operation,
                        "the IMAP server supports neither MOVE nor UIDPLUS",
                    ),
                )
            _imap_select(session, input.message.mailbox, False, input.message.uid_validity, operation)
            fetched = _fetch(session, input.message.uid, operation)
            if fetched is None:
                if not input.message_id:
                    return MutationAttempt.branch_result(
                        "notFound",
                        MovedMessage(),
                        _receipt(),
                        _failure(
                            FailureKind.NOT_FOUND, operation, "no message in the source mailbox has the UID"
                        ),
                    )
                validity = _imap_select(session, input.destination_mailbox, True, 0, operation)
                _, found = _uid_command(
                    session, "SEARCH", operation, "HEADER", "Message-ID", f'"<{input.message_id}>"'
                )
                candidate_uids = (
                    sorted((int(item) for item in found[0].split()), reverse=True)
                    if found and found[0]
                    else []
                )
                for uid in candidate_uids:
                    candidate = _fetch(session, uid, operation)
                    if (
                        candidate
                        and _message_id(
                            email.message_from_bytes(candidate[1], policy=default).get("Message-ID")
                        )
                        == input.message_id
                    ):
                        output = MovedMessage(
                            MessageReference(input.destination_mailbox, validity, uid), True
                        )
                        return MutationAttempt.branch_result("moved", output, _receipt(input.message_id))
                return MutationAttempt.branch_result(
                    "notFound",
                    MovedMessage(),
                    _receipt(),
                    _failure(
                        FailureKind.NOT_FOUND, operation, "the message was not in the source or destination"
                    ),
                )
            source_id = _message_id(email.message_from_bytes(fetched[1], policy=default).get("Message-ID"))
            if input.message_id and source_id != input.message_id:
                return MutationAttempt.branch_result(
                    "notFound",
                    MovedMessage(),
                    _receipt(),
                    _failure(
                        FailureKind.NOT_FOUND, operation, "the message at the UID has a different Message-ID"
                    ),
                )
            if "MOVE" in capabilities:
                _, response = _uid_command(
                    session, "MOVE", operation, str(input.message.uid), input.destination_mailbox
                )
            else:
                _, response = _uid_command(
                    session, "COPY", operation, str(input.message.uid), input.destination_mailbox
                )
                _uid_command(
                    session, "STORE", operation, str(input.message.uid), "+FLAGS.SILENT", "(\\Deleted)"
                )
                _uid_command(session, "EXPUNGE", operation, str(input.message.uid))
            destination = MessageReference(input.destination_mailbox, 0, 0)
            joined = b" ".join(item for item in response if isinstance(item, bytes))
            copy_uid = re.search(rb"COPYUID (\d+) \d+ (\d+)", joined)
            if copy_uid:
                destination = MessageReference(
                    input.destination_mailbox, int(copy_uid.group(1)), int(copy_uid.group(2))
                )
            return MutationAttempt.branch_result(
                "moved", MovedMessage(destination), _receipt(input.message_id)
            )
        finally:
            _imap_close(session)
