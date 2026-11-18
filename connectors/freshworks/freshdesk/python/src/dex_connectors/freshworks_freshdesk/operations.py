# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Freshdesk ticket operations and Go-compatible provider decoding."""

import html
import ipaddress
import json
import re
from dataclasses import dataclass, replace
from datetime import UTC, date, datetime, timedelta
from email.utils import parseaddr
from typing import Any, Protocol, TypeVar

from dex_connectors.sdk import (
    Call,
    CallID,
    Failure,
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
    Marker,
)

from ._generated import definitions as defs
from .client import Client, Exchange, accepted_provider_id
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
_DecodedOutput = TypeVar("_DecodedOutput", covariant=True)
# Only these statuses prove Freshdesk applied nothing; any other non-2xx answer may have been applied.
_DEFINITE_REFUSAL_STATUSES = frozenset({400, 401, 403, 404, 411, 413, 414, 415, 422})
# A read with one of these outcomes may answer differently on a later attempt.
_TRANSIENT_OUTCOMES = frozenset({"rateLimited", "notSent", "unavailable", "serviceUnavailable"})
# Freshdesk's largest conversation page, and the pages a note read-back searches in one attempt.
_ACCEPTED_CONVERSATION_PAGE_SIZE = 100
_MAXIMUM_ACCEPTED_CONVERSATION_PAGES = 10
_MAXIMUM_PROVIDER_ID = 2**63 - 1


@dataclass(frozen=True, slots=True)
class _WritePermit:
    """A fence that allows this attempt to send, with its send-cap facts."""

    fence: Fence
    # No earlier attempt of this call may have sent.
    is_first_send: bool
    # This attempt's send leaves room under the cap for one more.
    can_resend_unknown_outcome: bool
    # An earlier attempt checkpointed this ID; nonzero means read back and never send.
    saved_provider_id: int = 0


@dataclass(frozen=True, slots=True)
class _AcceptedWriteReadBack:
    """The result of reading an accepted write back by provider ID."""

    is_confirmed: bool
    # The last read; its failure explains an unconfirmed read-back.
    exchange: Exchange
    output: Any = None


class _AcceptedWriteReader(Protocol[_DecodedOutput]):
    """Decode one operation's accepted answer and read the write back by provider ID."""

    def decode_accepted_write(self, body: bytes) -> tuple[_DecodedOutput, int]:
        """Return the output and provider ID of a 2xx answer; raise ValueError or TypeError."""
        ...

    async def read_accepted_write(self, call: Call, key: str, provider_id: int) -> _AcceptedWriteReadBack:
        """Read the accepted write back by the provider ID Freshdesk returned."""
        ...


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


def _is_unknown_update_exchange(exchange: Exchange) -> bool:
    """Report an updateTicket answer other than success or a definite refusal with a trusted body."""
    return exchange.is_body_incomplete or (
        exchange.outcome not in ("succeeded", "notFound", "defect")
        and not (exchange.outcome == "rejected" and exchange.status in _DEFINITE_REFUSAL_STATUSES)
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


async def _begin_write(
    client: Client, call: Call, operation: str, definition: MutationDefinition, zero: Output
) -> tuple[_WritePermit | None, MutationAttempt[Output] | None]:
    """Decide whether this attempt may send or must read a saved provider ID back.

    Freshdesk has no read-back by Call ID, so only a checkpointed provider ID is read back.
    """
    try:
        fence = Fence(call, definition.step_defaults.execute_method_timeout, max_sends=call.max_sends)
        decision = await fence.decide()
    except ValueError:
        return None, MutationAttempt.branch_result(
            "defect",
            zero,
            Receipt(),
            client.failure(operation, "LOCAL_DEFECT", "single-dispatch fence could not start"),
        )
    if decision.kind == DecisionKind.FAIL_CLOSED:
        # FailClosed sends nothing, so a later attempt decides again from a readable checkpoint.
        return None, MutationAttempt.retry(
            client.failure(
                operation,
                "AVAILABILITY",
                "the single-dispatch checkpoint could not be read; nothing was sent in this attempt",
            )
        )
    if decision.marker is not None and decision.marker.provider_id:
        # A saved provider ID proves Freshdesk accepted the write, so it is read back and never resent.
        saved_provider_id = _marker_provider_id(decision.marker)
        if not saved_provider_id:
            return None, MutationAttempt.uncertain(
                zero,
                client.failure(
                    operation,
                    "PROTOCOL",
                    "Freshdesk accepted an earlier attempt's write, and its checkpointed ID is not a "
                    "Freshdesk ID",
                ),
                client.receipt(call),
            )
        return _WritePermit(fence, False, False, saved_provider_id), None
    if decision.kind == DecisionKind.READ_BACK_REQUIRED:
        return None, MutationAttempt.uncertain(
            zero,
            client.failure(
                operation,
                "TRANSPORT",
                "earlier attempts may have sent the request and the send cap is reached, "
                "so it is not sent again",
            ),
            client.receipt(call),
        )
    prior_sends = decision.prior_send_upper_bound
    return _WritePermit(fence, prior_sends == 0, prior_sends + 1 < call.max_sends), None


async def _credential_attempt(
    client: Client, permit: _WritePermit, failure: Failure, transient: bool, zero: Output
) -> MutationAttempt[Output]:
    """Map a credential failure before any send in this attempt."""
    if not permit.is_first_send:
        # notSent would hide the earlier possible send, so nothing is recorded.
        if transient:
            return MutationAttempt.retry(failure)
        return MutationAttempt.uncertain(zero, failure, Receipt())
    try:
        await permit.fence.record_not_sent()
    except Exception:
        return MutationAttempt.retry(
            client.failure(failure.operation, "AVAILABILITY", "Dex did not record the not-sent checkpoint")
        )
    if transient:
        return MutationAttempt.retry(failure)
    return MutationAttempt.branch_result("defect", zero, Receipt(), failure)


async def _send_write(
    client: Client,
    call: Call,
    permit: _WritePermit,
    operation: str,
    key: str,
    path: str,
    payload: dict[str, Any],
    write_budget: float,
    zero: Output,
) -> tuple[Exchange | None, MutationAttempt[Output] | None]:
    """Dispatch one POST under the fence; a refused dispatch returns its Retry attempt.

    The write window expiring after dispatch is an unknown outcome, unless a 2xx status arrived.
    """
    observed_statuses: list[int] = []
    try:
        exchange = await permit.fence.dispatch(
            lambda deadline: client.exchange(
                call,
                operation,
                key,
                "POST",
                path,
                payload=payload,
                write_budget=write_budget,
                deadline=deadline,
                on_response_status=observed_statuses.append,
            )
        )
    except (CheckpointError, InsufficientAttemptTimeError, DispatchNotAllowedError):
        # Dispatch refused before sending, so nothing reached Freshdesk in this attempt.
        return None, MutationAttempt.retry(
            client.failure(operation, "AVAILABILITY", "Freshdesk write was not dispatched")
        )
    except TimeoutError:
        if observed_statuses and 200 <= observed_statuses[0] < 300:
            # Freshdesk accepted the write, and the cut body carries no ID to read it back by.
            return None, MutationAttempt.uncertain(
                zero,
                client.failure(
                    operation,
                    "TRANSPORT",
                    "Freshdesk accepted the write, but its answer did not arrive within the "
                    "single-dispatch write window; it is not sent again",
                ),
                client.receipt(call),
            )
        # The claim was recorded and the request may have reached Freshdesk.
        failure = client.failure(
            operation, "TRANSPORT", "Freshdesk did not answer within the single-dispatch write window"
        )
        return None, _resend_or_uncertain(permit, zero, failure, 0, client.receipt(call))
    return exchange, None


async def _finish_write(
    client: Client,
    call: Call,
    permit: _WritePermit,
    operation: str,
    key: str,
    exchange: Exchange,
    success_branch: str,
    zero: Output,
    reader: _AcceptedWriteReader[Output],
    not_found: str = "providerRejected",
) -> MutationAttempt[Output]:
    """Map a sent write; any 2xx, or an answer carrying a created ID, is accepted and never resent."""
    if 200 <= exchange.status < 300:
        failure = exchange.failure
        if exchange.outcome == "succeeded":
            try:
                output, provider_id = reader.decode_accepted_write(exchange.body)
            except (ValueError, TypeError) as error:
                failure = client.failure(
                    operation,
                    "PROTOCOL",
                    f"Freshdesk accepted the write but returned an invalid answer: {error}",
                )
            else:
                await _record_accepted_write(permit, provider_id)
                return MutationAttempt.branch_result(
                    success_branch, output, client.receipt(call, exchange, provider_id)
                )
        assert failure is not None
        return await _complete_unreadable_accepted_write(
            client, call, permit, key, exchange, accepted_provider_id(exchange.body), failure,
            success_branch, zero, reader,
        )
    assert exchange.failure is not None
    if exchange.body_provider_id:
        return await _complete_unreadable_accepted_write(
            client, call, permit, key, exchange, exchange.body_provider_id, exchange.failure,
            success_branch, zero, reader,
        )
    return await _write_exchange_attempt(client, call, permit, exchange, zero, not_found)


async def _complete_unreadable_accepted_write(
    client: Client,
    call: Call,
    permit: _WritePermit,
    key: str,
    exchange: Exchange,
    provider_id: int,
    failure: Failure,
    success_branch: str,
    zero: Output,
    reader: _AcceptedWriteReader[Output],
) -> MutationAttempt[Output]:
    """Read back an accepted write whose answer was unusable; without an ID select uncertain."""
    if not provider_id:
        return MutationAttempt.uncertain(zero, failure, client.receipt(call, exchange))
    try:
        await permit.fence.record_provider_id(str(provider_id))
        is_provider_id_recorded = True
    except Exception:
        # Without a saved ID a later attempt could resend, so Retry is forbidden.
        is_provider_id_recorded = False
    return await _confirm_accepted_write(
        client, call, key, provider_id, is_provider_id_recorded, success_branch, zero, reader
    )


async def _confirm_accepted_write(
    client: Client,
    call: Call,
    key: str,
    provider_id: int,
    is_provider_id_recorded: bool,
    success_branch: str,
    zero: Output,
    reader: _AcceptedWriteReader[Output],
) -> MutationAttempt[Output]:
    """Read an accepted write back: confirmed succeeds, a checkpointed transient failure retries."""
    read_back = await reader.read_accepted_write(call, key, provider_id)
    receipt = client.receipt(call, read_back.exchange, provider_id)
    if read_back.is_confirmed:
        return MutationAttempt.branch_result(success_branch, read_back.output, receipt)
    assert read_back.exchange.failure is not None
    if is_provider_id_recorded and read_back.exchange.outcome in _TRANSIENT_OUTCOMES:
        return MutationAttempt.retry(read_back.exchange.failure, read_back.exchange.retry_after)
    return MutationAttempt.uncertain(zero, read_back.exchange.failure, receipt)


async def _write_exchange_attempt(
    client: Client,
    call: Call,
    permit: _WritePermit,
    exchange: Exchange,
    zero: Output,
    not_found: str = "providerRejected",
) -> MutationAttempt[Output]:
    """Map a sent write Freshdesk did not accept; only a complete definite refusal selects a refusal."""
    assert exchange.failure is not None
    if exchange.outcome in ("rateLimited", "notSent", "serviceUnavailable"):
        try:
            await permit.fence.record_can_resend()
        except Exception:
            pass  # A lost checkpoint leaves the claim, so the next attempt counts this send.
        return MutationAttempt.retry(exchange.failure, exchange.retry_after)
    receipt = client.receipt(call, exchange)
    if exchange.outcome == "notFound" and not exchange.is_body_incomplete:
        return await _reject_write(client, permit, exchange.failure, receipt, not_found, zero)
    if exchange.outcome == "defect":
        # defect would hide an earlier possible send, so only a first send selects it.
        if permit.is_first_send:
            return await _reject_write(client, permit, exchange.failure, Receipt(), "defect", zero)
        # The request could not be built, so a resend would fail the same way.
        return MutationAttempt.uncertain(zero, exchange.failure, receipt)
    if (
        exchange.outcome == "rejected"
        and exchange.status in _DEFINITE_REFUSAL_STATUSES
        and not exchange.is_body_incomplete
    ):
        return await _reject_write(client, permit, exchange.failure, receipt, "providerRejected", zero)
    return _resend_or_uncertain(permit, zero, exchange.failure, exchange.retry_after, receipt)


async def _reject_write(
    client: Client, permit: _WritePermit, failure: Failure, receipt: Receipt, branch: str, zero: Output
) -> MutationAttempt[Output]:
    """Select a refusal branch only when no earlier send of this call may have applied."""
    if not permit.is_first_send:
        return MutationAttempt.uncertain(zero, failure, receipt)
    try:
        await permit.fence.record_can_resend()
    except Exception:
        return MutationAttempt.retry(
            client.failure(failure.operation, "AVAILABILITY", "Dex did not record the refusal checkpoint")
        )
    return MutationAttempt.branch_result(branch, zero, receipt, failure)


def _resend_or_uncertain(
    permit: _WritePermit, zero: Output, failure: Failure, retry_after: float, receipt: Receipt
) -> MutationAttempt[Output]:
    """Retry an unknown outcome while the send cap allows another send."""
    if permit.can_resend_unknown_outcome:
        # The claim stays the last marker, so the next attempt counts this send.
        return MutationAttempt.retry(failure, retry_after)
    return MutationAttempt.uncertain(zero, failure, receipt)


async def _record_accepted_write(permit: _WritePermit, provider_id: int) -> None:
    try:
        await permit.fence.record_provider_id(str(provider_id))
    except Exception:
        pass  # The accepted response already proves success; this checkpoint only helps recovery.


def _marker_provider_id(marker: Marker | None) -> int:
    """Return a checkpointed Freshdesk ID for the Receipt, or 0 when none is valid."""
    if marker is None or not marker.provider_id.isdigit():
        return 0
    provider_id = int(marker.provider_id)
    return provider_id if 1 <= provider_id <= _MAXIMUM_PROVIDER_ID else 0


class CreateTicketOperation:
    """Create one Freshdesk ticket under the single-dispatch fence and its send cap."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return stable manifest metadata."""
        return defs.CREATE_TICKET_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreateTicketInput) -> IdempotencyKey:
        """Use the Call ID as receipt correlation, never as a provider replay key."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: CreateTicketInput) -> MutationAttempt[CreateTicketOutput]:
        """Send the ticket, resending an unknown outcome while the send cap allows."""
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
        permit, terminal = await _begin_write(self.client, call, operation, self.definition(), zero)
        if permit is None:
            assert terminal is not None
            return terminal
        key, failure, transient = await self.client.resolve_credentials(call, operation)
        if failure:
            return await _credential_attempt(self.client, permit, failure, transient, zero)
        reader = _CreatedTicketReader(self.client)
        if permit.saved_provider_id:
            return await _confirm_accepted_write(
                self.client, call, key, permit.saved_provider_id, True, "created", zero, reader
            )
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
        exchange, terminal = await _send_write(
            self.client, call, permit, operation, key, "/tickets", payload, write_budget, zero
        )
        if exchange is None:
            assert terminal is not None
            return terminal
        return await _finish_write(
            self.client, call, permit, operation, key, exchange, "created", zero, reader
        )


class _CreatedTicketReader:
    """Decode a created ticket and read it back by ID."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def decode_accepted_write(self, body: bytes) -> tuple[CreateTicketOutput, int]:
        ticket = _ticket(body)
        return CreateTicketOutput(ticket), ticket.id

    async def read_accepted_write(self, call: Call, key: str, provider_id: int) -> _AcceptedWriteReadBack:
        """Read GET /tickets/{id}; a missing or invalid ticket is unconfirmed."""
        exchange = await self.client.exchange(call, "createTicket", key, "GET", f"/tickets/{provider_id}")
        if exchange.outcome != "succeeded":
            return _AcceptedWriteReadBack(False, exchange)
        try:
            ticket = _ticket(exchange.body, provider_id)
        except (ValueError, TypeError) as error:
            return _unconfirmed_read_back(
                self.client,
                exchange,
                "createTicket",
                f"Freshdesk accepted the ticket, and its read-back returned an invalid ticket: {error}",
            )
        return _AcceptedWriteReadBack(True, exchange, CreateTicketOutput(ticket))


def _unconfirmed_read_back(
    client: Client, exchange: Exchange, operation: str, message: str
) -> _AcceptedWriteReadBack:
    return _AcceptedWriteReadBack(
        False, replace(exchange, outcome="invalid", failure=client.failure(operation, "PROTOCOL", message))
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
        if failure and (transient or (call.context is not None and call.context.attempt > 1)):
            # defect would hide a change an earlier attempt may have sent; repeating it is safe.
            return MutationAttempt.retry(failure)
        if failure:
            return MutationAttempt.branch_result("defect", zero, Receipt(), failure)
        path = f"/tickets/{input.ticket_id}"
        first = await self.client.exchange(call, operation, key, "GET", path)
        if _is_unknown_update_exchange(first):
            # An earlier attempt's PUT may have applied, so the read selects a branch only when definite.
            assert first.failure is not None
            return MutationAttempt.retry(first.failure, first.retry_after)
        terminal = _mutation_terminal(self.client, call, first, zero, input.ticket_id)
        if terminal:
            return terminal
        try:
            current = _ticket(first.body, input.ticket_id)
        except (ValueError, TypeError) as error:
            # An invalid ticket is an unknown answer, so the safe change repeats.
            return MutationAttempt.retry(
                self.client.failure(operation, "PROTOCOL", f"Freshdesk returned an invalid ticket: {error}")
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
        if _is_unknown_update_exchange(second):
            # The PUT sets final values, so an unknown outcome repeats it.
            assert second.failure is not None
            return MutationAttempt.retry(second.failure, second.retry_after)
        terminal = _mutation_terminal(self.client, call, second, current_output, input.ticket_id)
        if terminal:
            return terminal
        try:
            updated = _ticket(second.body, input.ticket_id)
        except (ValueError, TypeError) as error:
            return MutationAttempt.retry(
                self.client.failure(
                    operation,
                    "PROTOCOL",
                    f"Freshdesk accepted the change but returned an invalid updated ticket: {error}",
                )
            )
        return MutationAttempt.branch_result(
            "updated", UpdateTicketOutput(updated), self.client.receipt(call, second, input.ticket_id)
        )


class AddNoteOperation:
    """Add one private note or public reply under the single-dispatch fence and its send cap."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return stable manifest metadata."""
        return defs.ADD_NOTE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: AddNoteInput) -> IdempotencyKey:
        """Use the Call ID for receipt correlation only."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: AddNoteInput) -> MutationAttempt[AddNoteOutput]:
        """Send the note or reply, resending an unknown outcome while the send cap allows."""
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
        permit, terminal = await _begin_write(self.client, call, operation, self.definition(), zero)
        if permit is None:
            assert terminal is not None
            return terminal
        key, failure, transient = await self.client.resolve_credentials(call, operation)
        if failure:
            return await _credential_attempt(self.client, permit, failure, transient, zero)
        reader = _AddedConversationReader(self.client, input)
        if permit.saved_provider_id:
            return await _confirm_accepted_write(
                self.client, call, key, permit.saved_provider_id, True, "added", zero, reader
            )
        path = f"/tickets/{input.ticket_id}/" + ("reply" if input.is_public_reply else "notes")
        payload: dict[str, Any] = (
            {"body": _html(input.body)}
            if input.is_public_reply
            else {"body": _html(input.body), "private": True}
        )
        exchange, terminal = await _send_write(
            self.client, call, permit, operation, key, path, payload, write_budget, zero
        )
        if exchange is None:
            assert terminal is not None
            return terminal
        return await _finish_write(
            self.client, call, permit, operation, key, exchange, "added", zero, reader, "notFound"
        )


def _added_conversation(value: Any, input: AddNoteInput) -> TicketConversation:
    """Decode an added conversation, filling its kind from the request as a reply answer lacks it."""
    wire = _object(value)
    conversation = _conversation(wire)
    if conversation.ticket_id and conversation.ticket_id != input.ticket_id:
        raise ValueError("conversation response is for another ticket")
    private = _boolean(wire.get("private")) if wire.get("private") is not None else True
    return replace(
        conversation,
        ticket_id=input.ticket_id,
        source=0 if input.is_public_reply else 2,
        is_private=False if input.is_public_reply else private,
    )


class _AddedConversationReader:
    """Decode an added note or reply and find it in the ticket's conversation list."""

    def __init__(self, client: Client, input: AddNoteInput) -> None:
        self.client = client
        self.input = input

    def decode_accepted_write(self, body: bytes) -> tuple[AddNoteOutput, int]:
        conversation = _added_conversation(body, self.input)
        return AddNoteOutput(conversation), conversation.id

    async def read_accepted_write(self, call: Call, key: str, provider_id: int) -> _AcceptedWriteReadBack:
        """Page GET /tickets/{id}/conversations, Freshdesk's only conversation read, for the ID."""
        for page in range(1, _MAXIMUM_ACCEPTED_CONVERSATION_PAGES + 1):
            exchange = await self.client.exchange(
                call,
                "addNote",
                key,
                "GET",
                f"/tickets/{self.input.ticket_id}/conversations",
                {"per_page": str(_ACCEPTED_CONVERSATION_PAGE_SIZE), "page": str(page)},
            )
            if exchange.outcome != "succeeded":
                return _AcceptedWriteReadBack(False, exchange)
            try:
                rows = json.loads(exchange.body)
            except (ValueError, TypeError):
                rows = None
            if not isinstance(rows, list):
                return _unconfirmed_read_back(
                    self.client,
                    exchange,
                    "addNote",
                    "Freshdesk accepted the note, and its read-back returned an invalid conversation page",
                )
            for row in rows:
                row_id = row.get("id") if isinstance(row, dict) else None
                if type(row_id) is not int or row_id != provider_id:
                    continue
                try:
                    conversation = _added_conversation(row, self.input)
                except (ValueError, TypeError) as error:
                    return _unconfirmed_read_back(
                        self.client,
                        exchange,
                        "addNote",
                        "Freshdesk accepted the note, and its read-back returned an invalid "
                        f"conversation: {error}",
                    )
                return _AcceptedWriteReadBack(True, exchange, AddNoteOutput(conversation))
            if not (exchange.headers and 'rel="next"' in exchange.headers.get("Link", "")):
                return _unconfirmed_read_back(
                    self.client,
                    exchange,
                    "addNote",
                    "Freshdesk accepted the note, and the ticket's conversations do not list it",
                )
        return _unconfirmed_read_back(
            self.client,
            Exchange("invalid"),
            "addNote",
            "Freshdesk accepted the note, and it is not within the ticket's first conversation pages",
        )
