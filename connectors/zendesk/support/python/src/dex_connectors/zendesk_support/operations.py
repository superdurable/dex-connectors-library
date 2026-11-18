# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Zendesk Support operation validation, invocation, and branch selection."""

import json
import re
from datetime import timedelta
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
    Credentials,
    Exchange,
    bare_email,
    decode_comment,
    decode_ticket,
    decode_ticket_envelope,
    decode_user,
    failure,
    parse_time,
)
from .models import (
    CreateTicketInput,
    CreateTicketOutput,
    GetTicketInput,
    SearchTicketsInput,
    SearchTicketsOutput,
    Ticket,
    TicketCommentInput,
    TicketDetails,
    TicketRequesterInput,
    UpdateTicketInput,
    UpdateTicketOutput,
)

_ZERO_TICKET = Ticket(status="")
_STATUSES = frozenset(("new", "open", "pending", "hold", "solved", "closed"))
_PRIORITIES = frozenset(("low", "normal", "high", "urgent"))
_TYPES = frozenset(("problem", "incident", "question", "task"))
_TAG = re.compile(r"^[a-z0-9][a-z0-9_./-]{0,79}$")


def _query_credential_failure(zero: Any, credential_failure: Failure, is_terminal: bool) -> QueryAttempt[Any]:
    """Select defect for a terminal credential failure and retry a transient one."""
    if is_terminal:
        return QueryAttempt.branch_result(defs.DEFECT, zero, Receipt(), credential_failure)
    return QueryAttempt.retry(credential_failure)


def _query_exchange(
    exchange: Exchange, receipt: Receipt, zero: Any, has_not_found: bool = False
) -> QueryAttempt[Any] | None:
    if exchange.outcome == "success":
        return None
    assert exchange.failure is not None
    if exchange.outcome == "retry":
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
    exchange: Exchange, receipt: Receipt, zero: Any, has_not_found: bool = False
) -> MutationAttempt[Any] | None:
    if exchange.outcome == "success":
        return None
    assert exchange.failure is not None
    if exchange.outcome == "retry":
        return MutationAttempt.retry(exchange.failure, exchange.retry_after)
    branch = (
        defs.NOT_FOUND
        if has_not_found and exchange.outcome == "notFound"
        else defs.INVALID_RESPONSE
        if exchange.outcome == "invalid"
        else defs.DEFECT
        if exchange.outcome == "defect"
        else defs.PROVIDER_REJECTED
    )
    return MutationAttempt.branch_result(branch, zero, receipt, exchange.failure)


def _validate_tags(tags: tuple[str, ...], field: str) -> None:
    if any(not isinstance(tag, str) or not _TAG.fullmatch(tag) for tag in tags):
        raise ValueError(f"{field} has an invalid tag")
    if len(tags) != len(set(tags)):
        raise ValueError(f"{field} lists a tag twice")


def _validate_comment(comment: TicketCommentInput, field: str) -> None:
    if not isinstance(comment.body, str) or not comment.body.strip():
        raise ValueError(f"{field}.body is required")


def _validate_enum(value: str | None, allowed: frozenset[str], field: str) -> None:
    if value and value not in allowed:
        raise ValueError(f"{field} is not a Zendesk {field}")


def _search_query(input: SearchTicketsInput) -> str:
    terms = []
    if len(input.statuses) != len(set(input.statuses)):
        raise ValueError("statuses lists a status twice")
    for status in input.statuses:
        _validate_enum(status, _STATUSES, "status")
        terms.append("status:" + status)
    if input.requester_email:
        if not bare_email(input.requester_email):
            raise ValueError("requesterEmail must be one bare email address")
        terms.append("requester:" + input.requester_email)
    _validate_tags(input.tags, "tags")
    terms.extend("tags:" + tag for tag in input.tags)
    if input.updated_since:
        instant = parse_time(input.updated_since)
        terms.append("updated>=" + instant.isoformat(timespec="seconds").replace("+00:00", "Z"))
    words = re.findall(
        r"[^\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+", input.text
    )
    if len(words) > 32:
        raise ValueError("text has more than 32 keywords")
    for word in words:
        if any(character in word for character in ':<>="*') or word[0] in "+-":
            raise ValueError("text keyword contains a search operator")
    terms.extend(words)
    if not terms:
        raise ValueError("at least one search filter is required")
    if not 0 <= input.page_size <= 100:
        raise ValueError("pageSize must be between 1 and 100, or zero for 25")
    if len(input.cursor) > 2048 or any(not ("!" <= character <= "~") for character in input.cursor):
        raise ValueError("cursor must be the printable NextCursor of a previous page")
    return " ".join(terms)


class SearchTicketsOperation:
    """Search one bounded page of tickets."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated search definition."""
        return defs.SEARCH_TICKETS_DEFINITION

    async def invoke(self, call: Call, input: SearchTicketsInput) -> QueryAttempt[SearchTicketsOutput]:
        """Fetch one export-search page and decode its tickets."""
        operation = defs.SEARCH_TICKETS_OPERATION_ID
        zero = SearchTicketsOutput()
        try:
            query = _search_query(input)
        except (ValueError, TypeError) as error:
            return QueryAttempt.branch_result(
                defs.DEFECT, zero, Receipt(), failure(FailureKind.VALIDATION, operation, str(error))
            )
        resolved = await self.client.resolve_write_credentials(call, operation)
        credentials, credential_failure, is_terminal = resolved
        if credential_failure:
            return _query_credential_failure(zero, credential_failure, is_terminal)
        assert credentials is not None
        parameters = {"query": query, "filter[type]": "ticket", "page[size]": str(input.page_size or 25)}
        if input.cursor:
            parameters["page[after]"] = input.cursor
        exchange = await self.client.exchange(
            call, credentials, operation, "GET", "/search/export", parameters
        )
        receipt = self.client.receipt(call, exchange)
        terminal = _query_exchange(exchange, receipt, zero)
        if terminal is not None:
            return terminal
        try:
            page = json.loads(exchange.body)
            if not isinstance(page, dict) or not isinstance(page.get("results"), list):
                raise ValueError("search page has no results array")
            tickets = []
            for result in page["results"]:
                if not isinstance(result, dict) or result.get("result_type", "ticket") not in (
                    None,
                    "",
                    "ticket",
                ):
                    raise ValueError("search result is not a ticket")
                ticket, _ = decode_ticket(result, self.client.agent_ticket_url)
                from dataclasses import replace

                tickets.append(replace(ticket, description="", is_description_truncated=False))
            meta = page.get("meta") or {}
            if not isinstance(meta, dict):
                raise ValueError("page meta is invalid")
            has_more = meta.get("has_more")
            if has_more is None:
                has_more = False
            elif not isinstance(has_more, bool):
                raise ValueError("search page meta is invalid")
            after_cursor = meta.get("after_cursor")
            if after_cursor is not None and not isinstance(after_cursor, str):
                raise ValueError("search page cursor is invalid")
            next_cursor = (after_cursor or "") if has_more else ""
            if not isinstance(next_cursor, str) or (has_more and not next_cursor):
                raise ValueError("search page has more results but no cursor")
            output = SearchTicketsOutput(tuple(tickets), next_cursor)
        except (ValueError, TypeError, KeyError) as error:
            return QueryAttempt.branch_result(
                defs.INVALID_RESPONSE,
                zero,
                receipt,
                failure(
                    FailureKind.PROTOCOL, operation, "Zendesk returned an invalid search page: " + str(error)
                ),
            )
        return QueryAttempt.branch_result(defs.SEARCHED, output, receipt)


class GetTicketOperation:
    """Read a ticket, sideloaded requester, and latest comments."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated ticket read definition."""
        return defs.GET_TICKET_DEFINITION

    async def invoke(self, call: Call, input: GetTicketInput) -> QueryAttempt[TicketDetails]:
        """Read the ticket and one bounded comments page."""
        operation = defs.GET_TICKET_OPERATION_ID
        zero = TicketDetails(_ZERO_TICKET)
        if input.ticket_id < 1 or not 0 <= input.latest_comment_limit <= 20:
            return QueryAttempt.branch_result(
                defs.DEFECT,
                zero,
                Receipt(),
                failure(FailureKind.VALIDATION, operation, "ticketId or latestCommentLimit is invalid"),
            )
        resolved = await self.client.resolve_write_credentials(call, operation)
        credentials, credential_failure, is_terminal = resolved
        if credential_failure:
            return _query_credential_failure(zero, credential_failure, is_terminal)
        assert credentials is not None
        path = f"/tickets/{input.ticket_id}"
        exchange = await self.client.exchange(call, credentials, operation, "GET", path, {"include": "users"})
        receipt = self.client.receipt(call, exchange, input.ticket_id)
        terminal = _query_exchange(exchange, receipt, zero, True)
        if terminal is not None:
            return terminal
        try:
            ticket, _, document = decode_ticket_envelope(exchange.body, self.client.agent_ticket_url)
            if ticket.id != input.ticket_id:
                raise ValueError("ticket response is for another ticket")
            requester = None
            users = document.get("users") or []
            if not isinstance(users, list):
                raise ValueError("users are invalid")
            for user in users:
                decoded_user = decode_user(user)
                if requester is None and decoded_user.id == ticket.requester_id and ticket.requester_id > 0:
                    requester = decoded_user
        except (ValueError, TypeError, KeyError) as error:
            return QueryAttempt.branch_result(
                defs.INVALID_RESPONSE,
                zero,
                receipt,
                failure(FailureKind.PROTOCOL, operation, "Zendesk returned an invalid ticket: " + str(error)),
            )
        limit = input.latest_comment_limit or 5
        exchange = await self.client.exchange(
            call,
            credentials,
            operation,
            "GET",
            path + "/comments",
            {"sort": "-created_at", "page[size]": str(limit)},
        )
        receipt = self.client.receipt(call, exchange, input.ticket_id)
        terminal = _query_exchange(exchange, receipt, zero, True)
        if terminal is not None:
            return terminal
        try:
            page = json.loads(exchange.body)
            if not isinstance(page, dict) or not isinstance(page.get("comments"), list):
                raise ValueError("comment page has no comments array")
            if len(page["comments"]) > limit:
                raise ValueError("comment page is larger than requested")
            comments = tuple(
                sorted(
                    (decode_comment(comment) for comment in page["comments"]),
                    key=lambda comment: (comment.created_at, comment.id),
                    reverse=True,
                )
            )
            meta = page.get("meta") or {}
            if not isinstance(meta, dict):
                raise ValueError("page meta is invalid")
            has_more = meta.get("has_more")
            if has_more is None:
                has_more = False
            elif not isinstance(has_more, bool):
                raise ValueError("comment page meta is invalid")
            output = TicketDetails(ticket, requester, comments, has_more)
        except (ValueError, TypeError, KeyError) as error:
            return QueryAttempt.branch_result(
                defs.INVALID_RESPONSE,
                zero,
                receipt,
                failure(
                    FailureKind.PROTOCOL, operation, "Zendesk returned an invalid comment page: " + str(error)
                ),
            )
        return QueryAttempt.branch_result(defs.FOUND, output, receipt)


class CreateTicketOperation:
    """Create one ticket under a stable Zendesk Idempotency-Key."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated create definition."""
        return defs.CREATE_TICKET_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreateTicketInput) -> IdempotencyKey:
        """Use the stable Dex Call ID as the provider key."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: CreateTicketInput) -> MutationAttempt[CreateTicketOutput]:
        """Post only the fields Zendesk accepts for ticket creation."""
        operation = defs.CREATE_TICKET_OPERATION_ID
        zero = CreateTicketOutput(_ZERO_TICKET)
        comment = (
            TicketCommentInput(
                body=input.comment.get("body", ""),
                is_internal_note=input.comment.get("isInternalNote", False),
            )
            if isinstance(input.comment, dict)
            else input.comment
        )
        requester = (
            TicketRequesterInput(**input.requester) if isinstance(input.requester, dict) else input.requester
        )
        try:
            if not input.subject.strip():
                raise ValueError("subject is required")
            _validate_comment(comment, "comment")
            if requester and not bare_email(requester.email):
                raise ValueError("requester.email must be one bare email address")
            _validate_enum(input.priority, _PRIORITIES, "priority")
            _validate_enum(input.type, _TYPES, "type")
            _validate_tags(input.tags, "tags")
            if min(input.group_id, input.assignee_id, input.brand_id) < 0:
                raise ValueError("optional ticket IDs cannot be negative")
            if not call.idempotency_key:
                raise ValueError("connector call has no idempotency key")
        except (ValueError, TypeError) as error:
            kind = FailureKind.LOCAL_DEFECT if "idempotency key" in str(error) else FailureKind.VALIDATION
            return MutationAttempt.branch_result(
                defs.DEFECT, zero, Receipt(), failure(kind, operation, str(error))
            )
        credentials, credential_failure, is_terminal = await self.client.resolve_write_credentials(
            call, operation
        )
        if credential_failure:
            if (call.context is not None and call.context.attempt > 1) or not is_terminal:
                # defect would hide a ticket an earlier attempt may have created;
                # createTicket declares no uncertain branch.
                return MutationAttempt.retry(credential_failure)
            return MutationAttempt.branch_result(defs.DEFECT, zero, Receipt(), credential_failure)
        assert credentials is not None
        ticket: dict[str, Any] = {
            "subject": input.subject,
            "comment": {"body": comment.body, "public": not comment.is_internal_note},
            "metadata": {"dex_idempotency_key": str(call.idempotency_key)},
        }
        if requester:
            ticket["requester"] = {"email": requester.email}
            if requester.name:
                ticket["requester"]["name"] = requester.name
        for field in ("priority", "type", "external_id"):
            value = getattr(input, field)
            if value:
                ticket[field] = value
        if input.tags:
            ticket["tags"] = list(input.tags)
        for field in ("group_id", "assignee_id", "brand_id"):
            value = getattr(input, field)
            if value:
                ticket[field] = value
        exchange = await self.client.exchange(
            call,
            credentials,
            operation,
            "POST",
            "/tickets",
            payload={"ticket": ticket},
            idempotency_key=str(call.idempotency_key),
        )
        receipt = self.client.receipt(call, exchange)
        if exchange.outcome != "success":
            assert exchange.failure is not None
            if exchange.outcome == "defect":
                return MutationAttempt.branch_result(defs.DEFECT, zero, receipt, exchange.failure)
            if _is_definite_refusal(exchange):
                return MutationAttempt.branch_result(defs.PROVIDER_REJECTED, zero, receipt, exchange.failure)
            # The keyed request is safe to repeat, and its outcome is unknown.
            return MutationAttempt.retry(exchange.failure, exchange.retry_after)
        try:
            created, _, _ = decode_ticket_envelope(exchange.body, self.client.agent_ticket_url)
        except (ValueError, TypeError, KeyError) as error:
            return MutationAttempt.retry(
                failure(
                    FailureKind.PROTOCOL,
                    operation,
                    "Zendesk returned an invalid created ticket: " + str(error),
                )
            )
        receipt = self.client.receipt(call, exchange, created.id)
        lookup = (
            exchange.headers.get("X-Idempotency-Lookup", "") if exchange.headers is not None else ""
        ).lower()
        if lookup:
            from dataclasses import replace

            receipt = replace(receipt, metadata={"idempotencyLookup": lookup})
        return MutationAttempt.branch_result(
            defs.CREATED, CreateTicketOutput(created, lookup == "hit"), receipt
        )


class UpdateTicketOperation:
    """Read, reconcile, and safely update one ticket."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated update definition."""
        return defs.UPDATE_TICKET_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpdateTicketInput) -> IdempotencyKey:
        """Use the Call ID as an audit marker for repeat detection."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: UpdateTicketInput) -> MutationAttempt[UpdateTicketOutput]:
        """Read the current ticket, audit a repeat, then write only differences."""
        operation = defs.UPDATE_TICKET_OPERATION_ID
        zero = UpdateTicketOutput(_ZERO_TICKET)
        comment = (
            TicketCommentInput(
                body=input.comment.get("body", ""),
                is_internal_note=input.comment.get("isInternalNote", False),
            )
            if isinstance(input.comment, dict)
            else input.comment
        )
        try:
            if input.ticket_id < 1:
                raise ValueError("ticketId must be a positive Zendesk ticket ID")
            _validate_enum(input.status, _STATUSES, "status")
            _validate_enum(input.priority, _PRIORITIES, "priority")
            if input.assignee_id < 0 or input.group_id < 0:
                raise ValueError("assigneeId and groupId cannot be negative")
            _validate_tags(input.add_tags, "addTags")
            _validate_tags(input.remove_tags, "removeTags")
            if set(input.add_tags) & set(input.remove_tags):
                raise ValueError("a tag is in both addTags and removeTags")
            if comment:
                _validate_comment(comment, "comment")
            if input.expected_updated_at:
                parse_time(input.expected_updated_at)
            if not any(
                (
                    input.status,
                    input.priority,
                    input.assignee_id,
                    input.group_id,
                    input.add_tags,
                    input.remove_tags,
                    input.comment,
                )
            ):
                raise ValueError("at least one ticket change is required")
            if not call.idempotency_key:
                raise ValueError("connector call has no idempotency key")
        except (ValueError, TypeError) as error:
            kind = FailureKind.LOCAL_DEFECT if "idempotency key" in str(error) else FailureKind.VALIDATION
            return MutationAttempt.branch_result(
                defs.DEFECT, zero, Receipt(), failure(kind, operation, str(error))
            )
        try:
            fence = Fence(call, timedelta(seconds=30), max_sends=call.max_sends)
            decision = await fence.decide()
        except ValueError:
            return MutationAttempt.branch_result(
                defs.DEFECT,
                zero,
                Receipt(),
                failure(FailureKind.LOCAL_DEFECT, operation, "single-dispatch fence could not start"),
            )
        if decision.kind == DecisionKind.FAIL_CLOSED:
            # FailClosed sends nothing, so a later attempt decides again from a readable checkpoint.
            return MutationAttempt.retry(
                failure(
                    FailureKind.AVAILABILITY,
                    operation,
                    "the single-dispatch checkpoint could not be read; nothing was sent in this attempt",
                )
            )
        permit = _TicketWritePermit(fence, decision)
        credentials, credential_failure, is_terminal = await self.client.resolve_write_credentials(
            call, operation
        )
        if credential_failure:
            return await permit.credential_failure_attempt(credential_failure, is_terminal)
        assert credentials is not None
        if permit.has_accepted_write:
            # Zendesk accepted an earlier attempt's PUT, so this attempt only reads it back.
            return await _AcceptedTicketWrite(
                self.client,
                call,
                credentials,
                input,
                comment,
                can_retry_transient_read=True,
                is_from_earlier_attempt=True,
            ).read_back()
        path = f"/tickets/{input.ticket_id}"
        exchange = await self.client.exchange(call, credentials, operation, "GET", path)
        receipt = self.client.receipt(call, exchange, input.ticket_id)
        terminal = await permit.read_attempt(exchange, receipt, zero)
        if terminal is not None:
            return terminal
        try:
            current, stamp, _ = decode_ticket_envelope(exchange.body, self.client.agent_ticket_url)
            if current.id != input.ticket_id:
                raise ValueError("ticket response is for another ticket")
        except (ValueError, TypeError, KeyError) as error:
            invalid = failure(
                FailureKind.PROTOCOL, operation, "Zendesk returned an invalid ticket: " + str(error)
            )
            if not permit.is_first_send:
                return permit.unconfirmed_read(zero, invalid, receipt)
            return MutationAttempt.branch_result(defs.INVALID_RESPONSE, zero, receipt, invalid)
        current_output = UpdateTicketOutput(current)
        if comment or input.expected_updated_at:
            exchange = await self.client.exchange(
                call, credentials, operation, "GET", path + "/audits", {"sort_order": "desc"}
            )
            receipt = self.client.receipt(call, exchange, input.ticket_id)
            terminal = await permit.read_attempt(exchange, receipt, current_output)
            if terminal is not None:
                return terminal
            try:
                if _has_audit_with_idempotency_key(exchange.body, str(call.idempotency_key)):
                    return MutationAttempt.branch_result(
                        defs.UPDATED, UpdateTicketOutput(current, True), receipt
                    )
            except (ValueError, TypeError, AttributeError) as error:
                invalid = failure(
                    FailureKind.PROTOCOL,
                    operation,
                    "Zendesk returned an invalid audit page: " + str(error),
                )
                if not permit.is_first_send:
                    return permit.unconfirmed_read(current_output, invalid, receipt)
                return MutationAttempt.branch_result(defs.INVALID_RESPONSE, current_output, receipt, invalid)
        if input.expected_updated_at and parse_time(input.expected_updated_at) != current.updated_at:
            conflict = failure(
                FailureKind.CONFLICT,
                operation,
                "ticket was updated at "
                + current.updated_at.isoformat(timespec="seconds").replace("+00:00", "Z")
                + ", after expectedUpdatedAt",
            )
            if not permit.is_first_send:
                # An earlier send of this Step may have moved updated_at without an audit on the latest page.
                return permit.unconfirmed_read(current_output, conflict, receipt)
            return MutationAttempt.branch_result(defs.CONFLICT, current_output, receipt, conflict)
        change = _plan_ticket_change(input, comment, current, stamp, str(call.idempotency_key))
        if len(change) == 3:
            return MutationAttempt.branch_result(defs.UPDATED, UpdateTicketOutput(current, True), receipt)
        if permit.is_at_send_cap:
            return MutationAttempt.uncertain(
                current_output,
                failure(
                    FailureKind.TRANSPORT,
                    operation,
                    "earlier attempts may have written the change and the send cap is reached, "
                    "and the latest audit page shows no change by this Step",
                ),
                receipt,
            )
        observed_statuses: list[int] = []
        try:
            exchange = await fence.dispatch(
                lambda _deadline: self.client.exchange(
                    call,
                    credentials,
                    operation,
                    "PUT",
                    path,
                    payload={"ticket": change},
                    fenced=True,
                    on_response_status=observed_statuses.append,
                )
            )
        except (CheckpointError, InsufficientAttemptTimeError, DispatchNotAllowedError):
            # Dispatch refused before sending, so nothing reached Zendesk in this attempt.
            return MutationAttempt.retry(
                failure(
                    FailureKind.AVAILABILITY,
                    operation,
                    "Zendesk write was not sent before the attempt deadline or checkpoint",
                )
            )
        except TimeoutError:
            # The write window ended after the claim, so the PUT may have applied; 2xx headers prove it did.
            observed_status = observed_statuses[0] if observed_statuses else 0
            exchange = Exchange(
                "retry",
                failure(
                    FailureKind.TRANSPORT,
                    operation,
                    "Zendesk answered, but its response did not arrive within the write window"
                    if observed_status
                    else "Zendesk request failed before a response arrived",
                ),
                status=observed_status,
            )
        receipt = self.client.receipt(call, exchange, input.ticket_id)
        if not 200 <= exchange.status < 300:
            return await permit.write_attempt(exchange, receipt, current_output)
        updated = (
            _decode_ticket_for(exchange.body, self.client.agent_ticket_url, input.ticket_id)
            if exchange.outcome == "success"
            else None
        )
        if updated is not None:
            return MutationAttempt.branch_result(defs.UPDATED, UpdateTicketOutput(updated), receipt)
        # Zendesk accepted the PUT, so it is never resent; the saved ticket ID makes later attempts read back.
        try:
            await fence.record_provider_id(str(input.ticket_id))
            is_ticket_id_recorded = True
        except Exception:
            is_ticket_id_recorded = False
        return await _AcceptedTicketWrite(
            self.client, call, credentials, input, comment, can_retry_transient_read=is_ticket_id_recorded
        ).read_back()


# Only these statuses prove Zendesk applied nothing; any other non-2xx answer may have been applied.
_DEFINITE_REFUSAL_STATUSES = frozenset({400, 401, 403, 404, 411, 413, 414, 415, 422})


class _TicketWritePermit:
    """Map updateTicket reads and its fenced PUT under the fence decision's send-cap facts."""

    def __init__(self, fence: Fence, decision: Decision) -> None:
        self.fence = fence
        # An earlier attempt saved the ticket ID after Zendesk accepted its PUT.
        self.has_accepted_write = decision.marker is not None and bool(decision.marker.provider_id)
        # No earlier attempt of this call may have sent; SEND_ALLOWED after canResend can still carry sends.
        self.is_first_send = (
            decision.kind == DecisionKind.SEND_ALLOWED
            and decision.prior_send_upper_bound == 0
            and not self.has_accepted_write
        )
        # Earlier sends reached the cap, so this attempt only reads back.
        self.is_at_send_cap = decision.kind == DecisionKind.READ_BACK_REQUIRED

    async def credential_failure_attempt(self, problem: Failure, is_terminal: bool) -> MutationAttempt[Any]:
        """Map unusable credentials before any request of this attempt."""
        zero = UpdateTicketOutput(_ZERO_TICKET)
        if not self.is_first_send:
            # notSent would hide the earlier possible send, so nothing is recorded.
            if is_terminal:
                return MutationAttempt.uncertain(
                    zero,
                    failure(
                        problem.kind,
                        defs.UPDATE_TICKET_OPERATION_ID,
                        "an earlier attempt may have written the change, and the connection credentials "
                        "are unusable; reconnect the connection",
                    ),
                    Receipt(),
                )
            return MutationAttempt.retry(problem)
        try:
            await self.fence.record_not_sent()
        except Exception:
            return MutationAttempt.retry(
                failure(
                    FailureKind.AVAILABILITY,
                    defs.UPDATE_TICKET_OPERATION_ID,
                    "Dex did not record the not-sent checkpoint",
                )
            )
        if is_terminal:
            return MutationAttempt.branch_result(defs.DEFECT, zero, Receipt(), problem)
        return MutationAttempt.retry(problem)

    async def retry_before_dispatch(self, problem: Failure, retry_after: float) -> MutationAttempt[Any]:
        """Record notSent only on a first send; otherwise the claim stays counted."""
        if self.is_first_send:
            try:
                await self.fence.record_not_sent()
            except Exception:
                return MutationAttempt.retry(
                    failure(
                        FailureKind.AVAILABILITY,
                        defs.UPDATE_TICKET_OPERATION_ID,
                        "Dex did not record the not-sent checkpoint",
                    ),
                    retry_after,
                )
        return MutationAttempt.retry(problem, retry_after)

    def unconfirmed_read(self, value: Any, problem: Failure, receipt: Receipt) -> MutationAttempt[Any]:
        """Retry an inconclusive read after a possible earlier send, or select uncertain at the cap."""
        if self.is_at_send_cap:
            return MutationAttempt.uncertain(value, problem, receipt)
        return MutationAttempt.retry(problem)

    async def read_attempt(
        self, exchange: Exchange, receipt: Receipt, value: Any
    ) -> MutationAttempt[Any] | None:
        """Map a read before the write; a definite answer after a possible send is uncertain."""
        if exchange.outcome == "success":
            return None
        assert exchange.failure is not None
        if exchange.outcome in ("retry", "notSent"):
            return await self.retry_before_dispatch(exchange.failure, exchange.retry_after)
        if not self.is_first_send:
            if exchange.outcome in ("invalid", "defect"):
                return self.unconfirmed_read(value, exchange.failure, receipt)
            return MutationAttempt.uncertain(value, exchange.failure, receipt)
        return _mutation_exchange(exchange, receipt, value, True)

    async def write_attempt(self, exchange: Exchange, receipt: Receipt, value: Any) -> MutationAttempt[Any]:
        """Map a PUT that Zendesk did not answer with 2xx.

        Only a definite refusal of a first send selects a refusal branch.
        """
        assert exchange.failure is not None
        if exchange.outcome == "notSent" or exchange.status in (429, 503):
            try:
                await self.fence.record_can_resend()
            except Exception:
                # A lost checkpoint leaves the claim, so the next attempt counts this send toward the cap.
                pass
            return MutationAttempt.retry(exchange.failure, exchange.retry_after)
        if exchange.outcome == "defect" and not self.is_first_send:
            # The PUT could not be built, and an earlier send may have applied.
            return MutationAttempt.uncertain(value, exchange.failure, receipt)
        if exchange.outcome == "defect" or _is_definite_refusal(exchange):
            if not self.is_first_send:
                return MutationAttempt.uncertain(value, exchange.failure, receipt)
            try:
                await self.fence.record_can_resend()
            except Exception:
                return MutationAttempt.retry(
                    failure(
                        FailureKind.AVAILABILITY,
                        defs.UPDATE_TICKET_OPERATION_ID,
                        "Dex did not record the refusal checkpoint",
                    )
                )
            refusal = _mutation_exchange(exchange, receipt, value, True)
            assert refusal is not None
            return refusal
        # A 3xx, 408, 409, 410, other 5xx, unlisted status, lost answer, oversized error body, or error body
        # carrying a ticket may follow an applied write.
        return _retry_unknown_write_outcome(exchange.failure, exchange.retry_after)


def _retry_unknown_write_outcome(problem: Failure, retry_after: float) -> MutationAttempt[Any]:
    """Retry an unknown write outcome, also after the last allowed send.

    The claim stays the last marker, so the next attempt reads the audit back; it resends only
    below the cap and selects uncertain at the cap.
    """
    return MutationAttempt.retry(problem, retry_after)


class _AcceptedTicketWrite:
    """Confirm a PUT that Zendesk answered with an unusable 2xx, or that an earlier attempt saved.

    It never sends.
    """

    def __init__(
        self,
        client: Client,
        call: Call,
        credentials: Credentials,
        input: UpdateTicketInput,
        comment: TicketCommentInput | None,
        *,
        can_retry_transient_read: bool,
        is_from_earlier_attempt: bool = False,
    ) -> None:
        self.client = client
        self.call = call
        self.credentials = credentials
        self.input = input
        self.comment = comment
        # The ticket ID is checkpointed, so a Retry can only read back again.
        self.can_retry_transient_read = can_retry_transient_read
        # A confirmed change is reported as already applied.
        self.is_from_earlier_attempt = is_from_earlier_attempt

    async def read_back(self) -> MutationAttempt[UpdateTicketOutput]:
        """Confirm the change by this Step's audit, or by no pending field change without a comment."""
        operation = defs.UPDATE_TICKET_OPERATION_ID
        zero = UpdateTicketOutput(_ZERO_TICKET)
        key = str(self.call.idempotency_key)
        path = f"/tickets/{self.input.ticket_id}"
        exchange = await self.client.exchange(self.call, self.credentials, operation, "GET", path)
        receipt = self.client.receipt(self.call, exchange, self.input.ticket_id)
        terminal = self._read_attempt(exchange, receipt, zero)
        if terminal is not None:
            return terminal
        try:
            current, stamp, _ = decode_ticket_envelope(exchange.body, self.client.agent_ticket_url)
            if current.id != self.input.ticket_id:
                raise ValueError("ticket response is for another ticket")
        except (ValueError, TypeError, KeyError) as error:
            return self._unconfirmed(
                zero, FailureKind.PROTOCOL, "Zendesk returned an invalid ticket: " + str(error), receipt
            )
        current_output = UpdateTicketOutput(current)
        if self.comment or self.input.expected_updated_at:
            exchange = await self.client.exchange(
                self.call, self.credentials, operation, "GET", path + "/audits", {"sort_order": "desc"}
            )
            receipt = self.client.receipt(self.call, exchange, self.input.ticket_id)
            terminal = self._read_attempt(exchange, receipt, current_output)
            if terminal is not None:
                return terminal
            try:
                if _has_audit_with_idempotency_key(exchange.body, key):
                    return self._confirmed(current, receipt)
            except (ValueError, TypeError, AttributeError) as error:
                return self._unconfirmed(
                    current_output,
                    FailureKind.PROTOCOL,
                    "Zendesk returned an invalid audit page: " + str(error),
                    receipt,
                )
        if not self.comment and len(_plan_ticket_change(self.input, None, current, stamp, key)) == 3:
            return self._confirmed(current, receipt)
        return self._unconfirmed(
            current_output,
            FailureKind.TRANSPORT,
            "the ticket and its latest audit page show no change by this Step",
            receipt,
        )

    def _read_attempt(
        self, exchange: Exchange, receipt: Receipt, value: UpdateTicketOutput
    ) -> MutationAttempt[UpdateTicketOutput] | None:
        """Retry a transient read only when that cannot resend; any other failed read is uncertain."""
        if exchange.outcome == "success":
            return None
        assert exchange.failure is not None
        if exchange.outcome in ("retry", "notSent") and self.can_retry_transient_read:
            return MutationAttempt.retry(exchange.failure, exchange.retry_after)
        return self._unconfirmed(value, exchange.failure.kind, exchange.failure.message, receipt)

    def _confirmed(self, ticket: Ticket, receipt: Receipt) -> MutationAttempt[UpdateTicketOutput]:
        return MutationAttempt.branch_result(
            defs.UPDATED, UpdateTicketOutput(ticket, self.is_from_earlier_attempt), receipt
        )

    def _unconfirmed(
        self, value: UpdateTicketOutput, kind: FailureKind, reason: str, receipt: Receipt
    ) -> MutationAttempt[UpdateTicketOutput]:
        return MutationAttempt.uncertain(
            value,
            failure(
                kind,
                defs.UPDATE_TICKET_OPERATION_ID,
                "Zendesk accepted an update by this Step that could not be confirmed: " + reason,
            ),
            receipt,
        )


def _is_definite_refusal(exchange: Exchange) -> bool:
    """Report a refusal status whose readable body carries no ticket, proving Zendesk applied nothing."""
    return (
        exchange.outcome in ("rejected", "notFound")
        and exchange.status in _DEFINITE_REFUSAL_STATUSES
        and not exchange.has_oversized_body
        and not exchange.has_ticket_identity
    )


def _decode_ticket_for(body: bytes, agent_ticket_url: str, ticket_id: int) -> Ticket | None:
    """Return the ticket of a response for ticket_id, or None when it is unusable."""
    try:
        ticket, _, _ = decode_ticket_envelope(body, agent_ticket_url)
    except (ValueError, TypeError, KeyError):
        return None
    return ticket if ticket.id == ticket_id else None


def _has_audit_with_idempotency_key(body: bytes, idempotency_key: str) -> bool:
    """Scan one page of the newest audits for this call's metadata entry."""
    page = json.loads(body)
    if not isinstance(page, dict) or not isinstance(page.get("audits"), list):
        raise ValueError("audit page has no audits array")
    for audit in page["audits"]:
        if audit is None:
            continue
        if not isinstance(audit, dict):
            raise ValueError("audit is invalid")
        metadata = audit.get("metadata") or {}
        custom = metadata.get("custom") or {}
        if custom.get("dex_idempotency_key") == idempotency_key:
            return True
    return False


def _plan_ticket_change(
    input: UpdateTicketInput,
    comment: TicketCommentInput | None,
    current: Ticket,
    stamp: str,
    idempotency_key: str,
) -> dict[str, Any]:
    """Return the safe update with only fields that differ; three entries mean nothing to write."""
    change: dict[str, Any] = {
        "metadata": {"dex_idempotency_key": idempotency_key},
        "safe_update": True,
        "updated_stamp": stamp,
    }
    if input.status and input.status != current.status:
        change["status"] = input.status
    if input.priority and input.priority != current.priority:
        change["priority"] = input.priority
    if input.assignee_id and input.assignee_id != current.assignee_id:
        change["assignee_id"] = input.assignee_id
    if input.group_id and input.group_id != current.group_id:
        change["group_id"] = input.group_id
    tags = [tag for tag in current.tags if tag not in input.remove_tags]
    for tag in input.add_tags:
        if tag not in tags:
            tags.append(tag)
    if tags != list(current.tags):
        change["tags"] = tags
    if comment:
        change["comment"] = {"body": comment.body, "public": not comment.is_internal_note}
    return change
