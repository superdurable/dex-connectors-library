# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Freshdesk ticket operations and Go-compatible provider decoding."""

import html
import ipaddress
import json
import re
from dataclasses import dataclass, field, replace
from datetime import UTC, date, datetime, timedelta
from email.utils import parseaddr
from typing import Any, TypeVar

from dex_connectors.sdk import (
    Call,
    CallID,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    QueryAttempt,
    QueryDefinition,
    Receipt,
)

from ._generated import definitions as defs
from .client import Client, Exchange
from .models import (
    AddNoteInput,
    AddNoteOutput,
    CreateTicketInput,
    CreateTicketOutput,
    GetTicketInput,
    SearchTicketsInput,
    SearchTicketsOutput,
    Ticket,
    TicketConversation,
    TicketDetails,
    TicketRequester,
    UpdateTicketInput,
    UpdateTicketOutput,
)

_TAG = re.compile(r"^[A-Za-z0-9](?:[A-Za-z0-9 _./+-]{0,62}[A-Za-z0-9_./+-])?$")
_TIME = re.compile(
    r"^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:[.,][0-9]+)?(?:Z|[+-][0-9]{2}:[0-9]{2})$"
)
Output = TypeVar("Output")


@dataclass(frozen=True, slots=True)
class _Checkpoint:
    dispatched_call_id: str = field(metadata={"dex_json_name": "freshdeskDispatchedCallId"})
    dispatched_attempt: int = field(
        default=0, metadata={"dex_json_name": "freshdeskDispatchedAttempt", "dex_json_omit_default": True}
    )
    retry_attempt: int = field(
        default=0, metadata={"dex_json_name": "freshdeskRetryAttempt", "dex_json_omit_default": True}
    )


def _zero_ticket() -> Ticket:
    return Ticket(status=0, priority=0)


def _zero_conversation() -> TicketConversation:
    return TicketConversation(source=0)


def _integer(value: Any, default: int = 0, field_name: str = "numeric field") -> int:
    if value is None:
        return default
    if type(value) is not int or not -(1 << 63) <= value < (1 << 63):
        raise ValueError(f"invalid Freshdesk {field_name}")
    return value


def _string(value: Any) -> str:
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError("invalid Freshdesk text field")
    return value


def _boolean(value: Any) -> bool:
    if value is None:
        return False
    if type(value) is not bool:
        raise ValueError("invalid Freshdesk boolean field")
    return value


def _timestamp(value: Any, field_name: str = "timestamp") -> datetime:
    raw = _string(value)
    if not _TIME.fullmatch(raw):
        raise ValueError(f"invalid Freshdesk {field_name} timestamp")
    date_part, time_part = raw.split("T", 1)
    year, month, day = (int(part) for part in date_part.split("-"))
    hour, minute, second = (int(part) for part in time_part[:8].split(":"))
    if hour > 23 or minute > 59 or second > 59:
        raise ValueError(f"invalid Freshdesk {field_name} timestamp")
    zone = time_part[8:]
    fraction = ""
    if zone.startswith((".", ",")):
        fraction = zone[1:].split("+", 1)[0].split("-", 1)[0].rstrip("Z")
        zone = zone[1 + len(fraction) :]
    nanoseconds = int((fraction + "000000000")[:9]) if fraction else 0
    if nanoseconds % 1000 or any(digit != "0" for digit in fraction[9:]):
        raise ValueError(
            f"invalid Freshdesk {field_name} timestamp: nanoseconds cannot be represented exactly"
        )
    microseconds = nanoseconds // 1000
    try:
        parsed = datetime(year, month, day, hour, minute, second, microseconds, tzinfo=UTC)
    except ValueError as error:
        raise ValueError(f"invalid Freshdesk {field_name} timestamp") from error
    if zone != "Z":
        sign = 1 if zone[0] == "+" else -1
        offset_hours, offset_minutes = (int(part) for part in zone[1:].split(":"))
        if offset_hours > 24 or offset_minutes > 60:
            raise ValueError(f"invalid Freshdesk {field_name} timestamp")
        try:
            parsed -= sign * timedelta(hours=offset_hours, minutes=offset_minutes)
        except OverflowError as error:
            raise ValueError(f"invalid Freshdesk {field_name} timestamp") from error
    return parsed


def _truncated(value: str) -> tuple[str, bool]:
    encoded = value.encode()
    if len(encoded) <= 16384:
        return value, False
    return encoded[:16384].decode("utf-8", "ignore"), True


def _object(body: bytes | Any) -> dict[str, Any]:
    value = json.loads(body) if isinstance(body, bytes) else body
    if not isinstance(value, dict):
        raise ValueError("Freshdesk response is not an object")
    return value


def _ticket(value: Any, expected_id: int = 0) -> Ticket:
    wire = _object(value)
    identifier = _integer(wire.get("id"), field_name="id")
    status = _integer(wire.get("status"), field_name="status")
    if identifier < 1 or status < 1 or (expected_id and identifier != expected_id):
        raise ValueError("ticket ID or status is invalid")
    tags = wire.get("tags")
    if tags is not None and (not isinstance(tags, list) or any(not isinstance(tag, str) for tag in tags)):
        raise ValueError("ticket tags are invalid")
    description, truncated = _truncated(_string(wire.get("description_text")))
    return Ticket(
        id=identifier,
        status=status,
        priority=_integer(wire.get("priority"), field_name="priority"),
        subject=_string(wire.get("subject")),
        description=description,
        is_description_truncated=truncated,
        source=_integer(wire.get("source"), field_name="source") or None,
        type=_string(wire.get("type")),
        requester_id=_integer(wire.get("requester_id"), field_name="requester_id"),
        responder_id=_integer(wire.get("responder_id"), field_name="responder_id"),
        group_id=_integer(wire.get("group_id"), field_name="group_id"),
        company_id=_integer(wire.get("company_id"), field_name="company_id"),
        product_id=_integer(wire.get("product_id"), field_name="product_id"),
        tags=tuple(tags or ()),
        is_escalated=_boolean(wire.get("is_escalated")),
        is_spam=_boolean(wire.get("spam")),
        created_at=_timestamp(wire.get("created_at"), "created_at"),
        updated_at=_timestamp(wire.get("updated_at"), "updated_at"),
        due_by=_timestamp(wire["due_by"], "due_by")
        if wire.get("due_by") is not None and wire.get("due_by") != ""
        else None,
        first_response_due_by=_timestamp(wire["fr_due_by"], "fr_due_by")
        if wire.get("fr_due_by") is not None and wire.get("fr_due_by") != ""
        else None,
    )


def _conversation(value: Any) -> TicketConversation:
    wire = _object(value)
    identifier = _integer(wire.get("id"), field_name="id")
    if identifier < 1:
        raise ValueError("conversation ID is missing")
    body, truncated = _truncated(_string(wire.get("body_text")))
    return TicketConversation(
        id=identifier,
        ticket_id=_integer(wire.get("ticket_id"), field_name="ticket_id"),
        user_id=_integer(wire.get("user_id"), field_name="user_id"),
        source=_integer(wire.get("source"), field_name="source"),
        is_private=_boolean(wire.get("private")),
        is_incoming=_boolean(wire.get("incoming")),
        body=body,
        is_body_truncated=truncated,
        created_at=_timestamp(wire.get("created_at"), "created_at"),
    )


def _validate_email(value: str) -> None:
    name, address = parseaddr(value)
    local = value.partition("@")[0]
    domain = value.partition("@")[2]
    if domain.startswith("[") and domain.endswith("]"):
        try:
            ipaddress.IPv4Address(domain[1:-1])
            address = value
        except ipaddress.AddressValueError:
            pass
    if (
        name
        or address != value
        or not local
        or local.startswith(".")
        or local.endswith(".")
        or ".." in local
        or any(char in value for char in " \"'()<>,;")
    ):
        raise ValueError("email must be one bare address")


def _validate_tags(tags: tuple[str, ...]) -> None:
    seen: set[str] = set()
    for tag in tags:
        if not _TAG.fullmatch(tag) or len(tag.encode()) > 64 or tag.lower() in seen:
            raise ValueError("Freshdesk tag is invalid or duplicated")
        seen.add(tag.lower())


def _tags_equal_fold(left: str, right: str) -> bool:
    return len(left) == len(right) and left.casefold() == right.casefold()


def _filter(input: SearchTicketsInput) -> str:
    clauses = []
    for values, prefix, minimum, maximum in (
        (input.statuses, "status", 2, None),
        (input.priorities, "priority", 1, 4),
    ):
        if any(
            type(item) is not int or item < minimum or (maximum and item > maximum) for item in values
        ) or len(set(values)) != len(values):
            raise ValueError("Freshdesk status or priority is invalid")
        terms = [f"{prefix}:{item}" for item in values]
        if terms:
            clauses.append(terms[0] if len(terms) == 1 else "(" + " OR ".join(terms) + ")")
    _validate_tags(input.tags)
    terms = [f"tag:'{tag}'" for tag in input.tags]
    if terms:
        clauses.append(terms[0] if len(terms) == 1 else "(" + " OR ".join(terms) + ")")
    if input.updated_since_date:
        try:
            if date.fromisoformat(input.updated_since_date).isoformat() != input.updated_since_date:
                raise ValueError
        except ValueError as error:
            raise ValueError("updatedSinceDate is invalid") from error
        clauses.append(f"updated_at:>'{input.updated_since_date}'")
    if not clauses:
        raise ValueError("at least one Freshdesk filter is required")
    result = " AND ".join(clauses)
    if len(result.encode()) + 2 > 512:
        raise ValueError("Freshdesk filter is too long")
    return result


def _html(value: str) -> str:
    return (
        html.escape(value.replace("\r\n", "\n"), quote=True)
        .replace("&quot;", "&#34;")
        .replace("&#x27;", "&#39;")
        .replace("\n", "<br>")
    )


def _write_budget(call: Call, execute_timeout_seconds: float) -> float:
    deadline = getattr(call.context, "deadline", None)
    available = execute_timeout_seconds
    if deadline is not None:
        available = min(available, (deadline - datetime.now(UTC)).total_seconds())
    return max(available, 0) / 10


def _query_terminal(
    client: Client, call: Call, exchange: Exchange, zero: Output, not_found: str, object_id: int = 0
) -> QueryAttempt[Output] | None:
    if exchange.outcome == "succeeded":
        return None
    if exchange.outcome in ("rateLimited", "notSent", "unavailable", "serviceUnavailable"):
        assert exchange.failure is not None
        return QueryAttempt.retry(exchange.failure, exchange.retry_after)
    branch = (
        not_found
        if exchange.outcome == "notFound"
        else "invalidResponse"
        if exchange.outcome == "invalid"
        else "defect"
        if exchange.outcome == "defect"
        else "providerRejected"
    )
    return QueryAttempt.branch_result(
        branch, zero, client.receipt(call, exchange, object_id), exchange.failure
    )


def _mutation_terminal(
    client: Client, call: Call, exchange: Exchange, zero: Output, object_id: int = 0
) -> MutationAttempt[Output] | None:
    if exchange.outcome == "succeeded":
        return None
    if exchange.outcome in ("rateLimited", "notSent", "unavailable", "serviceUnavailable"):
        assert exchange.failure is not None
        return MutationAttempt.retry(exchange.failure, exchange.retry_after)
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
        branch, zero, client.receipt(call, exchange, object_id), exchange.failure
    )


class SearchTicketsOperation:
    """Search one Freshdesk filter page, optionally matching a contact email."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return stable manifest metadata."""
        return defs.SEARCH_TICKETS_DEFINITION

    async def invoke(self, call: Call, input: SearchTicketsInput) -> QueryAttempt[SearchTicketsOutput]:
        """Read contacts when requested, then the filter page."""
        operation, zero = "searchTickets", SearchTicketsOutput()
        try:
            filter_query = _filter(input)
            if input.page < 0 or input.page > 10:
                raise ValueError("page is invalid")
            if input.requester_email:
                _validate_email(input.requester_email)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", zero, Receipt(), self.client.failure(operation, "VALIDATION", str(error))
            )
        key, failure, transient = await self.client.resolve_credentials(call, operation)
        if failure:
            return (
                QueryAttempt.retry(failure)
                if transient
                else QueryAttempt.branch_result("defect", zero, Receipt(), failure)
            )
        requester_ids = None
        if input.requester_email:
            contact = await self.client.exchange(
                call, operation, key, "GET", "/contacts", {"email": input.requester_email}
            )
            terminal = _query_terminal(self.client, call, contact, zero, "providerRejected")
            if terminal:
                return terminal
            try:
                rows = json.loads(contact.body)
                if not isinstance(rows, list):
                    raise ValueError
                requester_ids = set()
                for row in rows:
                    item = _object(row)
                    identifier = _integer(item.get("id"), field_name="contact id")
                    if identifier < 1:
                        raise ValueError
                    _string(item.get("name"))
                    if _string(item.get("email")).casefold() == input.requester_email.casefold():
                        requester_ids.add(identifier)
            except (ValueError, TypeError) as error:
                return QueryAttempt.branch_result(
                    "invalidResponse",
                    zero,
                    self.client.receipt(call, contact),
                    self.client.failure(
                        operation, "PROTOCOL", f"Freshdesk returned an invalid contact list: {error}"
                    ),
                )
            if not requester_ids:
                return QueryAttempt.branch_result("searched", zero, self.client.receipt(call, contact))
        page = input.page or 1
        result = await self.client.exchange(
            call, operation, key, "GET", "/search/tickets", {"query": f'"{filter_query}"', "page": str(page)}
        )
        terminal = _query_terminal(self.client, call, result, zero, "providerRejected")
        if terminal:
            return terminal
        try:
            wire = _object(result.body)
            total = _integer(wire.get("total"), -1, "total")
            rows = wire.get("results")
            if total < 0 or not isinstance(rows, list) or len(rows) > 30:
                raise ValueError
            tickets = []
            for row in rows:
                ticket = _ticket(row)
                if requester_ids is None or ticket.requester_id in requester_ids:
                    tickets.append(replace(ticket, description="", is_description_truncated=False))
            output = SearchTicketsOutput(
                tuple(tickets), total, page + 1 if page < 10 and page * 30 < total else 0
            )
        except (ValueError, TypeError) as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                zero,
                self.client.receipt(call, result),
                self.client.failure(
                    operation, "PROTOCOL", f"Freshdesk returned an invalid search page: {error}"
                ),
            )
        return QueryAttempt.branch_result("searched", output, self.client.receipt(call, result))


class GetTicketOperation:
    """Read one ticket, its requester, and its first conversations."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return stable manifest metadata."""
        return defs.GET_TICKET_DEFINITION

    async def invoke(self, call: Call, input: GetTicketInput) -> QueryAttempt[TicketDetails]:
        """Perform bounded ticket and conversation reads."""
        operation, zero = "getTicket", TicketDetails(_zero_ticket())
        if input.ticket_id < 1 or input.conversation_limit < 0 or input.conversation_limit > 20:
            return QueryAttempt.branch_result(
                "defect",
                zero,
                Receipt(),
                self.client.failure(operation, "VALIDATION", "ticketId or conversationLimit is invalid"),
            )
        key, failure, transient = await self.client.resolve_credentials(call, operation)
        if failure:
            return (
                QueryAttempt.retry(failure)
                if transient
                else QueryAttempt.branch_result("defect", zero, Receipt(), failure)
            )
        first = await self.client.exchange(
            call, operation, key, "GET", f"/tickets/{input.ticket_id}", {"include": "requester"}
        )
        terminal = _query_terminal(self.client, call, first, zero, "notFound", input.ticket_id)
        if terminal:
            return terminal
        try:
            wire = _object(first.body)
            ticket = _ticket(wire, input.ticket_id)
            requester = None
            if wire.get("requester") is not None:
                person = _object(wire["requester"])
                identifier = _integer(person.get("id"), field_name="requester id")
                if identifier > 0:
                    if identifier != ticket.requester_id:
                        raise ValueError
                    requester = TicketRequester(
                        identifier, _string(person.get("name")), _string(person.get("email"))
                    )
        except (ValueError, TypeError) as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                zero,
                self.client.receipt(call, first, input.ticket_id),
                self.client.failure(operation, "PROTOCOL", f"Freshdesk returned an invalid ticket: {error}"),
            )
        limit = input.conversation_limit or 10
        second = await self.client.exchange(
            call,
            operation,
            key,
            "GET",
            f"/tickets/{input.ticket_id}/conversations",
            {"per_page": str(limit), "page": "1"},
        )
        terminal = _query_terminal(self.client, call, second, zero, "notFound", input.ticket_id)
        if terminal:
            return terminal
        try:
            rows = json.loads(second.body)
            if not isinstance(rows, list) or len(rows) > limit:
                raise ValueError
            conversations = tuple(
                sorted((_conversation(row) for row in rows), key=lambda item: (item.created_at, item.id))
            )
            details = TicketDetails(
                ticket,
                requester,
                conversations,
                bool(second.headers and 'rel="next"' in second.headers.get("Link", "")),
            )
        except (ValueError, TypeError) as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                zero,
                self.client.receipt(call, second, input.ticket_id),
                self.client.failure(
                    operation, "PROTOCOL", f"Freshdesk returned an invalid conversation page: {error}"
                ),
            )
        return QueryAttempt.branch_result(
            "found", details, self.client.receipt(call, second, input.ticket_id)
        )


def _validate_write_text(value: str) -> None:
    if not value.strip():
        raise ValueError("Freshdesk text is required")


def _validate_status_priority(status: int | None, priority: int | None) -> None:
    if status not in (None, 0) and (type(status) is not int or status < 2):
        raise ValueError("Freshdesk status is invalid")
    if priority not in (None, 0) and (type(priority) is not int or not 1 <= priority <= 4):
        raise ValueError("Freshdesk priority is invalid")


async def _single_dispatch_before(
    client: Client, call: Call, operation: str, zero: Output
) -> MutationAttempt[Output] | None:
    context = call.context
    if context is None:
        return MutationAttempt.uncertain(
            zero,
            client.failure(
                operation,
                "TRANSPORT",
                "an earlier attempt of this Step may have sent the request, so it is not sent again",
            ),
            client.receipt(call),
        )
    try:
        if context.has_last_heartbeat_value():
            marker = context.get_last_heartbeat_value(_Checkpoint)
            if (
                marker is None
                or marker.dispatched_call_id != str(call.id)
                or marker.retry_attempt != context.attempt
            ):
                raise ValueError
        elif context.attempt != 1:
            raise ValueError
    except (ValueError, TypeError, AttributeError):
        return MutationAttempt.uncertain(
            zero,
            client.failure(
                operation,
                "TRANSPORT",
                "an earlier attempt of this Step may have sent the request, so it is not sent again",
            ),
            client.receipt(call),
        )
    return None


async def _permit_retry(call: Call) -> None:
    if call.context is None:
        return
    try:
        await call.context.heartbeat(_Checkpoint(str(call.id), retry_attempt=call.context.attempt + 1))
    except Exception:
        pass  # A failed checkpoint deliberately makes the next attempt fail closed.


async def _record_dispatch(
    client: Client, call: Call, operation: str, zero: Output
) -> MutationAttempt[Output] | None:
    if call.context is None:
        return MutationAttempt.retry(
            client.failure(
                operation, "AVAILABILITY", "Dex did not record the dispatch checkpoint; nothing was sent"
            )
        )
    try:
        await call.context.heartbeat(_Checkpoint(str(call.id), dispatched_attempt=call.context.attempt))
    except Exception:
        await _permit_retry(call)
        return MutationAttempt.retry(
            client.failure(
                operation, "AVAILABILITY", "Dex did not record the dispatch checkpoint; nothing was sent"
            )
        )
    return None


async def _single_dispatch_exchange(
    client: Client,
    call: Call,
    operation: str,
    exchange: Exchange,
    zero: Output,
    not_found: str = "providerRejected",
) -> MutationAttempt[Output] | None:
    if exchange.outcome == "succeeded":
        return None
    if exchange.outcome in ("rateLimited", "notSent", "serviceUnavailable"):
        await _permit_retry(call)
        assert exchange.failure is not None
        return MutationAttempt.retry(exchange.failure, exchange.retry_after)
    receipt = client.receipt(call, exchange)
    if exchange.outcome in ("unavailable", "invalid"):
        assert exchange.failure is not None
        return MutationAttempt.uncertain(zero, exchange.failure, receipt)
    branch = (
        not_found
        if exchange.outcome == "notFound"
        else "defect"
        if exchange.outcome == "defect"
        else "providerRejected"
    )
    return MutationAttempt.branch_result(branch, zero, receipt, exchange.failure)


class CreateTicketOperation:
    """Create one Freshdesk ticket under a heartbeat dispatch checkpoint."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return stable manifest metadata."""
        return defs.CREATE_TICKET_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreateTicketInput) -> IdempotencyKey:
        """Use the Call ID as receipt correlation, never as a provider replay key."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: CreateTicketInput) -> MutationAttempt[CreateTicketOutput]:
        """Send the ticket once, retrying only proven safe outcomes."""
        operation, zero = "createTicket", CreateTicketOutput(_zero_ticket())
        write_budget = _write_budget(
            call, self.definition().step_defaults.execute_method_timeout.total_seconds()
        )
        requester_email = (
            input.requester["email"] if isinstance(input.requester, dict) else input.requester.email
        )
        requester_name = (
            input.requester.get("name", "") if isinstance(input.requester, dict) else input.requester.name
        )
        try:
            _validate_write_text(input.subject)
            _validate_write_text(input.description)
            _validate_email(requester_email)
            if requester_name != requester_name.strip():
                raise ValueError("requester name has surrounding spaces")
            _validate_status_priority(input.status, input.priority)
            _validate_tags(input.tags)
            if input.group_id < 0 or input.responder_id < 0:
                raise ValueError("ticket assignment ID is invalid")
            if input.type and (input.type != input.type.strip() or len(input.type.encode()) > 255):
                raise ValueError("ticket type is invalid")
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", zero, Receipt(), self.client.failure(operation, "VALIDATION", str(error))
            )
        terminal = await _single_dispatch_before(self.client, call, operation, zero)
        if terminal:
            return terminal
        key, failure, transient = await self.client.resolve_credentials(call, operation)
        if failure:
            if transient or call.context and call.context.attempt > 1:
                await _permit_retry(call)
                return MutationAttempt.retry(failure)
            return MutationAttempt.branch_result("defect", zero, Receipt(), failure)
        terminal = await _record_dispatch(self.client, call, operation, zero)
        if terminal:
            return terminal
        payload: dict[str, Any] = {
            "subject": input.subject,
            "description": _html(input.description),
            "email": requester_email,
        }
        for name, value in (
            ("name", requester_name),
            ("status", input.status),
            ("priority", input.priority),
            ("type", input.type),
            ("tags", list(input.tags)),
            ("group_id", input.group_id),
            ("responder_id", input.responder_id),
        ):
            if value:
                payload[name] = value
        exchange = await self.client.exchange(
            call, operation, key, "POST", "/tickets", payload=payload, write_budget=write_budget
        )
        terminal = await _single_dispatch_exchange(self.client, call, operation, exchange, zero)
        if terminal:
            return terminal
        try:
            ticket = _ticket(exchange.body)
        except (ValueError, TypeError) as error:
            return MutationAttempt.uncertain(
                zero,
                self.client.failure(
                    operation,
                    "PROTOCOL",
                    f"Freshdesk accepted the ticket but returned an invalid ticket: {error}",
                ),
                self.client.receipt(call, exchange),
            )
        return MutationAttempt.branch_result(
            "created", CreateTicketOutput(ticket), self.client.receipt(call, exchange, ticket.id)
        )


class UpdateTicketOperation:
    """Set absolute ticket fields after a read, safely repeatable on retry."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return stable manifest metadata."""
        return defs.UPDATE_TICKET_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpdateTicketInput) -> IdempotencyKey:
        """Correlate the read and absolute replacement write."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: UpdateTicketInput) -> MutationAttempt[UpdateTicketOutput]:
        """Read the ticket, then write only requested differences."""
        operation, zero = "updateTicket", UpdateTicketOutput(_zero_ticket())
        try:
            if input.ticket_id < 1:
                raise ValueError("ticketId is invalid")
            _validate_status_priority(input.status, input.priority)
            if input.group_id < 0 or input.responder_id < 0:
                raise ValueError("ticket assignment ID is invalid")
            _validate_tags(input.add_tags)
            _validate_tags(input.remove_tags)
            if any(
                _tags_equal_fold(added, removed) for added in input.add_tags for removed in input.remove_tags
            ):
                raise ValueError("tag appears in both addTags and removeTags")
            if not any(
                (
                    input.status,
                    input.priority,
                    input.group_id,
                    input.responder_id,
                    input.add_tags,
                    input.remove_tags,
                )
            ):
                raise ValueError("at least one ticket change is required")
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", zero, Receipt(), self.client.failure(operation, "VALIDATION", str(error))
            )
        key, failure, transient = await self.client.resolve_credentials(call, operation)
        if failure:
            return (
                MutationAttempt.retry(failure)
                if transient
                else MutationAttempt.branch_result("defect", zero, Receipt(), failure)
            )
        path = f"/tickets/{input.ticket_id}"
        first = await self.client.exchange(call, operation, key, "GET", path)
        terminal = _mutation_terminal(self.client, call, first, zero, input.ticket_id)
        if terminal:
            return terminal
        try:
            current = _ticket(first.body, input.ticket_id)
        except (ValueError, TypeError) as error:
            return MutationAttempt.branch_result(
                "invalidResponse",
                zero,
                self.client.receipt(call, first, input.ticket_id),
                self.client.failure(operation, "PROTOCOL", f"Freshdesk returned an invalid ticket: {error}"),
            )
        current_output = UpdateTicketOutput(current)
        payload: dict[str, Any] = {}
        for name, wanted, actual in (
            ("status", input.status, current.status),
            ("priority", input.priority, current.priority),
            ("group_id", input.group_id, current.group_id),
            ("responder_id", input.responder_id, current.responder_id),
        ):
            if wanted and wanted != actual:
                payload[name] = wanted
        tags = [
            tag
            for tag in current.tags
            if not any(_tags_equal_fold(tag, removed) for removed in input.remove_tags)
        ]
        for tag in input.add_tags:
            if not any(_tags_equal_fold(tag, existing) for existing in tags):
                tags.append(tag)
        if tuple(tags) != current.tags:
            payload["tags"] = tags
        if not payload:
            return MutationAttempt.branch_result(
                "updated",
                UpdateTicketOutput(current, True),
                self.client.receipt(call, first, input.ticket_id),
            )
        second = await self.client.exchange(call, operation, key, "PUT", path, payload=payload)
        terminal = _mutation_terminal(self.client, call, second, current_output, input.ticket_id)
        if terminal:
            return terminal
        try:
            updated = _ticket(second.body, input.ticket_id)
        except (ValueError, TypeError) as error:
            return MutationAttempt.branch_result(
                "invalidResponse",
                current_output,
                self.client.receipt(call, second, input.ticket_id),
                self.client.failure(
                    operation, "PROTOCOL", f"Freshdesk returned an invalid updated ticket: {error}"
                ),
            )
        return MutationAttempt.branch_result(
            "updated", UpdateTicketOutput(updated), self.client.receipt(call, second, input.ticket_id)
        )


class AddNoteOperation:
    """Add one private note or public reply under a dispatch checkpoint."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return stable manifest metadata."""
        return defs.ADD_NOTE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: AddNoteInput) -> IdempotencyKey:
        """Use the Call ID for receipt correlation only."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: AddNoteInput) -> MutationAttempt[AddNoteOutput]:
        """Dispatch one note or reply and map confirmed and uncertain outcomes."""
        operation, zero = "addNote", AddNoteOutput(_zero_conversation())
        write_budget = _write_budget(
            call, self.definition().step_defaults.execute_method_timeout.total_seconds()
        )
        try:
            if input.ticket_id < 1:
                raise ValueError("ticketId is invalid")
            _validate_write_text(input.body)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", zero, Receipt(), self.client.failure(operation, "VALIDATION", str(error))
            )
        terminal = await _single_dispatch_before(self.client, call, operation, zero)
        if terminal:
            return terminal
        key, failure, transient = await self.client.resolve_credentials(call, operation)
        if failure:
            if transient or call.context and call.context.attempt > 1:
                await _permit_retry(call)
                return MutationAttempt.retry(failure)
            return MutationAttempt.branch_result("defect", zero, Receipt(), failure)
        terminal = await _record_dispatch(self.client, call, operation, zero)
        if terminal:
            return terminal
        path = f"/tickets/{input.ticket_id}/" + ("reply" if input.is_public_reply else "notes")
        payload = (
            {"body": _html(input.body)}
            if input.is_public_reply
            else {"body": _html(input.body), "private": True}
        )
        exchange = await self.client.exchange(
            call, operation, key, "POST", path, payload=payload, write_budget=write_budget
        )
        terminal = await _single_dispatch_exchange(self.client, call, operation, exchange, zero, "notFound")
        if terminal:
            return terminal
        try:
            wire = _object(exchange.body)
            conversation = _conversation(wire)
            if conversation.ticket_id and conversation.ticket_id != input.ticket_id:
                raise ValueError
            private = _boolean(wire.get("private")) if wire.get("private") is not None else True
            conversation = replace(
                conversation,
                ticket_id=input.ticket_id,
                source=0 if input.is_public_reply else 2,
                is_private=False if input.is_public_reply else private,
            )
        except (ValueError, TypeError) as error:
            return MutationAttempt.uncertain(
                zero,
                self.client.failure(
                    operation,
                    "PROTOCOL",
                    f"Freshdesk accepted the note but returned an invalid conversation: {error}",
                ),
                self.client.receipt(call, exchange),
            )
        return MutationAttempt.branch_result(
            "added", AddNoteOutput(conversation), self.client.receipt(call, exchange, conversation.id)
        )
