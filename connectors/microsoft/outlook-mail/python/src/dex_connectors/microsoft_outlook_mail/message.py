# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Decode Microsoft Graph message resources into bounded connector models."""

import json
from decimal import Decimal
from html.parser import HTMLParser
from typing import Any

from ._generated.timestamps import canonical_go_timestamp
from .client import validate_graph_id
from .models import Attachment, EmailAddress, Message, MessageSummary

SUMMARY_PROPERTIES = (
    "id",
    "conversationId",
    "internetMessageId",
    "parentFolderId",
    "subject",
    "from",
    "sender",
    "replyTo",
    "toRecipients",
    "ccRecipients",
    "receivedDateTime",
    "sentDateTime",
    "isRead",
    "isDraft",
    "flag",
    "categories",
    "importance",
    "hasAttachments",
    "bodyPreview",
)
DRAFT_PROPERTIES = (
    "id",
    "isDraft",
    "subject",
    "toRecipients",
    "ccRecipients",
    "bccRecipients",
    "internetMessageId",
    "conversationId",
    "parentFolderId",
    "createdDateTime",
    "sentDateTime",
)


def json_object(body: bytes | dict[str, Any]) -> dict[str, Any]:
    """Decode JSON without accepting imprecise floating point values."""
    if isinstance(body, dict):
        return body
    try:
        decoded = json.loads(body, parse_float=Decimal, parse_constant=_reject_nonfinite_number)
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise ValueError("response is not a message object") from error
    if not isinstance(decoded, dict):
        raise ValueError("response is not a message object")
    return decoded


def _reject_nonfinite_number(value: str) -> None:
    raise ValueError(f"response contains non-finite number {value}")


def string_value(resource: dict[str, Any], key: str) -> str:
    """Read one Go string JSON field."""
    value = resource.get(key)
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError(f"{key} has an invalid type")
    return value


def bool_value(resource: dict[str, Any], key: str) -> bool:
    """Read one Go bool JSON field."""
    value = resource.get(key)
    if value is None:
        return False
    if type(value) is not bool:
        raise ValueError(f"{key} has an invalid type")
    return value


def int_value(resource: dict[str, Any], key: str) -> int:
    """Read one Go int JSON field, rejecting floats and exponent syntax."""
    value = resource.get(key)
    if value is None:
        return 0
    if type(value) is not int or value < -(1 << 63) or value >= (1 << 63):
        raise ValueError(f"{key} has an invalid integer")
    return value


def string_list(resource: dict[str, Any], key: str) -> tuple[str, ...]:
    """Read one Go string slice JSON field."""
    value = resource.get(key)
    if value is None:
        return ()
    if not isinstance(value, list):
        raise ValueError(f"{key} is not a list")
    return tuple(item if isinstance(item, str) else _invalid_string(key) for item in value)


def _invalid_string(key: str) -> str:
    raise ValueError(f"{key} contains a non-string")


def timestamp_value(resource: dict[str, Any], key: str) -> str | None:
    """Parse one Graph RFC 3339 timestamp with Go's accepted syntax and precision."""
    value = resource.get(key)
    if value is None:
        return None
    if not isinstance(value, str):
        raise ValueError(f"{key} has an invalid timestamp")
    try:
        return canonical_go_timestamp(value, key)
    except ValueError as error:
        raise ValueError(f"{key} has an invalid timestamp") from error


def address_value(value: Any) -> EmailAddress | None:
    """Decode one Graph recipient and skip an empty address."""
    if value is None:
        return None
    if not isinstance(value, dict):
        raise ValueError("recipient is not an object")
    address = value.get("emailAddress")
    if address is None:
        return None
    if not isinstance(address, dict):
        raise ValueError("emailAddress is not an object")
    mailbox = string_value(address, "address")
    return EmailAddress(name=string_value(address, "name"), address=mailbox) if mailbox else None


def address_list(resource: dict[str, Any], key: str, limit: int = 50) -> tuple[EmailAddress, ...]:
    """Decode up to 50 nonempty Graph recipients in their original order."""
    value = resource.get(key)
    if value is None:
        return ()
    if not isinstance(value, list):
        raise ValueError(f"{key} is not a list")
    addresses: list[EmailAddress] = []
    for entry in value:
        address = address_value(entry)
        if address is not None:
            addresses.append(address)
            if len(addresses) == limit:
                break
    return tuple(addresses)


def decode_graph_message(body: bytes | dict[str, Any]) -> dict[str, Any]:
    """Require a usable message ID and decode every field Go's wire struct uses."""
    resource = json_object(body)
    message_id = string_value(resource, "id")
    try:
        validate_graph_id("message id", message_id)
    except ValueError as error:
        raise ValueError("response has no usable message id") from error
    for key in (
        "conversationId",
        "internetMessageId",
        "parentFolderId",
        "subject",
        "importance",
        "bodyPreview",
        "webLink",
    ):
        string_value(resource, key)
    for key in ("isRead", "isDraft", "hasAttachments"):
        bool_value(resource, key)
    for key in ("receivedDateTime", "sentDateTime", "createdDateTime"):
        timestamp_value(resource, key)
    for key in ("replyTo", "toRecipients", "ccRecipients", "bccRecipients"):
        address_list(resource, key)
    for key in ("from", "sender"):
        address_value(resource.get(key))
    string_list(resource, "categories")
    flag = resource.get("flag")
    if flag is not None:
        if not isinstance(flag, dict):
            raise ValueError("flag is not an object")
        string_value(flag, "flagStatus")
    body_value = resource.get("body")
    if body_value is not None:
        if not isinstance(body_value, dict):
            raise ValueError("body is not an object")
        string_value(body_value, "contentType")
        string_value(body_value, "content")
    return resource


def message_summary(resource: dict[str, Any]) -> MessageSummary:
    """Build the bounded summary returned by search and get."""
    flag = resource.get("flag")
    flag_status = string_value(flag, "flagStatus") if isinstance(flag, dict) else ""
    is_draft = bool_value(resource, "isDraft")
    received_at = timestamp_value(resource, "receivedDateTime") or "0001-01-01T00:00:00Z"
    return MessageSummary(
        id=string_value(resource, "id"),
        conversation_id=string_value(resource, "conversationId"),
        internet_message_id=string_value(resource, "internetMessageId"),
        folder_id=string_value(resource, "parentFolderId"),
        subject=string_value(resource, "subject")[:255],
        from_=address_value(resource.get("from")),
        sender=address_value(resource.get("sender")),
        reply_to=address_list(resource, "replyTo"),
        to=address_list(resource, "toRecipients"),
        cc=address_list(resource, "ccRecipients"),
        received_at=received_at,
        sent_at=None if is_draft else timestamp_value(resource, "sentDateTime"),
        is_read=bool_value(resource, "isRead"),
        is_draft=is_draft,
        flag_status=flag_status or "notFlagged",
        categories=string_list(resource, "categories")[:25],
        importance=string_value(resource, "importance"),
        has_attachments=bool_value(resource, "hasAttachments"),
        preview=string_value(resource, "bodyPreview")[:255],
    )


class _HTMLText(HTMLParser):
    """Mirror Go's text extraction from Graph HTML responses."""

    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self.parts: list[str] = []
        self.skipped_depth = 0

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        if tag in ("script", "style", "head"):
            self.skipped_depth += 1
        elif tag in ("br", "p", "div", "li", "tr", "h1", "h2", "h3", "h4", "blockquote"):
            self.parts.append("\n")

    def handle_endtag(self, tag: str) -> None:
        if tag in ("script", "style", "head"):
            self.skipped_depth = max(0, self.skipped_depth - 1)

    def handle_data(self, data: str) -> None:
        if self.skipped_depth == 0:
            self.parts.append(data)


def decode_full_message(body: bytes) -> Message:
    """Decode one message including text, omitting attachment content."""
    resource = decode_graph_message(body)
    summary = message_summary(resource)
    text = ""
    source = "text"
    body_value = resource.get("body")
    if isinstance(body_value, dict):
        text = string_value(body_value, "content")
        if string_value(body_value, "contentType").lower() == "html":
            parser = _HTMLText()
            parser.feed(text)
            text = "".join(parser.parts).strip()
            source = "html"
    encoded = text.encode()
    is_truncated = len(encoded) > 65536
    if is_truncated:
        text = encoded[:65536].decode(errors="ignore")
    return Message(
        **{field: getattr(summary, field) for field in summary.__dataclass_fields__},
        bcc=address_list(resource, "bccRecipients"),
        text=text,
        is_text_truncated=is_truncated,
        text_source=source,
        web_link=string_value(resource, "webLink"),
    )


def decode_attachment_list(body: bytes) -> tuple[tuple[Attachment, ...], bool]:
    """Decode one bounded attachment page and mark a continuation as truncated."""
    envelope = json_object(body)
    values = envelope.get("value")
    if not isinstance(values, list):
        raise ValueError("response has no value list")
    attachments: list[Attachment] = []
    for value in values[:50]:
        if not isinstance(value, dict):
            raise ValueError("attachment is not an object")
        attachment_id = string_value(value, "id")
        size = int_value(value, "size")
        if not attachment_id or size < 0:
            raise ValueError("an attachment has no id or a negative size")
        kind = {
            "#microsoft.graph.itemAttachment": "item",
            "#microsoft.graph.referenceAttachment": "reference",
        }.get(string_value(value, "@odata.type"), "file")
        attachments.append(
            Attachment(
                id=attachment_id,
                name=string_value(value, "name"),
                content_type=string_value(value, "contentType"),
                size_bytes=size,
                is_inline=bool_value(value, "isInline"),
                kind=kind,
            )
        )
    return tuple(attachments), len(values) > 50 or bool(string_value(envelope, "@odata.nextLink"))
