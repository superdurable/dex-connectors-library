# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Decode the Help Scout wire subset used by operation results."""

import json
import re
from datetime import UTC, datetime, timedelta
from typing import Any
from urllib.parse import urlsplit

from .models import Conversation, Customer, Person, Thread

_TIMESTAMP = re.compile(r"^(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)(?:[.,](\d+))?(Z|[+-]\d\d:\d\d)$")
_ZERO = datetime.min.replace(tzinfo=UTC)


def object_value(value: Any) -> dict[str, Any]:
    """Reject wrong-typed provider objects without coercion."""
    if not isinstance(value, dict):
        raise ValueError("provider value is not an object")
    return value


def array_value(value: Any) -> list[Any]:
    """Treat an omitted Go slice as empty and reject another type."""
    if value is None:
        return []
    if not isinstance(value, list):
        raise ValueError("provider value is not an array")
    return value


def int_value(value: Any) -> int:
    """Decode a Go int without treating booleans as numbers."""
    if value is None:
        return 0
    if type(value) is not int or value < -(1 << 63) or value >= (1 << 63):
        raise ValueError("provider value is not an integer")
    return value


def text_value(value: Any) -> str:
    """Decode a Go string field."""
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError("provider value is not a string")
    return value


def parse_json(body: bytes) -> dict[str, Any]:
    """Parse a nonempty provider JSON object."""
    if not body:
        raise ValueError("provider response is empty")
    return object_value(json.loads(body))


def parse_timestamp(value: str, optional: bool = False) -> datetime:
    """Parse Go-compatible RFC 3339, including comma fractions and broad offsets."""
    if not value and optional:
        return _ZERO
    match = _TIMESTAMP.fullmatch(value)
    if not match:
        raise ValueError("timestamp is not ISO 8601")
    year, month, day, hour, minute, second = map(int, match.groups()[:6])
    fraction = match.group(7) or ""
    zone = match.group(8)
    if zone == "Z":
        offset = timedelta()
    else:
        hours, minutes = map(int, zone[1:].split(":"))
        if minutes > 59:
            raise ValueError("timestamp is not ISO 8601")
        offset = timedelta(hours=hours, minutes=minutes) * (1 if zone[0] == "+" else -1)
    try:
        parsed = datetime(year, month, day, hour, minute, second, int((fraction + "000000")[:6]))
        return (parsed - offset).replace(tzinfo=UTC)
    except ValueError as error:
        raise ValueError("timestamp is not ISO 8601") from error


def canonical_timestamp(value: str, optional: bool = False) -> str:
    """Preserve Go's nine fractional digits while expressing provider times in UTC."""
    if not value and optional:
        return "0001-01-01T00:00:00Z"
    parsed = parse_timestamp(value)
    match = _TIMESTAMP.fullmatch(value)
    assert match is not None
    fraction = (match.group(7) or "")[:9].rstrip("0")
    seconds = (
        f"{parsed.year:04d}-{parsed.month:02d}-{parsed.day:02d}T"
        f"{parsed.hour:02d}:{parsed.minute:02d}:{parsed.second:02d}"
    )
    return seconds + ("." + fraction if fraction else "") + "Z"


def truncate_utf8(value: str, limit: int) -> str:
    """Cut UTF-8 without splitting a code point."""
    return value.encode()[:limit].decode("utf-8", "ignore")


def person(value: Any) -> Person | None:
    """Ignore absent persons and persons without a positive ID."""
    if value is None:
        return None
    wire = object_value(value)
    identifier = int_value(wire.get("id"))
    if identifier < 1:
        return None
    return Person(
        identifier,
        text_value(wire.get("type")),
        text_value(wire.get("email")),
        text_value(wire.get("first")),
        text_value(wire.get("last")),
    )


def conversation(value: Any, expected_id: int = 0) -> Conversation:
    """Decode one conversation, checking its identity for a direct read."""
    wire = object_value(value)
    identifier = int_value(wire.get("id"))
    if identifier < 1 or expected_id and identifier != expected_id:
        raise ValueError("conversation has no ID or names another conversation")
    waiting = wire.get("customerWaitingSince")
    waiting_time = text_value(object_value(waiting).get("time")) if waiting is not None else ""
    source = object_value(wire.get("source")) if wire.get("source") is not None else {}
    links = object_value(wire.get("_links")) if wire.get("_links") is not None else {}
    web = object_value(links.get("web")) if links.get("web") is not None else {}
    web_url = text_value(web.get("href"))
    parsed_url = urlsplit(web_url)
    if parsed_url.scheme != "https" or not parsed_url.netloc or parsed_url.username is not None:
        web_url = ""
    tags = tuple(text_value(object_value(tag).get("tag")) for tag in array_value(wire.get("tags")))
    return Conversation(
        id=identifier,
        number=int_value(wire.get("number")),
        subject=text_value(wire.get("subject")),
        status=text_value(wire.get("status")),
        state=text_value(wire.get("state")),
        type=text_value(wire.get("type")),
        mailbox_id=int_value(wire.get("mailboxId")),
        folder_id=int_value(wire.get("folderId")),
        preview=truncate_utf8(text_value(wire.get("preview")), 1024),
        thread_count=int_value(wire.get("threads")),
        assignee=person(wire.get("assignee")),
        primary_customer=person(wire.get("primaryCustomer")),
        created_by=person(wire.get("createdBy")),
        tags=tuple(tag for tag in tags if tag),
        source_type=text_value(source.get("type")),
        source_via=text_value(source.get("via")),
        created_at=canonical_timestamp(text_value(wire.get("createdAt")), True),
        closed_at=canonical_timestamp(text_value(wire.get("closedAt")), True),
        user_updated_at=canonical_timestamp(text_value(wire.get("userUpdatedAt")), True),
        customer_waiting_since=canonical_timestamp(waiting_time, True),
        web_url=web_url,
    )


def thread(value: Any) -> Thread:
    """Decode a newest-first conversation thread."""
    wire = object_value(value)
    identifier = int_value(wire.get("id"))
    kind = text_value(wire.get("type"))
    if identifier < 1 or not kind:
        raise ValueError("thread has no ID or type")
    original_body = text_value(wire.get("body"))
    body = truncate_utf8(original_body, 16 << 10)
    source = object_value(wire.get("source")) if wire.get("source") is not None else {}
    return Thread(
        id=identifier,
        type=kind,
        status=text_value(wire.get("status")),
        state=text_value(wire.get("state")),
        body=body,
        is_body_truncated=len(body.encode()) < len(original_body.encode()),
        created_by=person(wire.get("createdBy")),
        source_type=text_value(source.get("type")),
        source_via=text_value(source.get("via")),
        created_at=canonical_timestamp(text_value(wire.get("createdAt"))),
    )


def customer(value: Any) -> Customer:
    """Decode one customer profile and its embedded addresses."""
    wire = object_value(value)
    identifier = int_value(wire.get("id"))
    if identifier < 1:
        raise ValueError("customer has no ID")
    embedded = object_value(wire.get("_embedded")) if wire.get("_embedded") is not None else {}
    addresses = tuple(
        text_value(object_value(item).get("value")) for item in array_value(embedded.get("emails"))
    )
    return Customer(
        id=identifier,
        first_name=text_value(wire.get("firstName")),
        last_name=text_value(wire.get("lastName")),
        emails=tuple(address for address in addresses if address),
        organization_id=int_value(wire.get("organizationId")),
        conversation_count=int_value(wire.get("conversationCount")),
        created_at=canonical_timestamp(text_value(wire.get("createdAt"))),
        updated_at=canonical_timestamp(text_value(wire.get("updatedAt")), True),
    )
