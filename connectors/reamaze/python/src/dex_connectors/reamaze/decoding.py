# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Validate provider conversation, message, and contact wire shapes."""

from __future__ import annotations

import json
import re
from datetime import UTC, datetime
from typing import Any

from .models import (
    Contact,
    Conversation,
    ConversationChannel,
    ConversationMessage,
    ConversationParticipant,
    FindContactByEmailOutput,
    SearchConversationsOutput,
)

_CONVERSATION_ID = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_-]{0,254}$")


class _WireFloat(float):
    lexeme: str

    def __new__(cls, lexeme: str) -> _WireFloat:
        value = float.__new__(cls, lexeme)
        value.lexeme = lexeme
        return value

    def __str__(self) -> str:
        return self.lexeme


def parse_document(body: bytes) -> Any:
    """Decode a complete JSON response or report a protocol error."""
    try:
        return json.loads(body, parse_float=_WireFloat)
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise ValueError("response is malformed JSON") from error


def parse_time(value: object) -> datetime:
    """Require an RFC 3339 timestamp and return UTC."""
    if not isinstance(value, str) or not re.fullmatch(
        r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})", value
    ):
        raise ValueError("time is not ISO 8601")
    try:
        result = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError as error:
        raise ValueError("time is not ISO 8601") from error
    if result.tzinfo is None:
        raise ValueError("time is not ISO 8601")
    return result.astimezone(UTC)


def _participant(wire: object) -> ConversationParticipant | None:
    if not isinstance(wire, dict):
        return None
    name, email = wire.get("name") or "", wire.get("email") or ""
    return ConversationParticipant(name, email) if name or email else None


def _bounded_text(value: str) -> tuple[str, bool]:
    encoded = value.encode()
    if len(encoded) <= 16384:
        return value, False
    return encoded[:16384].decode(errors="ignore"), True


def decode_conversation(wire: object, *, first_message: bool = True, expected_id: str = "") -> Conversation:
    """Check a provider conversation and map its stable public fields."""
    if (
        not isinstance(wire, dict)
        or not isinstance(wire.get("slug"), str)
        or not _CONVERSATION_ID.fullmatch(wire["slug"])
    ):
        raise ValueError("conversation slug is missing or invalid")
    if wire.get("status") is None or type(wire["status"]) is not int:
        raise ValueError("conversation status is missing")
    if expected_id and wire["slug"] != expected_id:
        raise ValueError("response is for another conversation")
    category = wire.get("category") or {}
    if not isinstance(category, dict):
        raise ValueError("conversation category is invalid")
    message = wire.get("message") or {}
    if not isinstance(message, dict):
        raise ValueError("conversation message is invalid")
    if "body" in message and message["body"] is not None and not isinstance(message["body"], str):
        raise ValueError("conversation message body is invalid")
    for field in ("last_customer_message", "last_staff_message"):
        item = wire.get(field)
        if (
            isinstance(item, dict)
            and "body" in item
            and item["body"] is not None
            and not isinstance(item["body"], str)
        ):
            raise ValueError("conversation message body is invalid")
    text, truncated = _bounded_text(message.get("body") or "") if first_message else ("", False)

    def message_time(field: str) -> datetime | None:
        item = wire.get(field)
        if not isinstance(item, dict) or not item.get("created_at"):
            return None
        return parse_time(item["created_at"])

    return Conversation(
        id=wire["slug"],
        subject=wire.get("subject") or "",
        status=wire["status"],
        tags=tuple(wire.get("tag_list") or ()),
        channel=ConversationChannel(
            category.get("slug") or "", category.get("name") or "", category.get("channel") or 0
        ),
        requester=_participant(wire.get("author")),
        assignee=_participant(wire.get("assignee")),
        first_message=text,
        is_first_message_truncated=truncated,
        created_at=parse_time(wire.get("created_at")),
        last_customer_message_at=message_time("last_customer_message"),
        last_staff_message_at=message_time("last_staff_message"),
    )


def decode_conversation_body(body: bytes, expected_id: str = "") -> Conversation:
    """Decode one unwrapped provider conversation."""
    return decode_conversation(parse_document(body), expected_id=expected_id)


def validate_page(document: object, collection: str) -> tuple[list[Any], int, int]:
    """Require Re:amaze pagination counts and a bounded list."""
    if not isinstance(document, dict) or not isinstance(document.get(collection), list):
        raise ValueError(f"response is not a {collection} page")
    items = document[collection]
    count, total = document.get("page_count"), document.get("total_count")
    if type(count) is not int or type(total) is not int or count < 0 or total < 0:
        raise ValueError("page_count and total_count are required")
    size = document.get("page_size")
    if size is not None and type(size) is not int:
        raise ValueError("page_size must be an integer")
    if len(items) > 100 or size is not None and len(items) > max(size, 0):
        raise ValueError("the page holds more entries than its page size")
    return items, count, total


def decode_conversation_page(body: bytes, page: int) -> SearchConversationsOutput:
    """Decode one page in provider order and calculate its next page."""
    items, count, total = validate_page(parse_document(body), "conversations")
    return SearchConversationsOutput(
        tuple(decode_conversation(item, first_message=False) for item in items),
        total,
        count,
        page + 1 if page < count and page < 10000 else 0,
    )


def decode_message(wire: object) -> ConversationMessage:
    """Decode one message, including numeric or text origin IDs."""
    if not isinstance(wire, dict) or type(wire.get("visibility")) is not int:
        raise ValueError("message visibility is missing")
    origin_id = wire.get("origin_id")
    if origin_id is None:
        origin_id = ""
    elif isinstance(origin_id, (str, int, float)) and not isinstance(origin_id, bool):
        origin_id = str(origin_id)
    else:
        raise ValueError("message origin_id is neither a string nor a number")
    if len(origin_id) > 256:
        raise ValueError("message origin_id is too long")
    body, truncated = _bounded_text(wire.get("body") or "")
    return ConversationMessage(
        body=body,
        is_body_truncated=truncated,
        visibility=wire["visibility"],
        is_internal_note=wire["visibility"] == 1,
        origin=wire.get("origin") or 0,
        origin_id=origin_id,
        author=_participant(wire.get("user")),
        created_at=parse_time(wire.get("created_at")),
    )


def decode_message_page(body: bytes) -> tuple[tuple[ConversationMessage, ...], int, int]:
    """Decode the newest message page and its pagination counts."""
    items, count, total = validate_page(parse_document(body), "messages")
    return tuple(decode_message(item) for item in items), count, total


def decode_matching_contacts(body: bytes, email: str) -> FindContactByEmailOutput:
    """Keep exact case-insensitive email matches from a fuzzy provider search."""
    items, count, _ = validate_page(parse_document(body), "contacts")
    for item in items:
        if isinstance(item, dict):
            for field in ("name", "friendly_name"):
                if field in item and item[field] is not None and not isinstance(item[field], str):
                    raise ValueError("contact name is invalid")
    contacts = tuple(
        Contact(item.get("name") or "", item["email"], item.get("friendly_name") or "")
        for item in items
        if isinstance(item, dict)
        and isinstance(item.get("email"), str)
        and item["email"].casefold() == email.casefold()
    )
    return FindContactByEmailOutput(contacts, count > 1)
