# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Gorgias operation invocation, validation, retry, and reconciliation."""

import html
import json
import re
from dataclasses import dataclass, replace
from functools import cmp_to_key
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
from dex_connectors.sdk.singledispatch import (
    CheckpointError,
    Decision,
    DecisionKind,
    DispatchNotAllowedError,
    Fence,
    InsufficientAttemptTimeError,
)

from ._generated import definitions as defs
from .client import (
    Client,
    Exchange,
    bare_email,
    decode_customer,
    decode_list,
    decode_message,
    decode_ticket,
    failure,
    parse_time,
)
from .models import (
    AddNoteInput,
    AddNoteOutput,
    CreateTicketInput,
    CreateTicketOutput,
    CustomerMatches,
    FindCustomerByEmailInput,
    GetTicketInput,
    SearchTicketsInput,
    SearchTicketsOutput,
    Ticket,
    TicketDetails,
    TicketMessage,
    UpdateTicketInput,
    UpdateTicketOutput,
)

_ZERO_TICKET = Ticket(status="")
_RETRY_OUTCOMES = frozenset(("rateLimited", "notSent", "unavailable"))
# Only these statuses prove Gorgias applied nothing; any other non-2xx answer may follow an applied write.
_DEFINITE_REFUSAL_STATUSES = frozenset({400, 401, 403, 404, 411, 413, 414, 415, 422})
_STATUSES = frozenset(("open", "closed"))
_PRIORITIES = frozenset(("low", "normal", "high", "critical"))
_CURSOR = re.compile(r"^[A-Za-z0-9+/=_.:-]{1,2048}$")
_RFC3339 = re.compile(r"^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:Z|[+-]\d\d:\d\d)$")


def _validate_tags(values: tuple[str, ...], field: str) -> None:
    if len(values) > 20:
        raise ValueError(f"{field} lists more than 20 tags")
    if any(
        not isinstance(value, str)
        or not value
        or value.strip() != value
        or len(value) > 256
        or any(ord(character) < 32 or ord(character) == 127 for character in value)
        for value in values
    ):
        raise ValueError(
            f"{field} entries must be 1 to 256 characters without surrounding spaces or control characters"
        )
    if len(values) != len(set(values)):
        raise ValueError(f"{field} lists a tag twice")


def _validate_status(value: str | None) -> None:
    if value and value not in _STATUSES:
        raise ValueError("status is not a Gorgias status; use open or closed")


def _validate_priority(value: str | None) -> None:
    if value and value not in _PRIORITIES:
        raise ValueError("priority is not a Gorgias priority; use low, normal, high, or critical")


def _validate_text(value: str, field: str) -> None:
    if not isinstance(value, str) or not value.strip():
        raise ValueError(f"{field} is required")


def _query_exchange(
    exchange: Exchange, receipt: Receipt, zero: Any, has_not_found: bool = False
) -> QueryAttempt[Any] | None:
    if exchange.outcome == "success":
        return None
    assert exchange.failure is not None
    if exchange.outcome in _RETRY_OUTCOMES:
        return QueryAttempt.retry(exchange.failure, exchange.retry_after)
    branch = (
        defs.NOT_FOUND
        if has_not_found and exchange.outcome == "notFound"
        else defs.INVALID_RESPONSE
        if exchange.outcome == "invalid"
        else defs.DEFECT
        if exchange.outcome == "defect"
        else defs.PROVIDER_REJECTED
    )
    return QueryAttempt.branch_result(branch, zero, receipt, exchange.failure)


def _mutation_exchange(
    exchange: Exchange,
    receipt: Receipt,
    zero: Any,
    has_not_found: bool = False,
    retry_invalid: bool = False,
    reject_invalid: bool = False,
) -> MutationAttempt[Any] | None:
    if exchange.outcome == "success":
        return None
    assert exchange.failure is not None
    if exchange.outcome in _RETRY_OUTCOMES or (retry_invalid and exchange.outcome == "invalid"):
        return MutationAttempt.retry(exchange.failure, exchange.retry_after)
    branch = (
        defs.NOT_FOUND
        if has_not_found and exchange.outcome == "notFound"
        else defs.PROVIDER_REJECTED
        if reject_invalid and exchange.outcome == "invalid"
        else defs.INVALID_RESPONSE
        if exchange.outcome == "invalid"
        else defs.DEFECT
        if exchange.outcome == "defect"
        else defs.PROVIDER_REJECTED
    )
    return MutationAttempt.branch_result(branch, zero, receipt, exchange.failure)


def _customer_matches(body: bytes, email: str) -> CustomerMatches:
    document = json.loads(body)
    if isinstance(document, list):
        records, cursor = document, ""
    else:
        records, cursor = decode_list(body, "customer")
    customers = tuple(decode_customer(record) for record in records)
    return CustomerMatches(
        email,
        tuple(customer for customer in customers if customer.email.casefold() == email.casefold()),
        bool(cursor),
    )


class SearchTicketsOperation:
    """List a filtered page of Gorgias tickets."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return generated operation identity and Step defaults."""
        return defs.SEARCH_TICKETS_DEFINITION

    async def invoke(self, call: Call, input: SearchTicketsInput) -> QueryAttempt[SearchTicketsOutput]:
        """Look up an optional requester and list one ticket page."""
        operation = defs.SEARCH_TICKETS_OPERATION_ID
        zero = SearchTicketsOutput()
        try:
            if input.requester_email and not bare_email(input.requester_email):
                raise ValueError("requesterEmail must be one bare email address")
            if input.requester_email and input.requester_id:
                raise ValueError("set requesterEmail or requesterId, not both")
            if input.requester_id < 0 or input.view_id < 0:
                raise ValueError("requesterId and viewId cannot be negative")
            _validate_tags(input.tags, "tags")
            if len(input.statuses) != len(set(input.statuses)) or any(
                status not in _STATUSES for status in input.statuses
            ):
                raise ValueError("statuses must be distinct Gorgias statuses")
            if len(input.priorities) != len(set(input.priorities)) or any(
                priority not in _PRIORITIES for priority in input.priorities
            ):
                raise ValueError("priorities must be distinct Gorgias priorities")
            if not 0 <= input.page_size <= 100:
                raise ValueError("pageSize must be between 1 and 100, or zero for 30")
            if input.cursor and not _CURSOR.fullmatch(input.cursor):
                raise ValueError("cursor must be a nextCursor value returned by searchTickets")
            if input.updated_since and not _RFC3339.fullmatch(input.updated_since):
                raise ValueError("updatedSince must be an RFC 3339 instant with an explicit offset")
            updated_since = parse_time(input.updated_since) if input.updated_since else None
        except (ValueError, TypeError) as error:
            return QueryAttempt.branch_result(
                defs.DEFECT, zero, Receipt(), failure(FailureKind.VALIDATION, operation, str(error))
            )
        credentials, credential_failure = await self.client.resolve_credentials(call, operation)
        if credential_failure:
            return QueryAttempt.branch_result(defs.DEFECT, zero, Receipt(), credential_failure)
        assert credentials is not None
        requester_id = input.requester_id
        if input.requester_email:
            exchange = await self.client.exchange(
                call,
                credentials,
                operation,
                "GET",
                "/customers",
                {"email": input.requester_email, "limit": "10"},
            )
            receipt = self.client.receipt(call, exchange)
            terminal = _query_exchange(exchange, receipt, zero)
            if terminal:
                return terminal
            try:
                matches = _customer_matches(exchange.body, input.requester_email)
                if len(matches.customers) > 1:
                    raise ValueError("more than one customer has this primary email address")
            except (ValueError, TypeError, KeyError, AttributeError) as error:
                return QueryAttempt.branch_result(
                    defs.INVALID_RESPONSE,
                    zero,
                    receipt,
                    failure(
                        FailureKind.PROTOCOL,
                        operation,
                        "Gorgias returned an invalid customer page: " + str(error),
                    ),
                )
            if not matches.customers:
                return QueryAttempt.branch_result(defs.SEARCHED, zero, receipt)
            requester_id = matches.customers[0].id
        query = {"order_by": "updated_datetime:desc", "limit": str(input.page_size or 30), "trashed": "false"}
        if requester_id:
            query["customer_id"] = str(requester_id)
        if input.view_id:
            query["view_id"] = str(input.view_id)
        if input.cursor:
            query["cursor"] = input.cursor
        exchange = await self.client.exchange(call, credentials, operation, "GET", "/tickets", query)
        receipt = self.client.receipt(call, exchange)
        terminal = _query_exchange(exchange, receipt, zero)
        if terminal:
            return terminal
        try:
            records, cursor = decode_list(exchange.body, "ticket")
            if len(records) > 100:
                raise ValueError("ticket page holds more tickets than Gorgias's page size")
            tickets = []
            for record in records:
                ticket = decode_ticket(record)
                if updated_since and ticket.updated_at < updated_since:
                    if not input.view_id:
                        cursor = ""
                    continue
                if (
                    input.statuses
                    and ticket.status not in input.statuses
                    or input.priorities
                    and ticket.priority not in input.priorities
                    or input.tags
                    and not set(ticket.tags).intersection(input.tags)
                ):
                    continue
                tickets.append(ticket)
            output = SearchTicketsOutput(tuple(tickets), cursor, len(records))
        except (ValueError, TypeError, KeyError, AttributeError) as error:
            return QueryAttempt.branch_result(
                defs.INVALID_RESPONSE,
                zero,
                receipt,
                failure(
                    FailureKind.PROTOCOL, operation, "Gorgias returned an invalid ticket page: " + str(error)
                ),
            )
        return QueryAttempt.branch_result(defs.SEARCHED, output, receipt)


class GetTicketOperation:
    """Read one ticket and its latest messages."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return generated operation identity and Step defaults."""
        return defs.GET_TICKET_DEFINITION

    async def invoke(self, call: Call, input: GetTicketInput) -> QueryAttempt[TicketDetails]:
        """Read and decode a bounded set of messages."""
        operation = defs.GET_TICKET_OPERATION_ID
        zero = TicketDetails(_ZERO_TICKET)
        if input.ticket_id < 1 or not 0 <= input.latest_message_limit <= 20:
            return QueryAttempt.branch_result(
                defs.DEFECT,
                zero,
                Receipt(),
                failure(
                    FailureKind.VALIDATION,
                    operation,
                    "ticketId must be positive and latestMessageLimit must be 0 through 20",
                ),
            )
        credentials, credential_failure = await self.client.resolve_credentials(call, operation)
        if credential_failure:
            return QueryAttempt.branch_result(defs.DEFECT, zero, Receipt(), credential_failure)
        assert credentials is not None
        exchange = await self.client.exchange(
            call, credentials, operation, "GET", f"/tickets/{input.ticket_id}"
        )
        receipt = self.client.receipt(call, exchange, input.ticket_id)
        terminal = _query_exchange(exchange, receipt, zero, True)
        if terminal:
            return terminal
        try:
            record = json.loads(exchange.body)
            ticket = decode_ticket(record, input.ticket_id)
            requester = decode_customer(record["customer"]) if record.get("customer") is not None else None
            messages = []
            for wire in record.get("messages") or ():
                message = decode_message(wire)
                if message.ticket_id and message.ticket_id != ticket.id:
                    raise ValueError("message belongs to another ticket")
                messages.append(replace(message, ticket_id=ticket.id))
            messages.sort(key=lambda message: (message.created_at, message.id), reverse=True)
            limit = input.latest_message_limit or 5
            details = TicketDetails(ticket, requester, tuple(messages[:limit]), len(messages) > limit)
        except (ValueError, TypeError, KeyError, AttributeError) as error:
            return QueryAttempt.branch_result(
                defs.INVALID_RESPONSE,
                zero,
                receipt,
                failure(FailureKind.PROTOCOL, operation, "Gorgias returned an invalid ticket: " + str(error)),
            )
        return QueryAttempt.branch_result(defs.FOUND, details, receipt)


class FindCustomerByEmailOperation:
    """Look up Gorgias customers by primary email address."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return generated operation identity and Step defaults."""
        return defs.FIND_CUSTOMER_BY_EMAIL_DEFINITION

    async def invoke(self, call: Call, input: FindCustomerByEmailInput) -> QueryAttempt[CustomerMatches]:
        """Return primary address matches or a definite absent branch."""
        operation = defs.FIND_CUSTOMER_BY_EMAIL_OPERATION_ID
        zero = CustomerMatches()
        if not bare_email(input.email):
            return QueryAttempt.branch_result(
                defs.DEFECT,
                zero,
                Receipt(),
                failure(
                    FailureKind.VALIDATION,
                    operation,
                    "email must be one bare email address such as jane@example.com",
                ),
            )
        credentials, credential_failure = await self.client.resolve_credentials(call, operation)
        if credential_failure:
            return QueryAttempt.branch_result(defs.DEFECT, zero, Receipt(), credential_failure)
        assert credentials is not None
        exchange = await self.client.exchange(
            call, credentials, operation, "GET", "/customers", {"email": input.email, "limit": "10"}
        )
        receipt = self.client.receipt(call, exchange)
        terminal = _query_exchange(exchange, receipt, zero)
        if terminal:
            return terminal
        try:
            matches = _customer_matches(exchange.body, input.email)
        except (ValueError, TypeError, KeyError, AttributeError) as error:
            return QueryAttempt.branch_result(
                defs.INVALID_RESPONSE,
                zero,
                receipt,
                failure(
                    FailureKind.PROTOCOL,
                    operation,
                    "Gorgias returned an invalid customer page: " + str(error),
                ),
            )
        if not matches.customers:
            return QueryAttempt.branch_result(defs.NOT_FOUND, matches, receipt)
        object_id = matches.customers[0].id if len(matches.customers) == 1 else 0
        return QueryAttempt.branch_result(defs.FOUND, matches, self.client.receipt(call, exchange, object_id))


class UpdateTicketOperation:
    """Apply only the ticket fields and tags that differ from the current ticket."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return generated operation identity and Step defaults."""
        return defs.UPDATE_TICKET_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpdateTicketInput) -> IdempotencyKey:
        """Use the stable Call ID for safe absolute ticket updates."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: UpdateTicketInput) -> MutationAttempt[UpdateTicketOutput]:
        """Read, compare, write differing tags and fields, and confirm the result."""
        operation = defs.UPDATE_TICKET_OPERATION_ID
        zero = UpdateTicketOutput(_ZERO_TICKET)
        try:
            if input.ticket_id < 1:
                raise ValueError("ticketId must be a positive Gorgias ticket ID")
            _validate_status(input.status)
            _validate_priority(input.priority)
            if input.assignee_user_id < 0 or input.assignee_team_id < 0:
                raise ValueError("assignee IDs cannot be negative")
            _validate_tags(input.add_tags, "addTags")
            _validate_tags(input.remove_tags, "removeTags")
            if set(input.add_tags).intersection(input.remove_tags):
                raise ValueError("a tag is in both addTags and removeTags")
            if not (
                input.status
                or input.priority
                or input.assignee_user_id
                or input.assignee_team_id
                or input.add_tags
                or input.remove_tags
            ):
                raise ValueError("at least one ticket change is required")
        except (ValueError, TypeError) as error:
            return MutationAttempt.branch_result(
                defs.DEFECT, zero, Receipt(), failure(FailureKind.VALIDATION, operation, str(error))
            )
        credentials, credential_failure = await self.client.resolve_credentials(call, operation)
        if credential_failure:
            return MutationAttempt.branch_result(defs.DEFECT, zero, Receipt(), credential_failure)
        assert credentials is not None
        path = f"/tickets/{input.ticket_id}"
        read = await self.client.exchange(call, credentials, operation, "GET", path)
        receipt = self.client.receipt(call, read, input.ticket_id)
        terminal = _mutation_exchange(read, receipt, zero, True)
        if terminal:
            return terminal
        try:
            ticket = decode_ticket(json.loads(read.body), input.ticket_id)
        except (ValueError, TypeError, KeyError, AttributeError) as error:
            return MutationAttempt.branch_result(
                defs.INVALID_RESPONSE,
                zero,
                receipt,
                failure(FailureKind.PROTOCOL, operation, "Gorgias returned an invalid ticket: " + str(error)),
            )
        fields: dict[str, Any] = {}
        if input.status and input.status != ticket.status:
            fields["status"] = input.status
        if input.priority and input.priority != ticket.priority:
            fields["priority"] = input.priority
        if input.assignee_user_id and input.assignee_user_id != ticket.assignee_user_id:
            fields["assignee_user"] = {"id": input.assignee_user_id}
        if input.assignee_team_id and input.assignee_team_id != ticket.assignee_team_id:
            fields["assignee_team"] = {"id": input.assignee_team_id}
        add_tags = [tag for tag in input.add_tags if tag not in ticket.tags]
        remove_tags = [tag for tag in input.remove_tags if tag in ticket.tags]
        if not fields and not add_tags and not remove_tags:
            return MutationAttempt.branch_result(
                defs.UPDATED,
                UpdateTicketOutput(ticket, True),
                self.client.receipt(call, Exchange("success"), ticket.id),
            )
        writes = []
        if add_tags:
            writes.append(("POST", path + "/tags", {"names": add_tags}))
        if remove_tags:
            writes.append(("DELETE", path + "/tags", {"names": remove_tags}))
        if fields:
            writes.append(("PUT", path, fields))
        current_output = UpdateTicketOutput(ticket)
        has_write = False
        for method, request_path, payload in writes:
            result = await self.client.exchange(
                call, credentials, operation, method, request_path, payload=payload
            )
            receipt = self.client.receipt(call, result, input.ticket_id)
            if has_write and _is_conclusive_write_failure(result):
                reread = await self.client.exchange(call, credentials, operation, "GET", path)
                if reread.outcome == "success":
                    try:
                        current_output = UpdateTicketOutput(
                            decode_ticket(json.loads(reread.body), input.ticket_id)
                        )
                    except (ValueError, TypeError, KeyError, AttributeError):
                        pass
            terminal = _after_write_exchange(result, receipt, current_output)
            if terminal:
                return terminal
            has_write = True
            if method == "PUT":
                try:
                    updated = decode_ticket(json.loads(result.body), input.ticket_id)
                except (ValueError, TypeError, KeyError, AttributeError) as error:
                    # The PUT sets final values, so an unusable answer repeats it.
                    return MutationAttempt.retry(
                        failure(
                            FailureKind.PROTOCOL,
                            operation,
                            "Gorgias accepted the change but returned an invalid updated ticket: "
                            + str(error),
                        )
                    )
                return MutationAttempt.branch_result(defs.UPDATED, UpdateTicketOutput(updated), receipt)
        reread = await self.client.exchange(call, credentials, operation, "GET", path)
        receipt = self.client.receipt(call, reread, input.ticket_id)
        terminal = _after_write_exchange(reread, receipt, current_output)
        if terminal:
            return terminal
        try:
            updated = decode_ticket(json.loads(reread.body), input.ticket_id)
        except (ValueError, TypeError, KeyError, AttributeError) as error:
            return MutationAttempt.retry(
                failure(
                    FailureKind.PROTOCOL,
                    operation,
                    "Gorgias returned an invalid ticket after the change: " + str(error),
                )
            )
        return MutationAttempt.branch_result(
            defs.UPDATED,
            UpdateTicketOutput(updated),
            self.client.receipt(call, Exchange("success"), updated.id),
        )


def _is_conclusive_write_failure(exchange: Exchange) -> bool:
    """Report a definite refusal or a request that was never built."""
    if exchange.outcome in ("notFound", "defect"):
        return True
    return exchange.outcome == "rejected" and exchange.status in _DEFINITE_REFUSAL_STATUSES


def _after_write_exchange(exchange: Exchange, receipt: Receipt, value: Any) -> MutationAttempt[Any] | None:
    """After a possible write, only a definite refusal selects a branch; anything else retries the change."""
    if exchange.outcome != "success" and not _is_conclusive_write_failure(exchange):
        assert exchange.failure is not None
        return MutationAttempt.retry(exchange.failure, exchange.retry_after)
    return _mutation_exchange(exchange, receipt, value, True)


def _reconcile_delay(retry_after: float, minimum: float) -> float:
    """Honor a Gorgias Retry-After of at least `minimum` seconds, capped at one minute."""
    return min(max(retry_after, minimum), 60)


@dataclass(frozen=True, slots=True)
class _WritePermit:
    """A fence decision that lets this attempt read back and send, with its send-cap facts."""

    fence: Fence
    decision: Decision

    @property
    def is_first_send(self) -> bool:
        return self.decision.prior_send_upper_bound == 0

    @property
    def needs_read_back(self) -> bool:
        return self.decision.kind != DecisionKind.SEND_ALLOWED or self.decision.prior_send_upper_bound > 0

    @property
    def can_send_after_unconfirmed_read_back(self) -> bool:
        has_saved_id = self.decision.marker is not None and bool(self.decision.marker.provider_id)
        return self.decision.kind != DecisionKind.READ_BACK_REQUIRED and not has_saved_id


async def _begin_write(
    call: Call, operation: str, definition: MutationDefinition, zero: Any
) -> tuple[_WritePermit | None, MutationAttempt[Any] | None]:
    """Start the SDK fence under the manifest send cap; an attempt ends this attempt before any request."""
    try:
        fence = Fence(call, definition.step_defaults.execute_method_timeout, max_sends=call.max_sends)
    except ValueError:
        return None, MutationAttempt.branch_result(
            defs.DEFECT,
            zero,
            Receipt(),
            failure(FailureKind.LOCAL_DEFECT, operation, "single-dispatch fence could not start"),
        )
    decision = await fence.decide()
    if decision.kind == DecisionKind.FAIL_CLOSED:
        # FailClosed sends nothing, so a later attempt decides again from a readable checkpoint.
        return None, MutationAttempt.retry(
            failure(
                FailureKind.AVAILABILITY,
                operation,
                "the dispatch checkpoint could not be read; nothing was sent in this attempt",
            )
        )
    return _WritePermit(fence, decision), None


async def _credential_failure_attempt(
    permit: _WritePermit, operation: str, reason: Failure, zero: Any
) -> MutationAttempt[Any]:
    if not permit.is_first_send:
        # notSent would hide the earlier possible send, so nothing is recorded.
        return MutationAttempt.uncertain(
            zero,
            failure(
                reason.kind,
                operation,
                "an earlier attempt of this Step may have sent the request and the connection "
                "credentials are unavailable to read it back",
            ),
            Receipt(),
        )
    try:
        await permit.fence.record_not_sent()
    except Exception:
        return MutationAttempt.retry(
            failure(FailureKind.AVAILABILITY, operation, "Dex did not record the not-sent checkpoint")
        )
    return MutationAttempt.branch_result(defs.DEFECT, zero, Receipt(), reason)


def _read_back_attempt(
    permit: _WritePermit,
    exchange: Exchange,
    found: MutationAttempt[Any] | None,
    receipt: Receipt,
    operation: str,
    resource: str,
    zero: Any,
) -> MutationAttempt[Any] | None:
    """Map an external-ID lookup; None means this attempt may send."""
    if exchange.outcome == "success" and found is not None:
        return found
    if exchange.outcome in _RETRY_OUTCOMES:
        assert exchange.failure is not None
        return MutationAttempt.retry(exchange.failure, _reconcile_delay(exchange.retry_after, 15))
    if permit.can_send_after_unconfirmed_read_back:
        return None
    if exchange.outcome == "success":
        reason = failure(
            FailureKind.TRANSPORT,
            operation,
            "earlier attempts of this Step may have sent the request, the send cap is reached, "
            f"and Gorgias shows no {resource} with its external ID",
        )
    else:
        assert exchange.failure is not None
        reason = failure(
            exchange.failure.kind,
            operation,
            "earlier attempts of this Step may have sent the request, the send cap is reached, "
            "and Gorgias could not confirm it: " + exchange.failure.message,
        )
    return MutationAttempt.uncertain(zero, reason, receipt)


async def _pre_send_retry(permit: _WritePermit, reason: Failure, retry_after: float) -> MutationAttempt[Any]:
    if permit.decision.kind == DecisionKind.SEND_ALLOWED:
        try:
            await permit.fence.record_not_sent()
        except Exception:
            pass  # A lost notSent leaves no marker, so the next attempt reads back before it sends.
    return MutationAttempt.retry(reason, retry_after)


async def _send_write(
    permit: _WritePermit,
    client: Client,
    call: Call,
    credentials: Any,
    operation: str,
    path: str,
    payload: dict[str, Any],
) -> tuple[Exchange | None, MutationAttempt[Any] | None]:
    """Dispatch one POST under the fence; a refused dispatch returns its Retry attempt."""
    try:
        exchange = await permit.fence.dispatch(
            lambda _: client.exchange(call, credentials, operation, "POST", path, payload=payload)
        )
    except (CheckpointError, InsufficientAttemptTimeError, DispatchNotAllowedError):
        # Dispatch refused before sending, so nothing reached Gorgias in this attempt.
        return None, MutationAttempt.retry(
            failure(
                FailureKind.AVAILABILITY,
                operation,
                "Dex did not record the dispatch checkpoint or too little attempt time remained; "
                "nothing was sent",
            )
        )
    except TimeoutError:
        # The claim stays, so the next attempt counts this send and reads back first.
        return None, MutationAttempt.retry(
            failure(FailureKind.TRANSPORT, operation, "Gorgias request failed before a response arrived"), 2
        )
    return exchange, None


async def _write_attempt(
    permit: _WritePermit, exchange: Exchange, receipt: Receipt, zero: Any, not_found_branch: str
) -> MutationAttempt[Any] | None:
    """Map a sent write; any outcome but a definite refusal retries, and the next attempt reads back."""
    if exchange.outcome == "success":
        return None
    assert exchange.failure is not None
    if exchange.outcome in ("rateLimited", "notSent") or (
        exchange.outcome == "unavailable" and exchange.status == 503
    ):
        try:
            await permit.fence.record_can_resend()
        except Exception:
            pass  # A lost checkpoint leaves the claim, so the next attempt counts this send and reads back.
        return MutationAttempt.retry(exchange.failure, exchange.retry_after)
    branch = ""
    if exchange.outcome == "notFound":
        branch = not_found_branch
    elif exchange.outcome == "defect" and permit.is_first_send:
        branch, receipt = defs.DEFECT, Receipt()
    elif exchange.outcome == "rejected" and exchange.status in _DEFINITE_REFUSAL_STATUSES:
        branch = defs.PROVIDER_REJECTED
    if not branch:
        # A 3xx, 408, 409, other 5xx, unlisted status, or unusable 2xx may follow an applied write.
        return MutationAttempt.retry(exchange.failure, _reconcile_delay(exchange.retry_after, 2))
    if not permit.is_first_send:
        return MutationAttempt.uncertain(zero, exchange.failure, receipt)
    try:
        await permit.fence.record_can_resend()
    except Exception:
        return MutationAttempt.retry(
            failure(
                FailureKind.AVAILABILITY,
                exchange.failure.operation,
                "Dex did not record the refusal checkpoint",
            )
        )
    return MutationAttempt.branch_result(branch, zero, receipt, exchange.failure)


async def _record_accepted_write(permit: _WritePermit, provider_id: int) -> None:
    try:
        await permit.fence.record_provider_id(str(provider_id))
    except Exception:
        pass  # The accepted response already identifies the write.


def _html_body(value: str) -> str:
    return (
        html.escape(value.replace("\r\n", "\n"), quote=True).replace("&#x27;", "&#39;").replace("\n", "<br>")
    )


class CreateTicketOperation:
    """Create a ticket once and reconcile retries using its external ID."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return generated operation identity and Step defaults."""
        return defs.CREATE_TICKET_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreateTicketInput) -> IdempotencyKey:
        """Prefix the Call ID used as Gorgias external_id."""
        return IdempotencyKey("dex-" + str(call_id))

    async def invoke(self, call: Call, input: CreateTicketInput) -> MutationAttempt[CreateTicketOutput]:
        """Read back a possibly accepted write by external ID, then send under the fence and send cap."""
        operation = defs.CREATE_TICKET_OPERATION_ID
        zero = CreateTicketOutput(_ZERO_TICKET)
        requester = input.requester
        if isinstance(requester, dict):
            from .models import TicketRequesterInput

            requester = TicketRequesterInput(**requester)
        try:
            _validate_text(input.subject, "subject")
            _validate_text(input.description, "description")
            if len(input.subject) > 998:
                raise ValueError("subject is longer than Gorgias's 998-character limit")
            if not bare_email(requester.email):
                raise ValueError("requester.email must be one bare email address")
            if requester.name != requester.name.strip():
                raise ValueError("requester.name cannot have surrounding spaces")
            _validate_status(input.status)
            _validate_priority(input.priority)
            _validate_tags(input.tags, "tags")
            if input.assignee_user_id < 0 or input.assignee_team_id < 0:
                raise ValueError("assignee IDs cannot be negative")
        except (ValueError, TypeError, AttributeError) as error:
            return MutationAttempt.branch_result(
                defs.DEFECT, zero, Receipt(), failure(FailureKind.VALIDATION, operation, str(error))
            )
        permit, ended = await _begin_write(call, operation, defs.CREATE_TICKET_DEFINITION, zero)
        if ended is not None:
            return ended
        assert permit is not None
        credentials, credential_failure = await self.client.resolve_credentials(call, operation)
        if credential_failure:
            return await _credential_failure_attempt(permit, operation, credential_failure, zero)
        assert credentials is not None
        if permit.needs_read_back:
            ticket, lookup, receipt = await self._look_up_ticket(call, credentials)
            found = (
                MutationAttempt.branch_result(defs.CREATED, CreateTicketOutput(ticket, True), receipt)
                if ticket is not None
                else None
            )
            read_back = _read_back_attempt(permit, lookup, found, receipt, operation, "ticket", zero)
            if read_back is not None:
                return read_back
            # Gorgias shows no ticket with the external ID, so this attempt resends below the cap.
        person = {"email": requester.email}
        if requester.name:
            person["name"] = requester.name
        payload: dict[str, Any] = {
            "customer": person,
            "subject": input.subject,
            "channel": "api",
            "via": "api",
            "from_agent": False,
            "external_id": str(call.idempotency_key),
            "messages": [
                {
                    "channel": "api",
                    "via": "api",
                    "from_agent": False,
                    "sender": person,
                    "subject": input.subject,
                    "body_text": input.description,
                    "body_html": _html_body(input.description),
                    "stripped_text": input.description,
                }
            ],
        }
        if input.status:
            payload["status"] = input.status
        if input.priority:
            payload["priority"] = input.priority
        if input.tags:
            payload["tags"] = [{"name": tag} for tag in input.tags]
        if input.assignee_user_id:
            payload["assignee_user"] = {"id": input.assignee_user_id}
        if input.assignee_team_id:
            payload["assignee_team"] = {"id": input.assignee_team_id}
        result, refused = await _send_write(
            permit, self.client, call, credentials, operation, "/tickets", payload
        )
        if refused is not None:
            return refused
        assert result is not None
        receipt = self.client.receipt(call, result)
        terminal = await _write_attempt(permit, result, receipt, zero, defs.PROVIDER_REJECTED)
        if terminal is not None:
            return terminal
        try:
            created = decode_ticket(json.loads(result.body))
        except (ValueError, TypeError, KeyError, AttributeError) as error:
            return MutationAttempt.retry(
                failure(
                    FailureKind.PROTOCOL,
                    operation,
                    "Gorgias accepted the ticket but returned an invalid ticket: " + str(error),
                ),
                2,
            )
        await _record_accepted_write(permit, created.id)
        return MutationAttempt.branch_result(
            defs.CREATED, CreateTicketOutput(created), self.client.receipt(call, result, created.id)
        )

    async def _look_up_ticket(self, call: Call, credentials: Any) -> tuple[Ticket | None, Exchange, Receipt]:
        """Find the ticket an earlier attempt may have created, by the external ID it wrote."""
        operation = defs.CREATE_TICKET_OPERATION_ID
        result = await self.client.exchange(
            call,
            credentials,
            operation,
            "GET",
            "/tickets",
            {"external_id": str(call.idempotency_key), "limit": "5"},
        )
        receipt = self.client.receipt(call, result)
        if result.outcome != "success":
            return None, result, receipt
        try:
            records, _ = decode_list(result.body, "ticket")
            for record in records:
                try:
                    ticket = decode_ticket(record)
                except (ValueError, TypeError, KeyError, AttributeError):
                    continue
                if ticket.external_id == str(call.idempotency_key):
                    return ticket, result, self.client.receipt(call, result, ticket.id)
        except (ValueError, TypeError, KeyError, AttributeError) as error:
            result = Exchange(
                "invalid",
                failure(
                    FailureKind.PROTOCOL, operation, "Gorgias returned an invalid ticket page: " + str(error)
                ),
                status=result.status,
                headers=result.headers,
            )
        return None, result, receipt


class AddNoteOperation:
    """Create an internal note or routed public reply once per logical call."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return generated operation identity and Step defaults."""
        return defs.ADD_NOTE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: AddNoteInput) -> IdempotencyKey:
        """Prefix the Call ID stored as the message external_id."""
        return IdempotencyKey("dex-" + str(call_id))

    async def invoke(self, call: Call, input: AddNoteInput) -> MutationAttempt[AddNoteOutput]:
        """Read back by external ID, read the reply route, and send under the fence and send cap."""
        operation = defs.ADD_NOTE_OPERATION_ID
        zero = AddNoteOutput()
        try:
            if input.ticket_id < 1:
                raise ValueError("ticketId must be a positive Gorgias ticket ID")
            _validate_text(input.body, "body")
        except (ValueError, TypeError) as error:
            return MutationAttempt.branch_result(
                defs.DEFECT, zero, Receipt(), failure(FailureKind.VALIDATION, operation, str(error))
            )
        permit, ended = await _begin_write(call, operation, defs.ADD_NOTE_DEFINITION, zero)
        if ended is not None:
            return ended
        assert permit is not None
        credentials, credential_failure = await self.client.resolve_credentials(call, operation)
        if credential_failure:
            return await _credential_failure_attempt(permit, operation, credential_failure, zero)
        assert credentials is not None
        path = f"/tickets/{input.ticket_id}"
        if permit.needs_read_back:
            message, lookup, receipt = await self._look_up_message(call, credentials, input.ticket_id)
            found = (
                MutationAttempt.branch_result(defs.ADDED, AddNoteOutput(message, True), receipt)
                if message is not None
                else None
            )
            read_back = _read_back_attempt(permit, lookup, found, receipt, operation, "message", zero)
            if read_back is not None:
                return read_back
            # The ticket shows no message with the external ID, so this attempt resends below the cap.
        payload: dict[str, Any] = {
            "channel": "internal-note",
            "via": "api",
            "from_agent": True,
            "public": False,
            "sender": {"email": credentials.email},
            "body_text": input.body,
            "body_html": _html_body(input.body),
            "external_id": str(call.idempotency_key),
        }
        if input.is_public_reply:
            route_exchange = await self.client.exchange(call, credentials, operation, "GET", path)
            receipt = self.client.receipt(call, route_exchange, input.ticket_id)
            if route_exchange.outcome in _RETRY_OUTCOMES:
                assert route_exchange.failure is not None
                return await _pre_send_retry(permit, route_exchange.failure, route_exchange.retry_after)
            if route_exchange.outcome != "success":
                assert route_exchange.failure is not None
                route_branch = {"notFound": defs.NOT_FOUND, "defect": defs.DEFECT}.get(
                    route_exchange.outcome, defs.PROVIDER_REJECTED
                )
                return _reply_route_failure(permit, route_branch, route_exchange.failure, receipt, zero)
            try:
                ticket_wire = json.loads(route_exchange.body)
                ticket = decode_ticket(ticket_wire, input.ticket_id)
            except (ValueError, TypeError, KeyError, AttributeError) as error:
                return _reply_route_failure(
                    permit,
                    defs.PROVIDER_REJECTED,
                    failure(
                        FailureKind.PROTOCOL,
                        operation,
                        "Gorgias returned an invalid ticket, so the reply could not be routed: " + str(error),
                    ),
                    receipt,
                    zero,
                )
            try:
                route = _reply_route(list(ticket_wire.get("messages") or ()), ticket.subject)
            except (ValueError, TypeError, KeyError, AttributeError) as error:
                return _reply_route_failure(
                    permit,
                    defs.PROVIDER_REJECTED,
                    failure(FailureKind.VALIDATION, operation, str(error)),
                    receipt,
                    zero,
                )
            payload.update(
                {
                    "channel": "email",
                    "public": True,
                    "subject": route[3],
                    "receiver": {"email": route[2]},
                    "source": {"type": "email", "from": {"address": route[1]}, "to": [{"address": route[2]}]},
                }
            )
            if route[0]:
                payload["integration_id"] = route[0]
        result, refused = await _send_write(
            permit, self.client, call, credentials, operation, path + "/messages", payload
        )
        if refused is not None:
            return refused
        assert result is not None
        receipt = self.client.receipt(call, result)
        terminal = await _write_attempt(permit, result, receipt, zero, defs.NOT_FOUND)
        if terminal is not None:
            return terminal
        try:
            record = json.loads(result.body)
            message = decode_message(record)
            if message.ticket_id and message.ticket_id != input.ticket_id:
                raise ValueError("message response is for another ticket")
            if message.external_id and message.external_id != str(call.idempotency_key):
                raise ValueError("message response is another message")
            message = replace(message, ticket_id=input.ticket_id)
        except (ValueError, TypeError, KeyError, AttributeError) as error:
            return MutationAttempt.retry(
                failure(
                    FailureKind.PROTOCOL,
                    operation,
                    "Gorgias accepted the message but returned an invalid message: " + str(error),
                ),
                2,
            )
        await _record_accepted_write(permit, message.id)
        return MutationAttempt.branch_result(
            defs.ADDED, AddNoteOutput(message), self.client.receipt(call, result, message.id)
        )

    async def _look_up_message(
        self, call: Call, credentials: Any, ticket_id: int
    ) -> tuple[TicketMessage | None, Exchange, Receipt]:
        """Find the message an earlier attempt may have added, by the external ID it wrote."""
        operation = defs.ADD_NOTE_OPERATION_ID
        result = await self.client.exchange(call, credentials, operation, "GET", f"/tickets/{ticket_id}")
        receipt = self.client.receipt(call, result, ticket_id)
        if result.outcome != "success":
            return None, result, receipt
        try:
            record = json.loads(result.body)
            decode_ticket(record, ticket_id)
            for wire in record.get("messages") or ():
                if wire.get("external_id") == str(call.idempotency_key):
                    try:
                        message = decode_message(wire, ticket_id)
                        return message, result, self.client.receipt(call, result, message.id)
                    except (ValueError, TypeError, KeyError, AttributeError):
                        pass
        except (ValueError, TypeError, KeyError, AttributeError) as error:
            result = Exchange(
                "invalid",
                failure(FailureKind.PROTOCOL, operation, "Gorgias returned an invalid ticket: " + str(error)),
                status=result.status,
                headers=result.headers,
            )
        return None, result, receipt


def _reply_route_failure(
    permit: _WritePermit, branch: str, reason: Failure, receipt: Receipt, zero: AddNoteOutput
) -> MutationAttempt[AddNoteOutput]:
    """Select `branch` on a first send; after a possible earlier send, select uncertain."""
    if not permit.is_first_send:
        return MutationAttempt.uncertain(
            zero,
            failure(
                reason.kind,
                defs.ADD_NOTE_OPERATION_ID,
                "an earlier attempt of this Step may have sent the reply, and the ticket could not be "
                "read to route it again: " + reason.message,
            ),
            receipt,
        )
    return MutationAttempt.branch_result(branch, zero, receipt, reason)


def _reply_route(messages: list[dict[str, Any]], ticket_subject: str) -> tuple[int, str, str, str]:
    """Select the newest customer email and a known helpdesk sender address."""

    def message_order(candidate: dict[str, Any], current: dict[str, Any]) -> int:
        candidate_id = candidate.get("id", 0)
        current_id = current.get("id", 0)
        try:
            candidate_at = parse_time(candidate["created_datetime"])
            current_at = parse_time(current["created_datetime"])
        except (ValueError, KeyError, TypeError):
            return int(candidate_id > current_id) - int(candidate_id < current_id)
        if candidate_at == current_at:
            return int(candidate_id > current_id) - int(candidate_id < current_id)
        return (candidate_at > current_at) - (candidate_at < current_at)

    customer_messages = [
        message
        for message in messages
        if not message.get("from_agent")
        and message.get("channel") == "email"
        and bare_email(((message.get("source") or {}).get("from") or {}).get("address", ""))
    ]
    customer_messages = [message for message in customer_messages if _recipient_addresses(message)]
    if not customer_messages:
        raise ValueError(
            "the ticket has no email from the customer to reply to; add an internal note instead"
        )
    newest = max(customer_messages, key=cmp_to_key(message_order))
    integration_id = newest.get("integration_id") or 0
    agent_messages = (
        [
            message
            for message in messages
            if message.get("from_agent")
            and message.get("channel") == "email"
            and message.get("integration_id") == integration_id
            and bare_email(((message.get("source") or {}).get("from") or {}).get("address", ""))
        ]
        if integration_id
        else []
    )
    if agent_messages:
        support_address = max(agent_messages, key=cmp_to_key(message_order))["source"]["from"]["address"]
    else:
        recipients = _recipient_addresses(newest)
        if len(recipients) != 1:
            raise ValueError(
                "the customer's newest email reached several addresses and none is known "
                "to be the helpdesk's, so the reply is not sent"
            )
        support_address = recipients[0]
    subject = newest.get("subject") or ticket_subject
    if not subject.lower().startswith("re:"):
        subject = "Re: " + subject
    return integration_id, support_address, newest["source"]["from"]["address"], subject


def _recipient_addresses(message: dict[str, Any]) -> list[str]:
    source = message.get("source") or {}
    sender = ((source.get("from") or {}).get("address") or "").casefold()
    result: list[str] = []
    for value in (source.get("to") or []) + (source.get("cc") or []):
        address = value.get("address", "")
        if (
            bare_email(address)
            and address.casefold() != sender
            and address.casefold() not in [item.casefold() for item in result]
        ):
            result.append(address)
    return result
