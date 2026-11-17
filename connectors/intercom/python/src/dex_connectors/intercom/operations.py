# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Intercom operation attempts and Go-compatible provider decoding."""

import html
import json
import re
from dataclasses import replace
from datetime import UTC, datetime
from email._header_value_parser import get_addr_spec
from email.errors import HeaderParseError
from functools import cmp_to_key
from ipaddress import IPv4Address
from typing import Any
from urllib.parse import quote

from dex_connectors.sdk import (
    Call,
    CallID,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    QueryAttempt,
    QueryDefinition,
    Receipt,
)
from dex_connectors.sdk.singledispatch import (
    CheckpointError,
    DecisionKind,
    DispatchNotAllowedError,
    Fence,
    InsufficientAttemptTimeError,
)

from ._generated.definitions import (
    FIND_CONTACT_BY_EMAIL_DEFINITION,
    GET_CONVERSATION_DEFINITION,
    REPLY_TO_CONVERSATION_DEFINITION,
    SEARCH_CONVERSATIONS_DEFINITION,
    UPDATE_CONVERSATION_STATE_DEFINITION,
)
from .client import Client, Exchange
from .models import (
    Contact,
    Conversation,
    ConversationAuthor,
    ConversationContact,
    ConversationDetails,
    ConversationPart,
    ConversationSource,
    ConversationTag,
    FindContactByEmailInput,
    FindContactByEmailOutput,
    GetConversationInput,
    ReplyToConversationInput,
    ReplyToConversationOutput,
    SearchConversationsInput,
    SearchConversationsOutput,
    UpdateConversationStateInput,
    UpdateConversationStateOutput,
)

_NUMERIC_ID = re.compile(r"[0-9]{1,24}\Z")
_CONTACT_ID = re.compile(r"[A-Za-z0-9_-]{1,64}\Z")
_ZERO_TIME = datetime.min.replace(tzinfo=UTC)


def _object(value: Any) -> dict[str, Any]:
    if value is None:
        return {}
    if not isinstance(value, dict):
        raise ValueError("object is invalid")
    return value


def _string(value: Any) -> str:
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError("string field is invalid")
    return value


def _identifier(value: Any) -> str:
    if value is None:
        return ""
    if isinstance(value, str):
        return value
    if type(value) is int and len(str(value)) <= 24 and re.fullmatch(r"-?[0-9]{1,24}", str(value)):
        return str(value)
    raise ValueError("identifier is not a string or an integer")


def _seconds(value: Any) -> int:
    text = _identifier(value)
    if not text:
        return 0
    if not re.fullmatch(r"-?[0-9]{1,24}", text):
        raise ValueError("timestamp is not Unix seconds")
    seconds = int(text)
    if seconds < 0 or seconds > 2**63 - 1:
        raise ValueError("timestamp is not Unix seconds")
    return seconds


def _time(value: Any, field_name: str = "timestamp") -> datetime:
    seconds = _seconds(value)
    try:
        return datetime.fromtimestamp(seconds, UTC) if seconds else _ZERO_TIME
    except (OverflowError, OSError, ValueError):
        raise ValueError(f"{field_name} is outside Python's representable timestamp range") from None


def _integer(value: Any) -> int:
    if value is None:
        return 0
    if type(value) is not int:
        raise ValueError("integer field is invalid")
    if not -(2**63) <= value <= 2**63 - 1:
        raise ValueError("integer field is outside Go int range")
    return value


def _wire_array(value: Any) -> list[Any]:
    if value is None:
        return []
    if not isinstance(value, list):
        raise ValueError("array field is invalid")
    return value


def _validate_author_wire(value: Any) -> None:
    wire = _object(value)
    for name in ("type", "name", "email"):
        _string(wire.get(name))
    _identifier(wire.get("id"))


def _validate_part_wire(value: Any) -> None:
    wire = _object(value)
    _identifier(wire.get("id"))
    for name in ("part_type", "body"):
        _string(wire.get(name))
    _validate_author_wire(wire.get("author"))
    _seconds(wire.get("created_at"))


def _validate_conversation_wire(value: Any) -> dict[str, Any]:
    wire = _object(value)
    for name in ("type", "title", "state", "priority"):
        _string(wire.get(name))
    for name in ("id", "admin_assignee_id", "team_assignee_id"):
        _identifier(wire.get(name))
    for name in ("open", "read"):
        _boolean(wire.get(name))
    for name in ("created_at", "updated_at", "waiting_since", "snoozed_until"):
        _seconds(wire.get(name))
    source = _object(wire.get("source"))
    for name in ("type", "delivered_as", "subject", "body"):
        _string(source.get(name))
    _validate_author_wire(source.get("author"))
    for entry in _wire_array(_object(wire.get("contacts")).get("contacts")):
        contact = _object(entry)
        _identifier(contact.get("id"))
        _string(contact.get("external_id"))
    for entry in _wire_array(_object(wire.get("tags")).get("tags")):
        tag = _object(entry)
        _identifier(tag.get("id"))
        _string(tag.get("name"))
    parts = _object(wire.get("conversation_parts"))
    _integer(parts.get("total_count"))
    for part in _wire_array(parts.get("conversation_parts")):
        _validate_part_wire(part)
    return wire


def _boolean(value: Any) -> bool:
    if value is None:
        return False
    if type(value) is not bool:
        raise ValueError("boolean field is invalid")
    return value


def _body(value: Any) -> tuple[str, bool]:
    text = _string(value)
    encoded = text.encode()
    if len(encoded) <= 16384:
        return text, False
    return encoded[:16384].decode("utf-8", errors="ignore"), True


def _author(value: Any) -> ConversationAuthor:
    wire = _object(value)
    return ConversationAuthor(
        _string(wire.get("type")),
        _identifier(wire.get("id")),
        _string(wire.get("name")),
        _string(wire.get("email")),
    )


def _conversation(wire: dict[str, Any], include_body: bool = False) -> Conversation:
    _validate_conversation_wire(wire)
    if _string(wire.get("type")) not in ("", "conversation"):
        raise ValueError("object is not a conversation")
    identifier = _identifier(wire.get("id"))
    if not _NUMERIC_ID.fullmatch(identifier):
        raise ValueError("conversation ID is missing or not numeric")
    source = _object(wire.get("source"))
    body, truncated = _body(source.get("body")) if include_body else ("", False)
    contacts = []
    contact_values = _object(wire.get("contacts")).get("contacts")
    for value in _wire_array(contact_values):
        entry = _object(value)
        contact_id = _identifier(entry.get("id"))
        if not contact_id:
            raise ValueError("conversation contact ID is missing")
        contacts.append(ConversationContact(contact_id, _string(entry.get("external_id"))))
    tags = []
    tag_values = _object(wire.get("tags")).get("tags")
    for value in _wire_array(tag_values):
        entry = _object(value)
        tag_id = _identifier(entry.get("id"))
        if not tag_id:
            raise ValueError("conversation tag ID is missing")
        tags.append(ConversationTag(tag_id, _string(entry.get("name"))))
    waiting = _seconds(wire.get("waiting_since"))
    snoozed = _seconds(wire.get("snoozed_until"))
    admin_id = _identifier(wire.get("admin_assignee_id"))
    team_id = _identifier(wire.get("team_assignee_id"))
    return Conversation(
        id=identifier,
        title=_string(wire.get("title")),
        state=_string(wire.get("state")),
        is_open=_boolean(wire.get("open")),
        is_read=_boolean(wire.get("read")),
        priority=_string(wire.get("priority")),
        admin_assignee_id="" if admin_id == "0" else admin_id,
        team_assignee_id="" if team_id == "0" else team_id,
        source=ConversationSource(
            type=_string(source.get("type")),
            delivered_as=_string(source.get("delivered_as")),
            subject=_string(source.get("subject")),
            body=body,
            is_body_truncated=truncated,
            author=_author(source.get("author")),
        ),
        contacts=tuple(contacts),
        tags=tuple(tags),
        created_at=_time(wire.get("created_at"), "created_at"),
        updated_at=_time(wire.get("updated_at"), "updated_at"),
        waiting_since=_time(waiting, "waiting_since") if waiting else None,
        snoozed_until=_time(snoozed, "snoozed_until") if snoozed else None,
    )


def _part(value: Any) -> ConversationPart:
    _validate_part_wire(value)
    wire = _object(value)
    part_id = _identifier(wire.get("id"))
    if not part_id:
        raise ValueError("conversation part ID is missing")
    body, truncated = _body(wire.get("body"))
    return ConversationPart(
        part_id,
        _string(wire.get("part_type")),
        body,
        truncated,
        _author(wire.get("author")),
        _time(wire.get("created_at"), "created_at"),
    )


def _envelope(exchange: Exchange, conversation_id: str) -> dict[str, Any]:
    try:
        wire = _validate_conversation_wire(json.loads(exchange.body))
    except (ValueError, UnicodeDecodeError):
        raise ValueError("conversation response is not a conversation object") from None
    if _identifier(wire.get("id")) != conversation_id:
        raise ValueError("conversation response is for another conversation")
    return wire


def _parts(wire: dict[str, Any]) -> list[dict[str, Any]]:
    values = _object(wire.get("conversation_parts")).get("conversation_parts")
    return [_object(value) for value in _wire_array(values)]


def _compare_part_ids(left: str, right: str) -> int:
    if len(left) != len(right) and _NUMERIC_ID.fullmatch(left) and _NUMERIC_ID.fullmatch(right):
        return (len(left) > len(right)) - (len(left) < len(right))
    return (left > right) - (left < right)


def _compare_parts_newest_first(left: ConversationPart, right: ConversationPart) -> int:
    if left.created_at != right.created_at:
        return (right.created_at > left.created_at) - (right.created_at < left.created_at)
    return _compare_part_ids(right.id, left.id)


def _zero_conversation() -> Conversation:
    return Conversation(id="", state="", is_open=False)


def _zero_details() -> ConversationDetails:
    return ConversationDetails(_zero_conversation())


def _zero_update() -> UpdateConversationStateOutput:
    return UpdateConversationStateOutput(_zero_conversation())


def _retryable(exchange: Exchange) -> bool:
    return exchange.outcome in ("notSent", "rateLimited", "unconfirmed")


def _query_failure(
    client: Client,
    call: Call,
    exchange: Exchange,
    empty: Any,
    has_not_found: bool = False,
    object_id: str = "",
) -> QueryAttempt[Any] | None:
    if exchange.outcome == "succeeded":
        return None
    if _retryable(exchange):
        assert exchange.failure is not None
        return QueryAttempt.retry(exchange.failure, exchange.retry_after.total_seconds())
    branch = (
        "notFound"
        if exchange.outcome == "notFound" and has_not_found
        else "invalidResponse"
        if exchange.outcome == "invalid"
        else "defect"
        if exchange.outcome == "defect"
        else "providerRejected"
    )
    return QueryAttempt.branch_result(
        branch, empty, client.receipt(call, object_id, exchange.request_id), exchange.failure
    )


def _mutation_failure(
    client: Client, call: Call, exchange: Exchange, empty: Any, object_id: str
) -> MutationAttempt[Any] | None:
    if exchange.outcome == "succeeded":
        return None
    if _retryable(exchange):
        assert exchange.failure is not None
        return MutationAttempt.retry(exchange.failure, exchange.retry_after.total_seconds())
    branch = (
        "notFound"
        if exchange.outcome == "notFound"
        else "invalidResponse"
        if exchange.outcome == "invalid"
        else "defect"
        if exchange.outcome == "defect"
        else "providerRejected"
    )
    return MutationAttempt.branch_result(
        branch, empty, client.receipt(call, object_id, exchange.request_id), exchange.failure
    )


def _bare_email(value: str) -> bool:
    if not value or any(character in value for character in " \"'()<>,;"):
        return False
    try:
        address, remaining = get_addr_spec(value)
        if remaining or address.defects or str(address) != value:
            return False
        domain = value.rsplit("@", 1)[-1]
        if domain.startswith("["):
            IPv4Address(domain[1:-1])
        return True
    except (ValueError, IndexError, HeaderParseError):
        return False


def _rfc3339(value: str) -> int | None:
    matched = re.fullmatch(
        r"(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:[.,]\d+)?(Z|([+-])(\d{2}):(\d{2}))", value
    )
    if not matched:
        return None
    year, month, day, hour, minute, second = map(int, matched.group(1, 2, 3, 4, 5, 6))
    offset_hours = int(matched.group(9) or 0)
    offset_minutes = int(matched.group(10) or 0)
    if hour > 23 or minute > 59 or second > 59 or offset_hours > 24 or offset_minutes > 60:
        return None
    try:
        instant = datetime(year, month, day, hour, minute, second, tzinfo=UTC)
    except ValueError:
        return None
    offset = (offset_hours * 60 + offset_minutes) * 60
    return int(instant.timestamp()) - (
        offset if matched.group(8) == "+" else -offset if matched.group(8) == "-" else 0
    )


def _filter(field: str, value: Any) -> dict[str, Any]:
    return {"field": field, "operator": "=", "value": value}


def _search_query(input: SearchConversationsInput) -> dict[str, Any]:
    filters = []
    for name, field, values in (
        ("states", "state", input.states),
        ("contactIds", "contact_ids", input.contact_ids),
        ("tagIds", "tag_ids", input.tag_ids),
    ):
        if len(values) > 15:
            raise ValueError(f"{name} has more than 15 values")
        seen: set[str] = set()
        for value in values:
            if name == "states" and value not in ("open", "closed", "snoozed"):
                raise ValueError(
                    f'state "{value}" is not an Intercom conversation state; use open, closed, or snoozed'
                )
            if name == "contactIds" and not _CONTACT_ID.fullmatch(value):
                raise ValueError(f'contactIds entry "{value}" is not an Intercom contact ID')
            if name == "tagIds" and not _NUMERIC_ID.fullmatch(value):
                raise ValueError(f'tagIds entry "{value}" is not an Intercom tag ID of digits')
            if value in seen:
                raise ValueError(f'{name} lists "{value}" twice')
            seen.add(value)
        if values:
            elements = [_filter(field, value) for value in values]
            filters.append(elements[0] if len(elements) == 1 else {"operator": "OR", "value": elements})
        if name == "states" and input.contact_email:
            if not _bare_email(input.contact_email):
                raise ValueError("contactEmail must be one bare email address such as jane@example.com")
            filters.append(_filter("source.author.email", input.contact_email))
    if input.updated_since:
        seconds = _rfc3339(input.updated_since)
        if seconds is None or seconds < 0:
            raise ValueError(
                "updatedSince must be an RFC 3339 instant with an explicit offset, "
                "such as 2026-01-21T00:00:00Z"
            )
        filters.append({"field": "updated_at", "operator": ">", "value": seconds})
    if not filters:
        raise ValueError(
            "at least one of states, contactEmail, contactIds, tagIds, or updatedSince is required"
        )
    return filters[0] if len(filters) == 1 else {"operator": "AND", "value": filters}


class SearchConversationsOperation:
    """Read one page of conversations selected by typed filters."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return immutable operation identity and defaults."""
        return SEARCH_CONVERSATIONS_DEFINITION

    async def invoke(
        self, call: Call, input: SearchConversationsInput
    ) -> QueryAttempt[SearchConversationsOutput]:
        """Send one search and decode its bounded result page."""
        operation = "searchConversations"
        empty = SearchConversationsOutput()
        try:
            query = _search_query(input)
            if type(input.page_size) is not int or not 0 <= input.page_size <= 150:
                raise ValueError("pageSize must be between 1 and 150, or zero for 20")
            if len(input.cursor.encode()) > 2048 or any(ord(c) < 33 or ord(c) > 126 for c in input.cursor):
                raise ValueError("cursor must be the printable NextCursor of a previous page")
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", empty, Receipt(), self.client.failure(operation, FailureKind.VALIDATION, str(error))
            )
        credentials, failure = await self.client.resolve_credentials(call, operation)
        if failure:
            return QueryAttempt.branch_result("defect", empty, Receipt(), failure)
        assert credentials is not None
        exchange = await self.client.exchange(
            call,
            credentials,
            operation,
            "POST",
            "/conversations/search",
            {
                "query": query,
                "pagination": {
                    "per_page": input.page_size or 20,
                    **({"starting_after": input.cursor} if input.cursor else {}),
                },
            },
        )
        bad = _query_failure(self.client, call, exchange, empty)
        if bad:
            return bad
        receipt = self.client.receipt(call, request_id=exchange.request_id)
        try:
            page = _object(json.loads(exchange.body))
            if _string(page.get("type")) not in ("", "conversation.list"):
                raise ValueError("search page is not a conversation list")
            values = page.get("conversations")
            if not isinstance(values, list):
                raise ValueError("search page has no conversations array")
            if len(values) > (input.page_size or 20):
                raise ValueError("search page is larger than requested")
            conversations = []
            for index, value in enumerate(values):
                try:
                    conversations.append(_conversation(_object(value)))
                except ValueError as error:
                    raise ValueError(f"conversation {index}: {error}") from None
            next_page = _object(_object(page.get("pages")).get("next"))
            cursor = _string(next_page.get("starting_after"))
            if cursor and (len(cursor.encode()) > 2048 or any(ord(c) < 33 or ord(c) > 126 for c in cursor)):
                raise ValueError("search page cursor is not printable")
            return QueryAttempt.branch_result(
                "searched",
                SearchConversationsOutput(tuple(conversations), _integer(page.get("total_count")), cursor),
                receipt,
            )
        except (ValueError, TypeError, OverflowError) as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                self.client.failure(
                    operation, FailureKind.PROTOCOL, "Intercom returned an invalid search page: " + str(error)
                ),
            )


class GetConversationOperation:
    """Read a conversation and its newest bounded parts."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return immutable operation identity and defaults."""
        return GET_CONVERSATION_DEFINITION

    async def invoke(self, call: Call, input: GetConversationInput) -> QueryAttempt[ConversationDetails]:
        """Read and decode one conversation."""
        operation = "getConversation"
        empty = _zero_details()
        problem = (
            "conversationId must be an Intercom conversation ID of digits, such as 215472658213"
            if not _NUMERIC_ID.fullmatch(input.conversation_id)
            else "latestPartLimit must be between 1 and 50, or zero for 10"
            if type(input.latest_part_limit) is not int or not 0 <= input.latest_part_limit <= 50
            else ""
        )
        if problem:
            return QueryAttempt.branch_result(
                "defect", empty, Receipt(), self.client.failure(operation, FailureKind.VALIDATION, problem)
            )
        credentials, failure = await self.client.resolve_credentials(call, operation)
        if failure:
            return QueryAttempt.branch_result("defect", empty, Receipt(), failure)
        assert credentials is not None
        exchange = await self.client.read_conversation(call, credentials, operation, input.conversation_id)
        bad = _query_failure(self.client, call, exchange, empty, True, input.conversation_id)
        if bad:
            return bad
        receipt = self.client.receipt(call, input.conversation_id, exchange.request_id)
        try:
            wire = _envelope(exchange, input.conversation_id)
            conversation = _conversation(wire, True)
            parts = []
            for index, value in enumerate(_parts(wire)):
                try:
                    parts.append(_part(value))
                except ValueError as error:
                    raise ValueError(f"part {index}: {error}") from None
            parts.sort(key=cmp_to_key(_compare_parts_newest_first))
            latest = tuple(parts[: input.latest_part_limit or 10])
            count = max(_integer(_object(wire.get("conversation_parts")).get("total_count")), len(parts))
            return QueryAttempt.branch_result(
                "found", ConversationDetails(conversation, latest, count > len(latest), count), receipt
            )
        except (ValueError, TypeError, OverflowError) as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                self.client.failure(
                    operation,
                    FailureKind.PROTOCOL,
                    "Intercom returned an invalid conversation: " + str(error),
                ),
            )


class FindContactByEmailOperation:
    """Find one page of user and lead contacts by exact email."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return immutable operation identity and defaults."""
        return FIND_CONTACT_BY_EMAIL_DEFINITION

    async def invoke(
        self, call: Call, input: FindContactByEmailInput
    ) -> QueryAttempt[FindContactByEmailOutput]:
        """Search the contact index once."""
        operation = "findContactByEmail"
        empty = FindContactByEmailOutput()
        problem = (
            "email must be one bare email address such as jane@example.com"
            if not _bare_email(input.email)
            else "contactLimit must be between 1 and 50, or zero for 10"
            if type(input.contact_limit) is not int or not 0 <= input.contact_limit <= 50
            else ""
        )
        if problem:
            return QueryAttempt.branch_result(
                "defect", empty, Receipt(), self.client.failure(operation, FailureKind.VALIDATION, problem)
            )
        credentials, failure = await self.client.resolve_credentials(call, operation)
        if failure:
            return QueryAttempt.branch_result("defect", empty, Receipt(), failure)
        limit = input.contact_limit or 10
        assert credentials is not None
        exchange = await self.client.exchange(
            call,
            credentials,
            operation,
            "POST",
            "/contacts/search",
            {"query": _filter("email", input.email), "pagination": {"per_page": limit}},
        )
        bad = _query_failure(self.client, call, exchange, empty)
        if bad:
            return bad
        receipt = self.client.receipt(call, request_id=exchange.request_id)
        try:
            page = _object(json.loads(exchange.body))
            if _string(page.get("type")) not in ("", "list"):
                raise ValueError("contact page is not a contact list")
            values = page.get("data")
            if not isinstance(values, list):
                raise ValueError("contact page has no data array")
            if len(values) > limit:
                raise ValueError("contact page is larger than requested")
            contacts = []
            for index, value in enumerate(values):
                wire = _object(value)
                if _string(wire.get("type")) not in ("", "contact"):
                    raise ValueError(f"contact {index} is not a contact")
                identifier = _identifier(wire.get("id"))
                if not _CONTACT_ID.fullmatch(identifier):
                    raise ValueError(f"contact {index} has no valid ID")
                contacts.append(
                    Contact(
                        id=identifier,
                        role=_string(wire.get("role")),
                        email=_string(wire.get("email")),
                        name=_string(wire.get("name")),
                        external_id=_string(wire.get("external_id")),
                        created_at=_time(wire.get("created_at")),
                        updated_at=_time(wire.get("updated_at")),
                    )
                )
            next_page = _object(_object(page.get("pages")).get("next"))
            has_more = bool(_string(next_page.get("starting_after"))) or _integer(
                page.get("total_count")
            ) > len(contacts)
            output = FindContactByEmailOutput(tuple(contacts), has_more)
            if not contacts:
                return QueryAttempt.branch_result(
                    "notFound",
                    output,
                    receipt,
                    self.client.failure(
                        operation, FailureKind.NOT_FOUND, "no Intercom contact has the email address"
                    ),
                )
            return QueryAttempt.branch_result("found", output, receipt)
        except (ValueError, TypeError, OverflowError) as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                self.client.failure(
                    operation,
                    FailureKind.PROTOCOL,
                    "Intercom returned an invalid contact page: " + str(error),
                ),
            )


# Only these statuses prove Intercom applied nothing; any other non-2xx answer may have been applied.
_DEFINITE_REFUSAL_STATUSES = frozenset({400, 401, 403, 404, 411, 413, 414, 415, 422})

# The reply read-back tolerates a difference between the Worker's and Intercom's clocks.
_REPLY_RECONCILIATION_SKEW_SECONDS = 300


def _reply_problem(input: ReplyToConversationInput) -> str:
    if not _NUMERIC_ID.fullmatch(input.conversation_id):
        return "conversationId must be an Intercom conversation ID of digits, such as 215472658213"
    if not _NUMERIC_ID.fullmatch(input.admin_id):
        return (
            "adminId must be an Intercom admin ID of digits, such as 5017691; "
            "choose one with the adminPicker unit"
        )
    if input.message_type not in ("comment", "note"):
        return (
            f'messageType "{input.message_type}" must be comment, for a reply the customer sees, '
            "or note, for an internal note"
        )
    if not input.body.strip():
        return "body is required"
    if len(input.body.encode()) > 16384:
        return "body must be valid UTF-8 of at most 16384 bytes"
    return ""


def _reply_html(text: str) -> str:
    normalized = text.replace("\r\n", "\n").strip()
    paragraphs = re.split(r"\n[ \t]*\n", normalized)
    return "".join(
        "<p>"
        + html.escape(paragraph.strip(), quote=True)
        .replace("&quot;", "&#34;")
        .replace("&#x27;", "&#39;")
        .replace("\n", "<br>")
        + "</p>"
        for paragraph in paragraphs
        if paragraph.strip()
    )


def _plain_comparison(text: str) -> str:
    return " ".join(html.unescape(text).split())


def _find_reply_part(
    wire: dict[str, Any],
    input: ReplyToConversationInput,
    since: int,
    html_body: bool,
    check_text: bool = True,
) -> dict[str, Any] | None:
    candidates = []
    wanted = _plain_comparison(input.body)
    for part in _parts(wire):
        author = _object(part.get("author"))
        if (
            _string(part.get("part_type")) != input.message_type
            or _string(author.get("type")) != "admin"
            or _identifier(author.get("id")) != input.admin_id
            or _seconds(part.get("created_at")) < since
        ):
            continue
        text = _string(part.get("body"))
        if html_body:
            text = re.sub(r"<[^>]*>", " ", text)
        if not check_text or _plain_comparison(text) == wanted:
            candidates.append(part)
    if not candidates:
        return None
    return max(
        enumerate(candidates), key=lambda indexed: (_seconds(indexed[1].get("created_at")), indexed[0])
    )[1]


class ReplyToConversationOperation:
    """Send an admin reply under the send cap, reading back before every resend."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return immutable operation identity and defaults."""
        return REPLY_TO_CONVERSATION_DEFINITION

    def idempotency_key(self, call_id: CallID, input: ReplyToConversationInput) -> IdempotencyKey:
        """Correlate the mutation receipt with its stable Call ID."""
        return IdempotencyKey(str(call_id))

    async def invoke(
        self, call: Call, input: ReplyToConversationInput
    ) -> MutationAttempt[ReplyToConversationOutput]:
        """Fence the reply under the send cap, reading back before any resend."""
        operation = "replyToConversation"
        output = ReplyToConversationOutput(conversation_id=input.conversation_id)
        problem = _reply_problem(input)
        if problem:
            return MutationAttempt.branch_result(
                "defect", output, Receipt(), self.client.failure(operation, FailureKind.VALIDATION, problem)
            )
        try:
            fence = Fence(
                call,
                REPLY_TO_CONVERSATION_DEFINITION.step_defaults.execute_method_timeout,
                max_sends=call.max_sends,
            )
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect",
                output,
                Receipt(),
                self.client.failure(operation, FailureKind.LOCAL_DEFECT, str(error)),
            )
        decision = await fence.decide()
        if decision.kind == DecisionKind.FAIL_CLOSED:
            # FailClosed sends nothing, so a later attempt decides again from a readable checkpoint.
            return MutationAttempt.retry(
                self.client.failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "dispatch checkpoint could not be read; nothing was sent in this attempt",
                )
            )
        credentials, failure = await self.client.resolve_credentials(call, operation)
        if decision.kind in (DecisionKind.READ_BACK_REQUIRED, DecisionKind.READ_BACK_THEN_RESEND_ALLOWED):
            if failure:
                # defect would hide an earlier possible send; Intercom cannot tell a transient failure apart.
                return MutationAttempt.retry(failure)
            assert credentials is not None
            read_back = await self._reconcile(call, input, credentials)
            if decision.kind == DecisionKind.READ_BACK_REQUIRED or read_back.kind != "uncertain":
                return read_back
            # The read-back found no matching part, so this attempt resends below the cap.
        elif failure:
            if decision.prior_send_upper_bound > 0:
                return MutationAttempt.retry(failure)
            try:
                await fence.record_not_sent()
            except Exception:
                return MutationAttempt.retry(
                    self.client.failure(
                        operation,
                        FailureKind.AVAILABILITY,
                        "credential checkpoint could not be recorded; nothing was sent",
                    )
                )
            return MutationAttempt.branch_result("defect", output, Receipt(), failure)
        assert credentials is not None
        is_first_send = decision.prior_send_upper_bound == 0
        can_resend = decision.prior_send_upper_bound + 1 < call.max_sends
        dispatched_at = int(self.client.now().timestamp())
        try:
            exchange = await fence.dispatch(
                lambda _: self.client.exchange(
                    call,
                    credentials,
                    operation,
                    "POST",
                    "/conversations/" + quote(input.conversation_id, safe="-._~$&+=:@") + "/reply",
                    {
                        "message_type": input.message_type,
                        "type": "admin",
                        "admin_id": input.admin_id,
                        "body": _reply_html(input.body),
                    },
                )
            )
        except (CheckpointError, InsufficientAttemptTimeError, DispatchNotAllowedError):
            # Dispatch refused before sending, so nothing reached Intercom in this attempt.
            return MutationAttempt.retry(
                self.client.failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "the reply was not sent before the attempt deadline or checkpoint",
                )
            )
        except TimeoutError:
            # The claim stays, so the next attempt counts this send and reads back first.
            return MutationAttempt.retry(
                self.client.failure(
                    operation, FailureKind.TRANSPORT, "Intercom request failed before a response arrived"
                )
            )
        receipt = self.client.receipt(call, input.conversation_id, exchange.request_id)
        if exchange.outcome == "invalid":
            # An oversized or credential-reflecting 2xx may follow an applied reply, so it is read back now.
            return _resend_when_unconfirmed(await self._reconcile(call, input, credentials), can_resend)
        classified = await _classify_sent_reply(self.client, fence, is_first_send, exchange, output, receipt)
        if classified is not None:
            return classified
        try:
            wire = _envelope(exchange, input.conversation_id)
        except (ValueError, TypeError, OverflowError):
            return _resend_when_unconfirmed(await self._reconcile(call, input, credentials), can_resend)
        since = dispatched_at - _REPLY_RECONCILIATION_SKEW_SECONDS
        try:
            part_wire = _find_reply_part(wire, input, since, True)
            if part_wire is None:
                part_wire = _find_reply_part(wire, input, since, True, False)
        except (ValueError, TypeError, OverflowError):
            return _resend_when_unconfirmed(await self._reconcile(call, input, credentials), can_resend)
        state = _string(wire.get("state")) or None
        if part_wire is None:
            return MutationAttempt.branch_result(
                "replied", ReplyToConversationOutput(input.conversation_id, conversation_state=state), receipt
            )
        try:
            part = _part(part_wire)
        except (ValueError, TypeError, OverflowError) as error:
            return MutationAttempt.branch_result(
                "invalidResponse",
                ReplyToConversationOutput(input.conversation_id, conversation_state=state),
                receipt,
                self.client.failure(
                    operation,
                    FailureKind.PROTOCOL,
                    "Intercom accepted the reply but returned an invalid part: " + str(error),
                ),
            )
        return MutationAttempt.branch_result(
            "replied",
            ReplyToConversationOutput(input.conversation_id, part, state),
            replace(receipt, metadata={"partId": part.id}),
        )

    async def _reconcile(
        self, call: Call, input: ReplyToConversationInput, credentials: Any
    ) -> MutationAttempt[ReplyToConversationOutput]:
        """Never send; report an earlier reply found in the conversation, or uncertain when unconfirmed."""
        operation = "replyToConversation"
        output = ReplyToConversationOutput(conversation_id=input.conversation_id)
        exchange = await self.client.read_conversation(call, credentials, operation, input.conversation_id)
        receipt = self.client.receipt(call, input.conversation_id, exchange.request_id)
        if _retryable(exchange):
            assert exchange.failure is not None
            return MutationAttempt.retry(exchange.failure, exchange.retry_after.total_seconds())
        if exchange.outcome != "succeeded":
            assert exchange.failure is not None
            return MutationAttempt.uncertain(
                output,
                self.client.failure(
                    operation,
                    exchange.failure.kind,
                    "an earlier attempt of this Step may have sent the reply, and the conversation "
                    "could not be read back: " + exchange.failure.message,
                ),
                receipt,
            )
        try:
            wire = _envelope(exchange, input.conversation_id)
            output = ReplyToConversationOutput(
                input.conversation_id, conversation_state=_string(wire.get("state")) or None
            )
            # Every send of this Step happened after its first attempt began.
            first_attempt_at = call.context.first_attempt_at if call.context is not None else None
            since = (
                int(first_attempt_at.timestamp()) - _REPLY_RECONCILIATION_SKEW_SECONDS
                if first_attempt_at is not None
                else 0
            )
            part_wire = _find_reply_part(wire, input, since, False)
        except (ValueError, TypeError, OverflowError) as error:
            return MutationAttempt.uncertain(
                output,
                self.client.failure(
                    operation,
                    FailureKind.PROTOCOL,
                    "an earlier attempt of this Step may have sent the reply, and the conversation "
                    "read back is invalid: " + str(error),
                ),
                receipt,
            )
        if part_wire is None:
            return MutationAttempt.uncertain(
                output,
                self.client.failure(
                    operation,
                    FailureKind.TRANSPORT,
                    "an earlier attempt of this Step may have sent the reply without a confirmed outcome, "
                    "and the conversation shows no matching part",
                ),
                receipt,
            )
        try:
            part = _part(part_wire)
        except (ValueError, TypeError, OverflowError) as error:
            return MutationAttempt.branch_result(
                "invalidResponse",
                output,
                receipt,
                self.client.failure(
                    operation,
                    FailureKind.PROTOCOL,
                    "the conversation shows the reply, but its part is invalid: " + str(error),
                ),
            )
        return MutationAttempt.branch_result(
            "replied",
            ReplyToConversationOutput(input.conversation_id, part, output.conversation_state, True),
            replace(receipt, metadata={"partId": part.id}),
        )


def _resend_when_unconfirmed(
    read_back: MutationAttempt[ReplyToConversationOutput], can_resend: bool
) -> MutationAttempt[ReplyToConversationOutput]:
    """Retry an unconfirmed read-back while the send cap allows another send."""
    if read_back.kind == "uncertain" and can_resend:
        assert read_back.failure is not None
        # The claim stays the last marker, so the next attempt reads back again before it resends.
        return MutationAttempt.retry(read_back.failure)
    return read_back


async def _classify_sent_reply(
    client: Client,
    fence: Fence,
    is_first_send: bool,
    exchange: Exchange,
    output: ReplyToConversationOutput,
    receipt: Receipt,
) -> MutationAttempt[ReplyToConversationOutput] | None:
    """Map a sent reply; any outcome but a definite refusal retries, and the next attempt reads back."""
    if exchange.outcome == "succeeded":
        return None
    assert exchange.failure is not None
    if exchange.outcome in ("notSent", "rateLimited") or (
        exchange.outcome == "unconfirmed" and exchange.status == 503
    ):
        try:
            await fence.record_can_resend()
        except Exception:
            pass  # A lost checkpoint leaves the claim, so the next attempt counts this send and reads back.
    branch = ""
    if exchange.outcome == "notFound" and exchange.status == 404:
        branch = "notFound"
    elif exchange.outcome == "defect" and is_first_send:
        branch = "defect"
    elif exchange.outcome == "rejected" and exchange.status in _DEFINITE_REFUSAL_STATUSES:
        branch = "providerRejected"
    if not branch:
        # A 3xx, 408, 409, 410, 5xx, unlisted status, or lost answer may follow an applied reply.
        return MutationAttempt.retry(exchange.failure, exchange.retry_after.total_seconds())
    if not is_first_send:
        return MutationAttempt.uncertain(output, exchange.failure, receipt)
    try:
        await fence.record_can_resend()
    except Exception:
        return MutationAttempt.retry(
            client.failure(
                "replyToConversation", FailureKind.AVAILABILITY, "refusal checkpoint could not be recorded"
            )
        )
    return MutationAttempt.branch_result(
        branch, output, Receipt() if branch == "defect" else receipt, exchange.failure
    )


class UpdateConversationStateOperation:
    """Read, conditionally write, and reconcile one state change."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return immutable operation identity and defaults."""
        return UPDATE_CONVERSATION_STATE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpdateConversationStateInput) -> IdempotencyKey:
        """Correlate the mutation receipt with its stable Call ID."""
        return IdempotencyKey(str(call_id))

    async def invoke(
        self, call: Call, input: UpdateConversationStateInput
    ) -> MutationAttempt[UpdateConversationStateOutput]:
        """Read first and write only when the requested state differs."""
        operation = "updateConversationState"
        empty = _zero_update()
        problem = (
            "conversationId must be an Intercom conversation ID of digits, such as 215472658213"
            if not _NUMERIC_ID.fullmatch(input.conversation_id)
            else (
                "adminId must be an Intercom admin ID of digits, such as 5017691; "
                "choose one with the adminPicker unit"
            )
            if not _NUMERIC_ID.fullmatch(input.admin_id)
            else f'state "{input.state}" is not an Intercom conversation state; use open, closed, or snoozed'
            if input.state not in ("open", "closed", "snoozed")
            else ""
        )
        snoozed_until = 0
        if not problem:
            if input.state != "snoozed" and input.snoozed_until:
                problem = "snoozedUntil is only used with state snoozed"
            elif input.state == "snoozed":
                snoozed_until = _rfc3339(input.snoozed_until) or 0
                if snoozed_until <= 0:
                    problem = (
                        "snoozedUntil must be an RFC 3339 instant with an explicit offset, "
                        "such as 2026-02-01T09:00:00Z, for state snoozed"
                    )
        if problem:
            return MutationAttempt.branch_result(
                "defect", empty, Receipt(), self.client.failure(operation, FailureKind.VALIDATION, problem)
            )
        credentials, failure = await self.client.resolve_credentials(call, operation)
        if failure:
            return MutationAttempt.branch_result("defect", empty, Receipt(), failure)
        assert credentials is not None
        first = await self.client.read_conversation(call, credentials, operation, input.conversation_id)
        bad = _mutation_failure(self.client, call, first, empty, input.conversation_id)
        if bad:
            return bad
        first_receipt = self.client.receipt(call, input.conversation_id, first.request_id)
        try:
            current = _conversation(_envelope(first, input.conversation_id))
        except (ValueError, TypeError, OverflowError) as error:
            return MutationAttempt.branch_result(
                "invalidResponse",
                empty,
                first_receipt,
                self.client.failure(
                    operation,
                    FailureKind.PROTOCOL,
                    "Intercom returned an invalid conversation: " + str(error),
                ),
            )
        if _state_matches(current, input.state, snoozed_until):
            return MutationAttempt.branch_result(
                "updated",
                UpdateConversationStateOutput(current, True),
                self.client.receipt(call, input.conversation_id),
            )
        payload: dict[str, Any] = {
            "message_type": "close"
            if input.state == "closed"
            else input.state
            if input.state == "snoozed"
            else "open",
            "admin_id": input.admin_id,
        }
        if input.state == "closed":
            payload["type"] = "admin"
        if input.state == "snoozed":
            payload["snoozed_until"] = snoozed_until
        write = await self.client.exchange(
            call,
            credentials,
            operation,
            "POST",
            "/conversations/" + quote(input.conversation_id, safe="-._~$&+=:@") + "/parts",
            payload,
        )
        write_receipt = self.client.receipt(call, input.conversation_id, write.request_id)
        if write.outcome == "rejected" and write.status in _DEFINITE_REFUSAL_STATUSES:
            reread = await self.client.read_conversation(call, credentials, operation, input.conversation_id)
            if reread.outcome != "succeeded":
                # A failed reread retries, because the absolute change is safe to repeat.
                assert reread.failure is not None
                return MutationAttempt.retry(reread.failure, reread.retry_after.total_seconds())
            try:
                after = _conversation(_envelope(reread, input.conversation_id))
            except (ValueError, TypeError, OverflowError) as error:
                return MutationAttempt.retry(
                    self.client.failure(
                        operation,
                        FailureKind.PROTOCOL,
                        "Intercom returned an invalid conversation when read again after rejecting "
                        "the change: " + str(error),
                    )
                )
            if _state_matches(after, input.state, snoozed_until):
                return MutationAttempt.branch_result(
                    "updated", UpdateConversationStateOutput(after, True), write_receipt
                )
            return MutationAttempt.branch_result(
                "providerRejected", UpdateConversationStateOutput(after), write_receipt, write.failure
            )
        if write.outcome == "notFound" and write.status == 404:
            return MutationAttempt.branch_result(
                "notFound", UpdateConversationStateOutput(current), write_receipt, write.failure
            )
        if write.outcome == "defect":
            # The request could not be built, so nothing was sent.
            return MutationAttempt.branch_result(
                "defect", UpdateConversationStateOutput(current), write_receipt, write.failure
            )
        if write.outcome != "succeeded":
            # A lost answer, 3xx, 408, 409, 410, 5xx, or unlisted status may follow an applied change.
            assert write.failure is not None
            return MutationAttempt.retry(write.failure, write.retry_after.total_seconds())
        try:
            updated = _conversation(_envelope(write, input.conversation_id))
            if not _state_matches(updated, input.state, snoozed_until):
                raise ValueError("conversation returned after the change is not in the requested state")
        except (ValueError, TypeError, OverflowError) as error:
            # The change is absolute, so the next attempt reads again and writes only if still needed.
            return MutationAttempt.retry(
                self.client.failure(
                    operation,
                    FailureKind.PROTOCOL,
                    "Intercom returned an invalid conversation after the change: " + str(error),
                )
            )
        return MutationAttempt.branch_result("updated", UpdateConversationStateOutput(updated), write_receipt)


def _state_matches(conversation: Conversation, state: str, snoozed_until: int) -> bool:
    return conversation.state == state and (
        state != "snoozed"
        or conversation.snoozed_until is not None
        and int(conversation.snoozed_until.timestamp()) == snoozed_until
    )
