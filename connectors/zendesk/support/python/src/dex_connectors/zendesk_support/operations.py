# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Zendesk Support operation validation, invocation, and branch selection."""

import json
import re
from typing import Any

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

from ._generated import definitions as defs
from .client import (
    Client,
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
        credentials, credential_failure = await self.client.resolve_credentials(call, operation)
        if credential_failure:
            return QueryAttempt.branch_result(defs.DEFECT, zero, Receipt(), credential_failure)
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
        credentials, credential_failure = await self.client.resolve_credentials(call, operation)
        if credential_failure:
            return QueryAttempt.branch_result(defs.DEFECT, zero, Receipt(), credential_failure)
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
        credentials, credential_failure = await self.client.resolve_credentials(call, operation)
        if credential_failure:
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
        terminal = _mutation_exchange(exchange, receipt, zero)
        if terminal is not None:
            return terminal
        try:
            created, _, _ = decode_ticket_envelope(exchange.body, self.client.agent_ticket_url)
        except (ValueError, TypeError, KeyError) as error:
            return MutationAttempt.branch_result(
                defs.INVALID_RESPONSE,
                zero,
                receipt,
                failure(
                    FailureKind.PROTOCOL,
                    operation,
                    "Zendesk returned an invalid created ticket: " + str(error),
                ),
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
        credentials, credential_failure = await self.client.resolve_credentials(call, operation)
        if credential_failure:
            return MutationAttempt.branch_result(defs.DEFECT, zero, Receipt(), credential_failure)
        assert credentials is not None
        path = f"/tickets/{input.ticket_id}"
        exchange = await self.client.exchange(call, credentials, operation, "GET", path)
        receipt = self.client.receipt(call, exchange, input.ticket_id)
        terminal = _mutation_exchange(exchange, receipt, zero, True)
        if terminal is not None:
            return terminal
        try:
            current, stamp, _ = decode_ticket_envelope(exchange.body, self.client.agent_ticket_url)
            if current.id != input.ticket_id:
                raise ValueError("ticket response is for another ticket")
        except (ValueError, TypeError, KeyError) as error:
            return MutationAttempt.branch_result(
                defs.INVALID_RESPONSE,
                zero,
                receipt,
                failure(FailureKind.PROTOCOL, operation, "Zendesk returned an invalid ticket: " + str(error)),
            )
        current_output = UpdateTicketOutput(current)
        if comment or input.expected_updated_at:
            exchange = await self.client.exchange(
                call, credentials, operation, "GET", path + "/audits", {"sort_order": "desc"}
            )
            receipt = self.client.receipt(call, exchange, input.ticket_id)
            terminal = _mutation_exchange(exchange, receipt, current_output, True)
            if terminal is not None:
                return terminal
            try:
                page = json.loads(exchange.body)
                if not isinstance(page, dict) or not isinstance(page.get("audits"), list):
                    raise ValueError("audit page has no audits array")
                for audit in page["audits"]:
                    if audit is None:
                        continue
                    if not isinstance(audit, dict):
                        raise ValueError("audit is invalid")
                    metadata = audit.get("metadata") or {}
                    custom = metadata.get("custom") or {}
                    if custom.get("dex_idempotency_key") == str(call.idempotency_key):
                        return MutationAttempt.branch_result(
                            defs.UPDATED, UpdateTicketOutput(current, True), receipt
                        )
            except (ValueError, TypeError, AttributeError) as error:
                return MutationAttempt.branch_result(
                    defs.INVALID_RESPONSE,
                    current_output,
                    receipt,
                    failure(
                        FailureKind.PROTOCOL,
                        operation,
                        "Zendesk returned an invalid audit page: " + str(error),
                    ),
                )
        if input.expected_updated_at and parse_time(input.expected_updated_at) != current.updated_at:
            return MutationAttempt.branch_result(
                defs.CONFLICT,
                current_output,
                receipt,
                failure(
                    FailureKind.CONFLICT,
                    operation,
                    "ticket was updated at "
                    + current.updated_at.isoformat(timespec="seconds").replace("+00:00", "Z")
                    + ", after expectedUpdatedAt",
                ),
            )
        change: dict[str, Any] = {
            "metadata": {"dex_idempotency_key": str(call.idempotency_key)},
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
        if len(change) == 3:
            return MutationAttempt.branch_result(defs.UPDATED, UpdateTicketOutput(current, True), receipt)
        exchange = await self.client.exchange(
            call, credentials, operation, "PUT", path, payload={"ticket": change}
        )
        receipt = self.client.receipt(call, exchange, input.ticket_id)
        terminal = _mutation_exchange(exchange, receipt, current_output, True)
        if terminal is not None:
            return terminal
        try:
            updated, _, _ = decode_ticket_envelope(exchange.body, self.client.agent_ticket_url)
            if updated.id != input.ticket_id:
                raise ValueError("ticket response is for another ticket")
        except (ValueError, TypeError, KeyError) as error:
            return MutationAttempt.branch_result(
                defs.INVALID_RESPONSE,
                current_output,
                receipt,
                failure(
                    FailureKind.PROTOCOL,
                    operation,
                    "Zendesk returned an invalid updated ticket: " + str(error),
                ),
            )
        return MutationAttempt.branch_result(defs.UPDATED, UpdateTicketOutput(updated), receipt)
