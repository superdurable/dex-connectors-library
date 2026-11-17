# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Zoho Desk operation invocations and Go-compatible provider decoding."""

from __future__ import annotations

import html
import json
import re
import unicodedata
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Any
from urllib.parse import quote

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
from dex_connectors.sdk.singledispatch import (
    CheckpointError,
    DecisionKind,
    DispatchNotAllowedError,
    Fence,
    InsufficientAttemptTimeError,
)

from ._generated import definitions as d
from ._generated.timestamps import parse_go_datetime
from .client import _ID, Client, Credentials, Exchange, failure
from .models import (
    AddCommentInput,
    AddCommentOutput,
    CreateTicketInput,
    CreateTicketOutput,
    GetTicketInput,
    SearchTicketsInput,
    SearchTicketsOutput,
    Ticket,
    TicketComment,
    TicketContact,
    TicketDetails,
    TicketThread,
    UpdateTicketInput,
    UpdateTicketOutput,
)

_PICKLIST_FORBIDDEN = ",*${}"


def _object(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError("expected object")
    return value


def _string(value: Any) -> str:
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError("expected string")
    return value


def _id(value: Any) -> str:
    if value is None:
        return ""
    if isinstance(value, bool) or not isinstance(value, (int, str)):
        raise ValueError("ID is neither a string nor a number")
    text = str(value)
    if text and not _ID.fullmatch(text):
        raise ValueError("ID is not a numeric Zoho Desk ID")
    return text


def _count(value: Any) -> int:
    if value is None:
        return 0
    if isinstance(value, bool) or not isinstance(value, (int, str)):
        raise ValueError("count is neither a string nor a number")
    text = str(value)
    if not re.fullmatch(r"[+-]?[0-9]+", text):
        raise ValueError("count is not a non-negative integer")
    count = int(text)
    if count < 0 or count > 2**63 - 1:
        raise ValueError("count is not a non-negative integer")
    return count


def _flag(value: Any) -> bool:
    if value is True or value == "true":
        return True
    if value is False or value is None or value in ("false", ""):
        return False
    raise ValueError("flag is not a boolean")


def _timestamp(field_name: str, value: Any) -> datetime:
    text = _string(value)
    try:
        return parse_go_datetime(text, field_name).astimezone(UTC)
    except ValueError:
        raise ValueError(f"{field_name} is not an ISO 8601 timestamp") from None


def _optional_timestamp(field_name: str, value: Any) -> datetime | None:
    return _timestamp(field_name, value) if value not in (None, "") else None


def _truncate(value: str) -> tuple[str, bool]:
    encoded = re.sub(r"[\ud800-\udfff]", "\ufffd", value).encode()
    if len(encoded) <= 16384:
        return encoded.decode(), False
    return encoded[:16384].decode(errors="ignore"), True


def _ticket(value: Any, expected_id: str = "") -> tuple[Ticket, dict[str, Any] | None]:
    wire = _object(value)
    ticket_id = _id(wire.get("id"))
    if not ticket_id:
        raise ValueError("ticket ID is missing")
    if expected_id and ticket_id != expected_id:
        raise ValueError("ticket response is for another ticket")
    status = _string(wire.get("status"))
    if not status.strip():
        raise ValueError("ticket status is missing")
    raw_type = _string(wire.get("statusType"))
    upper_type = raw_type.upper().replace(" ", "")
    status_type = {"OPEN": "Open", "ONHOLD": "On Hold", "CLOSED": "Closed"}.get(upper_type, raw_type)
    description, is_truncated = _truncate(_string(wire.get("description")))
    contact = wire.get("contact")
    if contact is not None:
        contact = _object(contact)
    ticket = Ticket(
        id=ticket_id,
        status=status,
        ticket_number=_string(wire.get("ticketNumber")),
        subject=_string(wire.get("subject")),
        description_html=description,
        is_description_truncated=is_truncated,
        status_type=status_type or None,
        priority=_string(wire.get("priority")) or None,
        channel=_string(wire.get("channel")),
        classification=_string(wire.get("classification")),
        department_id=_id(wire.get("departmentId")),
        contact_id=_id(wire.get("contactId")),
        assignee_id=_id(wire.get("assigneeId")),
        team_id=_id(wire.get("teamId")),
        email=_string(wire.get("email")),
        is_escalated=_flag(wire.get("isEscalated")),
        is_overdue=_flag(wire.get("isOverDue")),
        is_spam=_flag(wire.get("isSpam")),
        thread_count=_count(wire.get("threadCount")),
        comment_count=_count(wire.get("commentCount")),
        created_at=_timestamp("createdTime", wire.get("createdTime")),
        modified_at=_timestamp("modifiedTime", wire.get("modifiedTime")),
        due_at=_optional_timestamp("dueDate", wire.get("dueDate")),
        closed_at=_optional_timestamp("closedTime", wire.get("closedTime")),
        web_url=_string(wire.get("webUrl")),
    )
    return ticket, contact


def _ticket_body(body: bytes, expected_id: str = "") -> tuple[Ticket, dict[str, Any] | None]:
    try:
        return _ticket(json.loads(body), expected_id)
    except (json.JSONDecodeError, UnicodeDecodeError):
        raise ValueError("ticket response is not a ticket object") from None


def _contact(value: dict[str, Any] | None) -> TicketContact | None:
    if value is None:
        return None
    contact_id = _id(value.get("id"))
    first_name = _string(value.get("firstName"))
    last_name = _string(value.get("lastName"))
    email = _string(value.get("email"))
    if not contact_id:
        return None
    return TicketContact(
        contact_id,
        first_name,
        last_name,
        email,
    )


def _comment(value: Any) -> TicketComment:
    wire = _object(value)
    comment_id = _id(wire.get("id"))
    if not comment_id:
        raise ValueError("comment has no ID")
    content_type = _string(wire.get("contentType"))
    content = _string(wire.get("content"))
    plain_text = wire.get("plainText")
    if plain_text is not None:
        plain_text = _string(plain_text)
        if content_type.casefold() != "plaintext":
            content, content_type = plain_text, "plainText"
    content, is_truncated = _truncate(content)
    author = wire.get("commenter")
    if author is not None:
        author = _object(author)
    return TicketComment(
        id=comment_id,
        is_public=_flag(wire.get("isPublic")),
        content=content,
        content_type=content_type,
        is_content_truncated=is_truncated,
        commenter_id=_id(wire.get("commenterId")),
        commenter_name=_string(author.get("name")) if author else "",
        commenter_type=_string(author.get("type")) if author else "",
        commented_at=_timestamp("commentedTime", wire.get("commentedTime")),
        modified_at=_optional_timestamp("modifiedTime", wire.get("modifiedTime")),
    )


def _list(body: bytes, status: int, limit: int) -> list[Any]:
    if status == 204:
        return []
    try:
        page = _object(json.loads(body))
        data = page.get("data")
        if not isinstance(data, list):
            raise ValueError("list is not a JSON object with a data array")
        if len(data) > limit:
            raise ValueError("list is larger than requested")
        return data
    except (json.JSONDecodeError, UnicodeDecodeError, TypeError):
        raise ValueError("list is not a JSON object with a data array") from None


def _thread(value: Any) -> TicketThread:
    wire = _object(value)
    thread_id = _id(wire.get("id"))
    if not thread_id:
        raise ValueError("thread has no ID")
    summary, is_truncated = _truncate(_string(wire.get("summary")))
    author = wire.get("author")
    if author is not None:
        author = _object(author)
    return TicketThread(
        id=thread_id,
        channel=_string(wire.get("channel")),
        direction=_string(wire.get("direction")),
        visibility=_string(wire.get("visibility")),
        status=_string(wire.get("status")),
        summary=summary,
        is_summary_truncated=is_truncated,
        is_description_thread=_flag(wire.get("isDescriptionThread")),
        author_name=_string(author.get("name")) if author else "",
        author_type=_string(author.get("type")) if author else "",
        created_at=_timestamp("createdTime", wire.get("createdTime")),
    )


def _validate_id(name: str, value: str, optional: bool = False) -> None:
    if not (optional and value == "") and not _ID.fullmatch(value):
        raise ValueError(f"{name} must be a numeric Zoho Desk ID such as 1892000000042034")


def _validate_picklist(name: str, value: str) -> None:
    if (
        not value
        or value.strip() != value
        or len(value) > 120
        or any(
            unicodedata.category(character) == "Cc" or character in _PICKLIST_FORBIDDEN for character in value
        )
    ):
        raise ValueError(f"{name} must be 1 to 120 characters without surrounding spaces")


def _validate_text(name: str, value: str, limit: int) -> None:
    if not value.strip() or len(value) > limit:
        raise ValueError(f"{name} is required or exceeds Zoho Desk's {limit}-character limit")


def _path(ticket_id: str) -> str:
    return "/tickets/" + quote(ticket_id, safe="-._~$&+=:@")


def _required_failure(result: Exchange) -> Failure:
    if result.failure is None:
        raise ValueError("Zoho Desk failed exchange has no failure")
    return result.failure


def _query_attempt(
    result: Exchange, receipt: Receipt, invalid: str, not_found: str = ""
) -> QueryAttempt[Any] | None:
    if result.outcome == "success":
        return None
    if result.outcome in ("rate_limited", "not_sent", "unavailable"):
        return QueryAttempt.retry(_required_failure(result), result.retry_after_seconds)
    branch = (
        not_found
        if result.outcome == "not_found" and not_found
        else invalid
        if result.outcome == "invalid"
        else d.DEFECT
        if result.outcome == "defect"
        else d.PROVIDER_REJECTED
    )
    return QueryAttempt.branch_result(branch, None, receipt, result.failure)


def _mutation_attempt(
    result: Exchange, receipt: Receipt, value: Any, invalid: str, not_found: str = ""
) -> MutationAttempt[Any] | None:
    if result.outcome == "success":
        return None
    if result.outcome in ("rate_limited", "not_sent", "unavailable"):
        return MutationAttempt.retry(_required_failure(result), result.retry_after_seconds)
    branch = (
        not_found
        if result.outcome == "not_found" and not_found
        else invalid
        if result.outcome == "invalid"
        else d.DEFECT
        if result.outcome == "defect"
        else d.PROVIDER_REJECTED
    )
    return MutationAttempt.branch_result(branch, value, receipt, result.failure)


class SearchTicketsOperation:
    """Search one page of Desk tickets."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated operation definition."""
        return d.SEARCH_TICKETS_DEFINITION

    async def invoke(self, call: Call, input: SearchTicketsInput) -> QueryAttempt[SearchTicketsOutput]:
        """Validate filters, search, and decode one page."""
        try:
            if input.statuses and input.status_types:
                raise ValueError("set statuses or statusTypes, not both")
            status_types = {"Open": "${OPEN}", "On Hold": "${ONHOLD}", "Closed": "${CLOSED}"}
            statuses = list(input.statuses)
            for status in statuses:
                _validate_picklist("statuses entry", status)
            for status_type in input.status_types:
                if status_type not in status_types:
                    raise ValueError("statusTypes entry must be Open, On Hold, or Closed")
                statuses.append(status_types[status_type])
            priorities = list(input.priorities)
            for priority in priorities:
                _validate_picklist("priorities entry", priority)
            for name, values in (("statuses", statuses), ("priorities", priorities)):
                if len({value.casefold() for value in values}) != len(values):
                    raise ValueError(f"{name} lists a value twice")
            query: dict[str, str] = {}
            if statuses:
                query["status"] = ",".join(statuses)
            if priorities:
                query["priority"] = ",".join(priorities)
            if input.contact_email:
                if not _bare_email(input.contact_email):
                    raise ValueError("contactEmail must be one bare email address such as jane@example.com")
                query["email"] = input.contact_email
            _validate_id("departmentId", input.department_id, True)
            if input.department_id:
                query["departmentId"] = input.department_id
            if input.modified_since:
                since = _timestamp("modifiedSince", input.modified_since)
                now = datetime.now(UTC)
                if since > now:
                    raise ValueError("modifiedSince cannot be in the future")
                query["modifiedTimeRange"] = _zoho_time(since) + "," + _zoho_time(now)
            if not query:
                raise ValueError("at least one search filter is required")
            if input.from_ < 0 or input.from_ >= 5000 or input.limit < 0 or input.limit > 100:
                raise ValueError("from must be 0 to 4999, and limit 1 to 100 or zero for 25")
            limit = min(input.limit or 25, 5000 - input.from_)
            query.update({"from": str(input.from_), "limit": str(limit), "sortBy": "-modifiedTime"})
        except (ValueError, AttributeError) as error:
            return QueryAttempt.branch_result(
                d.DEFECT,
                SearchTicketsOutput(),
                Receipt(),
                failure("searchTickets", FailureKind.VALIDATION, str(error)),
            )
        credentials, base, route = await self.client.resolve(call, "searchTickets")
        if route:
            return _session_query(route, "searchTickets", SearchTicketsOutput())
        assert credentials is not None and base is not None
        result = await self.client.exchange(
            call, "searchTickets", credentials, base, "GET", "/tickets/search", query
        )
        receipt = self.client.receipt(call, result)
        routed = _query_attempt(result, receipt, d.INVALID_RESPONSE)
        if routed:
            return _with_query_value(routed, SearchTicketsOutput())
        try:
            if result.status == 204:
                return QueryAttempt.branch_result(d.SEARCHED, SearchTicketsOutput(), receipt)
            page = _object(json.loads(result.body))
            rows = page.get("data")
            if not isinstance(rows, list):
                raise ValueError("search page has no data array")
            if len(rows) > limit:
                raise ValueError("search page holds more tickets than requested")
            count = _count(page.get("count"))
            tickets = []
            for row in rows:
                ticket, contact = _ticket(row)
                validated_contact = _contact(contact)
                if input.contact_email and input.contact_email.casefold() not in (
                    ticket.email.casefold(),
                    validated_contact.email.casefold() if validated_contact else "",
                ):
                    continue
                from dataclasses import replace

                tickets.append(replace(ticket, description_html="", is_description_truncated=False))
            next_from = input.from_ + limit if len(rows) == limit and input.from_ + limit < 5000 else 0
            return QueryAttempt.branch_result(
                d.SEARCHED, SearchTicketsOutput(tuple(tickets), count, next_from), receipt
            )
        except (ValueError, TypeError, json.JSONDecodeError) as error:
            return QueryAttempt.branch_result(
                d.INVALID_RESPONSE,
                SearchTicketsOutput(),
                receipt,
                failure(
                    "searchTickets",
                    FailureKind.PROTOCOL,
                    "Zoho Desk returned an invalid search page: " + str(error),
                ),
            )


class GetTicketOperation:
    """Read a ticket, contact, newest threads, and newest comments."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated operation definition."""
        return d.GET_TICKET_DEFINITION

    async def invoke(self, call: Call, input: GetTicketInput) -> QueryAttempt[TicketDetails]:
        """Read the ticket and two bounded lists within one attempt."""
        empty = TicketDetails(Ticket(status=""))
        try:
            _validate_id("ticketId", input.ticket_id)
            if (
                input.thread_limit < 0
                or input.thread_limit > 20
                or input.comment_limit < 0
                or input.comment_limit > 20
            ):
                raise ValueError("threadLimit and commentLimit must be 1 to 20 or zero for 5")
        except (ValueError, AttributeError) as error:
            return QueryAttempt.branch_result(
                d.DEFECT, empty, Receipt(), failure("getTicket", FailureKind.VALIDATION, str(error))
            )
        credentials, base, route = await self.client.resolve(call, "getTicket")
        if route:
            return _session_query(route, "getTicket", empty)
        assert credentials is not None and base is not None
        path = _path(input.ticket_id)
        result = await self.client.exchange(
            call, "getTicket", credentials, base, "GET", path, {"include": "contacts"}
        )
        receipt = self.client.receipt(call, result, input.ticket_id)
        routed = _query_attempt(result, receipt, d.INVALID_RESPONSE, d.NOT_FOUND)
        if routed:
            return _with_query_value(routed, empty)
        try:
            ticket, contact_wire = _ticket_body(result.body, input.ticket_id)
            contact = _contact(contact_wire)
        except ValueError as error:
            return QueryAttempt.branch_result(
                d.INVALID_RESPONSE,
                empty,
                receipt,
                failure(
                    "getTicket", FailureKind.PROTOCOL, "Zoho Desk returned an invalid ticket: " + str(error)
                ),
            )
        thread_limit = input.thread_limit or 5
        result = await self.client.exchange(
            call,
            "getTicket",
            credentials,
            base,
            "GET",
            path + "/threads",
            {"from": "0", "limit": str(thread_limit), "sortBy": "-sendDateTime"},
        )
        receipt = self.client.receipt(call, result, input.ticket_id)
        routed = _query_attempt(result, receipt, d.INVALID_RESPONSE, d.NOT_FOUND)
        if routed:
            return _with_query_value(routed, empty)
        try:
            threads = tuple(
                sorted(
                    (_thread(row) for row in _list(result.body, result.status, thread_limit)),
                    key=lambda item: item.created_at,
                    reverse=True,
                )
            )
        except ValueError as error:
            return QueryAttempt.branch_result(
                d.INVALID_RESPONSE,
                empty,
                receipt,
                failure(
                    "getTicket",
                    FailureKind.PROTOCOL,
                    "Zoho Desk returned an invalid thread list: " + str(error),
                ),
            )
        comment_limit = input.comment_limit or 5
        result = await self.client.exchange(
            call,
            "getTicket",
            credentials,
            base,
            "GET",
            path + "/comments",
            {"from": "0", "limit": str(comment_limit), "sortBy": "-commentedTime"},
        )
        receipt = self.client.receipt(call, result, input.ticket_id)
        routed = _query_attempt(result, receipt, d.INVALID_RESPONSE, d.NOT_FOUND)
        if routed:
            return _with_query_value(routed, empty)
        try:
            comments = tuple(
                sorted(
                    (_comment(row) for row in _list(result.body, result.status, comment_limit)),
                    key=lambda item: item.commented_at,
                    reverse=True,
                )
            )
        except ValueError as error:
            return QueryAttempt.branch_result(
                d.INVALID_RESPONSE,
                empty,
                receipt,
                failure(
                    "getTicket",
                    FailureKind.PROTOCOL,
                    "Zoho Desk returned an invalid comment list: " + str(error),
                ),
            )
        return QueryAttempt.branch_result(
            d.FOUND,
            TicketDetails(
                ticket,
                contact,
                threads,
                ticket.thread_count > len(threads),
                comments,
                ticket.comment_count > len(comments),
            ),
            receipt,
        )


def _with_query_value(attempt: QueryAttempt[Any], value: Any) -> QueryAttempt[Any]:
    if attempt.kind == "retry":
        return attempt
    assert attempt.receipt is not None
    return QueryAttempt.branch_result(attempt.branch, value, attempt.receipt, attempt.failure)


def _session_query(route: str, operation: str, empty: Any) -> QueryAttempt[Any]:
    reply = failure(
        operation,
        FailureKind.AUTHENTICATION,
        "Zoho authorization is revoked or expired; reconnect the connection"
        if route == "rejected"
        else "Zoho credentials are temporarily unavailable"
        if route == "retry"
        else "Zoho Desk connection credentials are missing or invalid",
    )
    if route == "retry":
        return QueryAttempt.retry(reply)
    return QueryAttempt.branch_result(
        d.PROVIDER_REJECTED if route == "rejected" else d.DEFECT, empty, Receipt(), reply
    )


def _session_mutation(route: str, operation: str, empty: Any) -> MutationAttempt[Any]:
    query = _session_query(route, operation, empty)
    if query.kind == "retry":
        assert query.failure is not None
        return MutationAttempt.retry(query.failure)
    assert query.receipt is not None
    return MutationAttempt.branch_result(query.branch, empty, query.receipt, query.failure)


def _bare_email(value: str) -> bool:
    # Zoho searches cannot admit separators that change the provider query.
    if any(character in value for character in " \"'()<>;,*${}"):
        return False
    from email.utils import parseaddr

    name, address = parseaddr(value)
    return not name and address == value and "@" in value


def _zoho_time(value: datetime) -> str:
    return value.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%S.%f")[:23] + "Z"


# Only these statuses prove Zoho Desk applied nothing; any other non-2xx answer may have been applied.
_DEFINITE_REFUSAL_STATUSES = frozenset({400, 401, 403, 404, 411, 413, 414, 415, 422})
_WRITE_EXECUTE_TIMEOUT = timedelta(seconds=30)


@dataclass(frozen=True)
class _WritePermit:
    """A fence that allows this attempt to send, with its send-cap facts."""

    fence: Fence
    # No earlier attempt of this call may have sent.
    is_first_send: bool
    # This attempt's send leaves room under the cap for one more.
    can_resend_unknown_outcome: bool


async def _begin_write(
    client: Client, call: Call, operation: str, empty: Any
) -> tuple[_WritePermit | None, MutationAttempt[Any] | None]:
    """Decide whether this attempt may send.

    Zoho Desk has no ticket or comment read-back by Call ID, so an earlier
    possible send is resent while the send cap allows it.
    """
    try:
        fence = Fence(call, _WRITE_EXECUTE_TIMEOUT, max_sends=call.max_sends)
        decision = await fence.decide()
    except ValueError:
        return None, MutationAttempt.branch_result(
            d.DEFECT,
            empty,
            Receipt(),
            failure(operation, FailureKind.LOCAL_DEFECT, "single-dispatch fence could not start"),
        )
    provider_id = decision.marker.provider_id if decision.marker else ""
    if decision.kind == DecisionKind.FAIL_CLOSED:
        # FailClosed sends nothing, so a later attempt decides again from a readable checkpoint.
        return None, MutationAttempt.retry(
            failure(
                operation,
                FailureKind.AVAILABILITY,
                "the dispatch checkpoint could not be read; nothing was sent in this attempt",
            )
        )
    if decision.kind == DecisionKind.READ_BACK_REQUIRED:
        return None, MutationAttempt.uncertain(
            empty,
            failure(
                operation,
                FailureKind.TRANSPORT,
                "earlier attempts may have sent the request and the send cap is reached, "
                "so it is not sent again",
            ),
            client.receipt(call, Exchange(""), provider_id),
        )
    if decision.kind == DecisionKind.READ_BACK_THEN_RESEND_ALLOWED and provider_id:
        # A saved provider ID proves Zoho Desk accepted the write, so it is never resent.
        return None, MutationAttempt.uncertain(
            empty,
            failure(
                operation,
                FailureKind.TRANSPORT,
                "Zoho Desk accepted an earlier attempt's write, and its answer was lost",
            ),
            client.receipt(call, Exchange(""), provider_id),
        )
    return (
        _WritePermit(
            fence,
            decision.prior_send_upper_bound == 0,
            decision.prior_send_upper_bound + 1 < call.max_sends,
        ),
        None,
    )


async def _session_write(
    permit: _WritePermit, route: str, operation: str, empty: Any
) -> MutationAttempt[Any]:
    """Map a session that could not start; nothing was sent in this attempt."""
    attempt = _session_mutation(route, operation, empty)
    if not permit.is_first_send:
        # notSent would hide the earlier possible send, so nothing is recorded.
        if attempt.kind == "retry":
            return attempt
        assert attempt.failure is not None
        return MutationAttempt.uncertain(
            empty,
            failure(
                operation,
                attempt.failure.kind,
                "an earlier attempt may have sent the request, "
                "and the connection needs reconnection to resend it",
            ),
            Receipt(),
        )
    try:
        await permit.fence.record_not_sent()
    except Exception:
        return MutationAttempt.retry(
            failure(operation, FailureKind.AVAILABILITY, "Dex did not record the not-sent checkpoint")
        )
    return attempt


async def _send_write(
    permit: _WritePermit,
    client: Client,
    call: Call,
    operation: str,
    credentials: Credentials,
    base: str,
    path: str,
    payload: dict[str, Any],
) -> tuple[Exchange, MutationAttempt[Any] | None]:
    """Dispatch one POST under the fence; a refused dispatch returns its Retry attempt."""
    try:
        result = await permit.fence.dispatch(
            lambda _deadline: client.exchange(
                call,
                operation,
                credentials,
                base,
                "POST",
                path,
                payload=payload,
                resend_after_unauthorized=False,
            )
        )
    except (CheckpointError, InsufficientAttemptTimeError, DispatchNotAllowedError):
        # Dispatch refused before sending, so nothing reached Zoho Desk in this attempt.
        return Exchange(""), MutationAttempt.retry(
            failure(
                operation,
                FailureKind.AVAILABILITY,
                "Dex did not record the dispatch checkpoint; nothing was sent",
            )
        )
    except TimeoutError:
        result = Exchange(
            "unavailable",
            failure=failure(
                operation, FailureKind.TRANSPORT, "the operation deadline passed before Zoho Desk answered"
            ),
        )
    return result, None


async def _write_attempt_for_exchange(
    permit: _WritePermit,
    client: Client,
    call: Call,
    credentials: Credentials,
    result: Exchange,
    receipt: Receipt,
    empty: Any,
    not_found: str = "",
) -> MutationAttempt[Any] | None:
    """Map a sent write.

    Only a definite refusal selects a refusal branch; any other outcome may
    have been applied, so it resends while the send cap allows.
    """
    if result.outcome == "success":
        return None
    problem = _required_failure(result)
    if result.outcome in ("rate_limited", "not_sent") or (
        result.outcome == "unavailable" and result.status == 503
    ):
        try:
            await permit.fence.record_can_resend()
        except Exception:
            # A lost checkpoint leaves the claim, so the next attempt counts this send toward the cap.
            pass
        return MutationAttempt.retry(problem, result.retry_after_seconds)
    if result.outcome == "not_found":
        return await _reject_write(permit, problem, receipt, empty, not_found or d.PROVIDER_REJECTED)
    if result.outcome == "defect" and permit.is_first_send:
        return await _reject_write(permit, problem, Receipt(), empty, d.DEFECT)
    if result.outcome == "rejected" and result.status == 401:
        return await _rejected_credential_write(permit, client, call, credentials, problem, receipt, empty)
    if result.outcome == "rejected" and result.status in _DEFINITE_REFUSAL_STATUSES:
        return await _reject_write(permit, problem, receipt, empty, d.PROVIDER_REJECTED)
    return _resend_or_uncertain(permit, problem, result.retry_after_seconds, receipt, empty)


async def _rejected_credential_write(
    permit: _WritePermit,
    client: Client,
    call: Call,
    credentials: Credentials,
    problem: Failure,
    receipt: Receipt,
    empty: Any,
) -> MutationAttempt[Any]:
    """Refresh after a 401 outside the fence.

    A changed token retries, because the 401 proves this send applied nothing;
    otherwise the refusal gate decides.
    """
    replacement = await client.replace_rejected_credentials(call, credentials)
    if replacement == "replacement":
        try:
            await permit.fence.record_can_resend()
        except Exception:
            return MutationAttempt.retry(
                failure(
                    problem.operation,
                    FailureKind.AVAILABILITY,
                    "Dex did not record the credential resend checkpoint",
                )
            )
        return MutationAttempt.retry(
            failure(
                problem.operation,
                FailureKind.AUTHENTICATION,
                "Zoho Desk rejected the prior access token (HTTP 401); retry with its replacement",
            )
        )
    if replacement == "unavailable":
        problem = failure(
            problem.operation,
            problem.kind,
            "Zoho Desk rejected the access token (HTTP 401); credential refresh is temporarily unavailable",
        )
    return await _reject_write(permit, problem, receipt, empty, d.PROVIDER_REJECTED)


async def _reject_write(
    permit: _WritePermit, problem: Failure, receipt: Receipt, empty: Any, branch: str
) -> MutationAttempt[Any]:
    """Select a refusal branch only when no earlier send of this call may have applied."""
    if not permit.is_first_send:
        return MutationAttempt.uncertain(empty, problem, receipt)
    try:
        await permit.fence.record_can_resend()
    except Exception:
        return MutationAttempt.retry(
            failure(problem.operation, FailureKind.AVAILABILITY, "Dex did not record the refusal checkpoint")
        )
    return MutationAttempt.branch_result(branch, empty, receipt, problem)


def _resend_or_uncertain(
    permit: _WritePermit, problem: Failure, retry_after: float, receipt: Receipt, empty: Any
) -> MutationAttempt[Any]:
    """Retry an unknown outcome while the send cap allows another send."""
    if permit.can_resend_unknown_outcome:
        # The claim stays the last marker, so the next attempt counts this send.
        return MutationAttempt.retry(problem, retry_after)
    return MutationAttempt.uncertain(empty, problem, receipt)


async def _accepted_write_attempt(
    permit: _WritePermit,
    client: Client,
    call: Call,
    result: Exchange,
    problem: Failure,
    receipt: Receipt,
    empty: Any,
) -> MutationAttempt[Any]:
    """Handle a 2xx body that failed to decode.

    A decoded provider ID proves acceptance, so only an ID-less answer is resent.
    """
    provider_id = _accepted_provider_id(result.body)
    if provider_id:
        await _record_accepted_write(permit, provider_id)
        return MutationAttempt.uncertain(empty, problem, client.receipt(call, result, provider_id))
    return _resend_or_uncertain(permit, problem, 0, receipt, empty)


async def _record_accepted_write(permit: _WritePermit, provider_id: str) -> None:
    try:
        await permit.fence.record_provider_id(provider_id)
    except Exception:
        # Acceptance is already known if this extra checkpoint fails.
        pass


def _accepted_provider_id(body: bytes) -> str:
    """Return the valid id of a 2xx object body, or an empty string."""
    try:
        return _id(_object(json.loads(body)).get("id"))
    except (ValueError, TypeError):
        return ""


class CreateTicketOperation:
    """Create one ticket under the SDK single-dispatch fence and send cap."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated operation definition."""
        return d.CREATE_TICKET_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreateTicketInput) -> IdempotencyKey:
        """Correlate the Call ID; Zoho provides no idempotency key."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: CreateTicketInput) -> MutationAttempt[CreateTicketOutput]:
        """Send the ticket; resend an unknown outcome only while the send cap allows."""
        empty = CreateTicketOutput(Ticket(status=""))
        try:
            _validate_text("subject", input.subject, 255)
            _validate_text("description", input.description, 65535)
            description = (
                html.escape(input.description.replace("\r\n", "\n"), quote=True)
                .replace("&#x27;", "&#39;")
                .replace("&quot;", "&#34;")
                .replace("\n", "<br>")
            )
            if len(description) > 65535:
                raise ValueError(
                    "description is longer than Zoho Desk's 65,535-character limit once escaped as HTML"
                )
            _validate_id("departmentId", input.department_id)
            contact = input.contact
            has_email = bool(contact and contact.email)
            if bool(input.contact_id) == has_email:
                raise ValueError("set exactly one of contactId and contact.email")
            if input.contact_id:
                _validate_id("contactId", input.contact_id)
                if contact is not None and any((contact.email, contact.first_name, contact.last_name)):
                    raise ValueError("contact names apply only with contact.email")
            elif contact is None or not _bare_email(contact.email):
                raise ValueError("contact.email must be one bare email address such as jane@example.com")
            if contact:
                for field_name, value in (
                    ("contact.firstName", contact.first_name),
                    ("contact.lastName", contact.last_name),
                ):
                    if value != value.strip() or len(value) > 80:
                        raise ValueError(
                            f"{field_name} must be at most 80 characters without surrounding spaces"
                        )
            if input.status:
                _validate_picklist("status", input.status)
            if input.priority:
                _validate_picklist("priority", input.priority)
            _validate_id("assigneeId", input.assignee_id, True)
        except (ValueError, AttributeError) as error:
            return MutationAttempt.branch_result(
                d.DEFECT, empty, Receipt(), failure("createTicket", FailureKind.VALIDATION, str(error))
            )
        permit, prior = await _begin_write(self.client, call, "createTicket", empty)
        if permit is None:
            assert prior is not None
            return prior
        credentials, base, route = await self.client.resolve(call, "createTicket")
        if route:
            return await _session_write(permit, route, "createTicket", empty)
        assert credentials is not None and base is not None
        payload: dict[str, Any] = {
            "subject": input.subject,
            "description": description,
            "departmentId": input.department_id,
        }
        if input.contact_id:
            payload["contactId"] = input.contact_id
        elif contact:
            payload["contact"] = {"email": contact.email}
            payload["email"] = contact.email
            if contact.first_name:
                payload["contact"]["firstName"] = contact.first_name
            if contact.last_name:
                payload["contact"]["lastName"] = contact.last_name
        for source, target in (("status", "status"), ("priority", "priority"), ("assignee_id", "assigneeId")):
            value = getattr(input, source)
            if value:
                payload[target] = value
        result, refused = await _send_write(
            permit, self.client, call, "createTicket", credentials, base, "/tickets", payload
        )
        if refused:
            return refused
        receipt = self.client.receipt(call, result)
        routed = await _write_attempt_for_exchange(
            permit, self.client, call, credentials, result, receipt, empty
        )
        if routed:
            return routed
        try:
            ticket, _ = _ticket_body(result.body)
        except ValueError as error:
            return await _accepted_write_attempt(
                permit,
                self.client,
                call,
                result,
                failure(
                    "createTicket",
                    FailureKind.PROTOCOL,
                    "Zoho Desk accepted the ticket but returned an invalid ticket: " + str(error),
                ),
                receipt,
                empty,
            )
        await _record_accepted_write(permit, ticket.id)
        return MutationAttempt.branch_result(
            d.CREATED, CreateTicketOutput(ticket), self.client.receipt(call, result, ticket.id)
        )


class UpdateTicketOperation:
    """Set absolute ticket values after reading the current state."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated operation definition."""
        return d.UPDATE_TICKET_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpdateTicketInput) -> IdempotencyKey:
        """Correlate repeated absolute writes with the Call ID."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: UpdateTicketInput) -> MutationAttempt[UpdateTicketOutput]:
        """Read current values and PATCH only requested differences."""
        empty = UpdateTicketOutput(Ticket(status=""))
        try:
            _validate_id("ticketId", input.ticket_id)
            for name, value in (("status", input.status), ("priority", input.priority)):
                if value:
                    _validate_picklist(name, value)
            _validate_id("assigneeId", input.assignee_id, True)
            _validate_id("departmentId", input.department_id, True)
            if not any((input.status, input.priority, input.assignee_id, input.department_id)):
                raise ValueError("at least one of status, priority, assigneeId, or departmentId is required")
        except (ValueError, AttributeError) as error:
            return MutationAttempt.branch_result(
                d.DEFECT, empty, Receipt(), failure("updateTicket", FailureKind.VALIDATION, str(error))
            )
        credentials, base, route = await self.client.resolve(call, "updateTicket")
        if route:
            return _session_mutation(route, "updateTicket", empty)
        assert credentials is not None and base is not None
        path = _path(input.ticket_id)
        result = await self.client.exchange(call, "updateTicket", credentials, base, "GET", path)
        receipt = self.client.receipt(call, result, input.ticket_id)
        routed = _mutation_attempt(result, receipt, empty, d.INVALID_RESPONSE, d.NOT_FOUND)
        if routed:
            return routed
        try:
            current, _ = _ticket_body(result.body, input.ticket_id)
        except ValueError as error:
            return MutationAttempt.branch_result(
                d.INVALID_RESPONSE,
                empty,
                receipt,
                failure(
                    "updateTicket",
                    FailureKind.PROTOCOL,
                    "Zoho Desk returned an invalid ticket: " + str(error),
                ),
            )
        current_output = UpdateTicketOutput(current)
        change: dict[str, str] = {}
        if input.status and input.status.casefold() != current.status.casefold():
            change["status"] = input.status
        if input.priority and input.priority.casefold() != (current.priority or "").casefold():
            change["priority"] = input.priority
        has_department_change = bool(input.department_id and input.department_id != current.department_id)
        if input.assignee_id and (input.assignee_id != current.assignee_id or has_department_change):
            change["assigneeId"] = input.assignee_id
        if has_department_change:
            change["departmentId"] = input.department_id
        if not change:
            return MutationAttempt.branch_result(d.UPDATED, UpdateTicketOutput(current, True), receipt)
        result = await self.client.exchange(
            call, "updateTicket", credentials, base, "PATCH", path, payload=change
        )
        receipt = self.client.receipt(call, result, input.ticket_id)
        if result.outcome == "invalid" or (
            result.outcome == "rejected" and result.status not in _DEFINITE_REFUSAL_STATUSES
        ):
            # The PATCH sets final values, so an unknown outcome repeats it.
            return MutationAttempt.retry(_required_failure(result), result.retry_after_seconds)
        routed = _mutation_attempt(result, receipt, current_output, d.INVALID_RESPONSE, d.NOT_FOUND)
        if routed:
            return routed
        try:
            updated, _ = _ticket_body(result.body, input.ticket_id)
        except ValueError as error:
            return MutationAttempt.retry(
                failure(
                    "updateTicket",
                    FailureKind.PROTOCOL,
                    "Zoho Desk accepted the change but returned an invalid updated ticket: " + str(error),
                )
            )
        return MutationAttempt.branch_result(d.UPDATED, UpdateTicketOutput(updated), receipt)


class AddCommentOperation:
    """Add a public or private comment under the SDK single-dispatch fence and send cap."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated operation definition."""
        return d.ADD_COMMENT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: AddCommentInput) -> IdempotencyKey:
        """Correlate the Call ID; Zoho provides no idempotency key."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: AddCommentInput) -> MutationAttempt[AddCommentOutput]:
        """Send the comment; resend an unknown outcome only while the send cap allows."""
        empty = AddCommentOutput()
        try:
            _validate_id("ticketId", input.ticket_id)
            _validate_text("content", input.content, 32000)
        except (ValueError, AttributeError) as error:
            return MutationAttempt.branch_result(
                d.DEFECT, empty, Receipt(), failure("addComment", FailureKind.VALIDATION, str(error))
            )
        permit, prior = await _begin_write(self.client, call, "addComment", empty)
        if permit is None:
            assert prior is not None
            return prior
        credentials, base, route = await self.client.resolve(call, "addComment")
        if route:
            return await _session_write(permit, route, "addComment", empty)
        assert credentials is not None and base is not None
        result, refused = await _send_write(
            permit,
            self.client,
            call,
            "addComment",
            credentials,
            base,
            _path(input.ticket_id) + "/comments",
            {"content": input.content, "isPublic": input.is_public, "contentType": "plainText"},
        )
        if refused:
            return refused
        receipt = self.client.receipt(call, result)
        routed = await _write_attempt_for_exchange(
            permit, self.client, call, credentials, result, receipt, empty, d.NOT_FOUND
        )
        if routed:
            return routed
        try:
            comment = _comment(json.loads(result.body))
            if comment.is_public != input.is_public:
                raise ValueError("comment response has another visibility than requested")
        except (ValueError, json.JSONDecodeError) as error:
            return await _accepted_write_attempt(
                permit,
                self.client,
                call,
                result,
                failure(
                    "addComment",
                    FailureKind.PROTOCOL,
                    "Zoho Desk accepted the comment but returned an invalid comment: " + str(error),
                ),
                receipt,
                empty,
            )
        await _record_accepted_write(permit, comment.id)
        return MutationAttempt.branch_result(
            d.ADDED, AddCommentOutput(comment), self.client.receipt(call, result, comment.id)
        )
