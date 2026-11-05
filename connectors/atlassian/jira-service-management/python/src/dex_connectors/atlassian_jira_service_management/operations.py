# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Jira Service Management operation invocations and provider response conversion."""

import ipaddress
import json
import re
from dataclasses import dataclass, replace
from dataclasses import field as dataclass_field
from datetime import UTC, datetime, timedelta
from email.utils import parseaddr
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
from .client import Classification, Client, ProviderResponse, failure, path_escape
from .models import (
    AccountReference,
    AddCommentInput,
    AddCommentOutput,
    CreateTicketInput,
    CreateTicketOutput,
    Customer,
    CustomerMatches,
    FindCustomerByEmailInput,
    GetTicketInput,
    PriorityReference,
    RequestStatus,
    SearchTicketsInput,
    SearchTicketsOutput,
    SLACycle,
    Ticket,
    TicketComment,
    TicketDetails,
    TicketSLA,
    TicketStatus,
    TransitionReference,
    TransitionTicketInput,
    TransitionTicketOutput,
    UpdateTicketInput,
    UpdateTicketOutput,
)

_NUMERIC_ID = re.compile(r"[1-9][0-9]{0,17}\Z")
_ISSUE_KEY = re.compile(r"[A-Za-z][A-Za-z0-9_]*-[1-9][0-9]{0,17}\Z")
_PROJECT_KEY = re.compile(r"[A-Z][A-Z0-9_]{0,254}\Z")
_ACCOUNT_ID = re.compile(r"[A-Za-z0-9:_-]{1,128}\Z")
_FIELD_ID = re.compile(r"[A-Za-z][A-Za-z0-9_.-]{0,63}\Z")
_TICKET_FIELDS = "summary,status,project,assignee,reporter,priority,labels,created,updated"
_JIRA_TIME = re.compile(
    r"([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})"
    r"(?:[.,]([0-9]+))?(Z|[+-][0-9]{2}:?[0-9]{2})\Z"
)


def _string(value: object) -> str:
    if not isinstance(value, str):
        raise ValueError("field must be a string")
    return value


def _integer(value: object) -> int:
    if type(value) is not int:
        raise ValueError("field must be an integer")
    return value


def _boolean(value: object) -> bool:
    if type(value) is not bool:
        raise ValueError("field must be a boolean")
    return value


def _object(value: object) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError("field must be an object")
    return value


def _optional_object(value: object) -> dict[str, Any]:
    return {} if value is None else _object(value)


def _array(value: object) -> list[Any]:
    if not isinstance(value, list):
        raise ValueError("field must be an array")
    return value


def _optional_array(value: object) -> list[Any]:
    return [] if value is None else _array(value)


def _str_field(value: dict[str, Any], key: str) -> str:
    current = value.get(key)
    return "" if current is None else _string(current)


def _bool_field(value: dict[str, Any], key: str) -> bool:
    current = value.get(key)
    return False if current is None else _boolean(current)


def _int_field(value: dict[str, Any], key: str) -> int:
    current = value.get(key)
    return 0 if current is None else _integer(current)


def _json_object(body: bytes) -> dict[str, Any]:
    return _object(json.loads(body))


def _issue_id(value: str) -> str:
    result = value.strip()
    if not _NUMERIC_ID.fullmatch(result) and not _ISSUE_KEY.fullmatch(result):
        raise ValueError("issueIdOrKey must be a request key such as ITH-12 or a numeric issue ID")
    return result


def _numeric_id(value: str, name: str) -> str:
    result = value.strip()
    if not _NUMERIC_ID.fullmatch(result):
        raise ValueError(f"{name} must be a numeric ID such as 10")
    return result


def _email_address(value: str) -> str:
    email = value.strip()
    message = "email must be one bare address such as jane@example.com"
    if len(email.encode()) > 254 or email.count("@") != 1:
        raise ValueError(message)
    local, domain = email.split("@")
    if not local or not domain or any(part in ("", ".") for part in local.split(".")):
        raise ValueError(message)
    if domain.startswith("[") and domain.endswith("]"):
        try:
            ipaddress.ip_address(domain[1:-1])
        except ValueError:
            raise ValueError(message) from None
        if any(character.isspace() or ord(character) < 32 for character in local):
            raise ValueError(message)
        return email
    if any(part == "" for part in domain.split(".")):
        raise ValueError(message)
    display, parsed = parseaddr(email)
    if display or parsed != email:
        raise ValueError(message)
    return email


def _single_line(value: str, name: str, limit: int) -> str:
    result = value.strip()
    if not result:
        raise ValueError(f"{name} is required")
    if len(result) > limit:
        raise ValueError(f"{name} cannot exceed {limit} characters")
    if any(ord(character) < 32 or ord(character) == 127 for character in result):
        raise ValueError(f"{name} must be one line without control characters")
    return result


def _plain_text(value: str, name: str, required: bool) -> None:
    if required and not value.strip():
        raise ValueError(f"{name} is required")
    if len(value) > 32767:
        raise ValueError(f"{name} cannot exceed 32767 characters")
    if any(character == "\0" or (ord(character) < 32 and character not in "\n\r\t") for character in value):
        raise ValueError(f"{name} cannot contain control characters")


def _labels(values: tuple[str, ...], name: str) -> tuple[str, ...]:
    if len(values) > 50:
        raise ValueError(f"{name} accepts at most 50 labels")
    for label in values:
        if (
            not label
            or len(label) > 255
            or any(character.isspace() or ord(character) < 32 for character in label)
        ):
            raise ValueError(f"each {name} label must be 1 to 255 characters without whitespace")
    return values


def _jira_time(value: str) -> datetime:
    if not value:
        return datetime.min.replace(tzinfo=UTC)
    match = _JIRA_TIME.fullmatch(value)
    if match is None:
        raise ValueError("timestamp is not a Jira date-time")
    year, month, day, hour, minute, second = (int(part) for part in match.groups()[:6])
    fraction = match.group(7) or ""
    offset = match.group(8)
    try:
        parsed = datetime(year, month, day, hour, minute, second, int((fraction + "000000")[:6]))
        if offset != "Z":
            digits = offset[1:].replace(":", "")
            offset_hours, offset_minutes = int(digits[:2]), int(digits[2:])
            if offset_minutes >= 60:
                raise ValueError("invalid offset")
            delta = timedelta(hours=offset_hours, minutes=offset_minutes)
            parsed = parsed - delta if offset[0] == "+" else parsed + delta
    except (ValueError, OverflowError):
        raise ValueError("timestamp is not a Jira date-time") from None
    return parsed.replace(tzinfo=UTC)


def _service_date(value: object) -> datetime | None:
    if value is None:
        return None
    date = _object(value)
    millis = date.get("epochMillis")
    if millis is not None:
        return datetime.fromtimestamp(_integer(millis) / 1000, UTC)
    iso = _str_field(date, "iso8601")
    return _jira_time(iso) if iso else None


def _status(value: object) -> TicketStatus:
    if value is None:
        return TicketStatus()
    item = _object(value)
    category = item.get("statusCategory")
    return TicketStatus(
        _str_field(item, "id"),
        _str_field(item, "name"),
        _str_field(_object(category), "key") if category is not None else None,
    )


def _account(value: object) -> AccountReference | None:
    if value is None:
        return None
    item = _object(value)
    account_id = _str_field(item, "accountId")
    return AccountReference(account_id, _str_field(item, "displayName")) if account_id else None


def _priority(value: object) -> PriorityReference | None:
    if value is None:
        return None
    item = _object(value)
    return PriorityReference(_str_field(item, "id"), _str_field(item, "name"))


def _ticket(value: object, description: bool = False) -> Ticket:
    resource = _object(value)
    issue_id = _str_field(resource, "id")
    key = _str_field(resource, "key")
    if not _NUMERIC_ID.fullmatch(issue_id) or not _ISSUE_KEY.fullmatch(key):
        raise ValueError("issue ID or key is invalid")
    fields = _optional_object(resource.get("fields"))
    project = fields.get("project")
    labels = fields.get("labels")
    if labels is not None:
        labels = tuple(_string(item) for item in _array(labels))
    content = ""
    truncated = False
    if description:
        raw = fields.get("description")
        if isinstance(raw, str):
            content = raw
        elif raw is not None:
            content = _document_text(_object(raw))
        truncated = len(content) > 32767
        content = content[:32767]
    return Ticket(
        issue_id,
        key,
        _str_field(_object(project), "key") if project is not None else "",
        _str_field(fields, "summary"),
        _status(fields.get("status")),
        _priority(fields.get("priority")),
        _account(fields.get("assignee")),
        _account(fields.get("reporter")),
        labels or (),
        content,
        truncated,
        _jira_time(_str_field(fields, "created")),
        _jira_time(_str_field(fields, "updated")),
    )


def _document_text(value: dict[str, Any]) -> str:
    pieces: list[str] = []

    def visit(node: dict[str, Any], depth: int) -> None:
        if depth > 48:
            return
        kind = _str_field(node, "type")
        if kind == "text":
            pieces.append(_str_field(node, "text"))
        elif kind == "hardBreak":
            pieces.append("\n")
        elif kind in ("mention", "emoji", "status"):
            pieces.append(_str_field(_optional_object(node.get("attrs")), "text"))
        elif kind in ("inlineCard", "blockCard", "embedCard"):
            pieces.append(_str_field(_optional_object(node.get("attrs")), "url"))
        elif kind == "rule":
            pieces.append("---")
        else:
            for child in _optional_array(node.get("content")):
                visit(_object(child), depth + 1)

    visit(value, 0)
    return "".join(pieces).rstrip("\n")


def _page(body: bytes) -> tuple[list[Any], bool]:
    page = _json_object(body)
    if page.get("values") is None:
        raise ValueError("page has no values list")
    return _array(page["values"]), _bool_field(page, "isLastPage")


def _query_failure(
    classification: Classification, output: Any, receipt: Receipt, *, not_found: str = defs.NOT_FOUND
) -> QueryAttempt[Any]:
    assert classification.failure is not None
    if classification.outcome == "retry":
        return QueryAttempt.retry(classification.failure, classification.retry_after)
    branch = {"notFound": not_found, "defect": defs.DEFECT, "invalidResponse": defs.INVALID_RESPONSE}.get(
        classification.outcome, defs.PROVIDER_REJECTED
    )
    return QueryAttempt.branch_result(branch, output, receipt, classification.failure)


def _mutation_failure(
    classification: Classification, output: Any, receipt: Receipt, *, not_found: str = defs.NOT_FOUND
) -> MutationAttempt[Any]:
    assert classification.failure is not None
    if classification.outcome == "retry":
        return MutationAttempt.retry(classification.failure, classification.retry_after)
    branch = {"notFound": not_found, "defect": defs.DEFECT, "invalidResponse": defs.INVALID_RESPONSE}.get(
        classification.outcome, defs.PROVIDER_REJECTED
    )
    return MutationAttempt.branch_result(branch, output, receipt, classification.failure)


def _jql_quote(value: str) -> str:
    return '"' + value.replace("\\", "\\\\").replace('"', '\\"') + '"'


def _jql(input: SearchTicketsInput) -> str:
    project = input.project_key.strip()
    if not _PROJECT_KEY.fullmatch(project):
        raise ValueError("projectKey must be the service desk's uppercase project key such as ITH")
    clauses = ["project = " + _jql_quote(project)]
    if input.status_categories:
        categories = {"new": "2", "indeterminate": "4", "done": "3"}
        if any(category not in categories for category in input.status_categories):
            raise ValueError("statusCategories accepts new, indeterminate, and done")
        clauses.append(
            "statusCategory in ("
            + ", ".join(categories[category] for category in input.status_categories)
            + ")"
        )
    if any(not _ACCOUNT_ID.fullmatch(value) for value in input.reporter_account_ids):
        raise ValueError("reporterAccountIds must be Atlassian account IDs")
    for field, values in (
        ("status", input.status_names),
        ("labels", input.labels),
        ("reporter", input.reporter_account_ids),
    ):
        if not values:
            continue
        if len(values) > 50:
            raise ValueError(f"search accepts at most 50 {field} values")
        quoted = []
        for value in values:
            trimmed = value.strip()
            if not trimmed:
                raise ValueError(f"search {field} values cannot be blank")
            if len(trimmed) > 1024 or any(
                ord(character) < 32 or ord(character) == 127 for character in trimmed
            ):
                raise ValueError(f"search {field} value must be valid text")
            quoted.append(_jql_quote(trimmed))
        clauses.append(field + " in (" + ", ".join(quoted) + ")")
    phrase = input.summary_phrase.strip()
    if phrase:
        text_phrase = _jql_quote(phrase)
        if len(text_phrase) > 1024:
            raise ValueError("summaryPhrase value cannot exceed 1024 characters")
        clauses.append("summary ~ " + _jql_quote(text_phrase))
    date = input.updated_since_date.strip()
    if date:
        try:
            if not re.fullmatch(r"[0-9]{4}-[0-9]{2}-[0-9]{2}", date):
                raise ValueError("invalid date shape")
            datetime.strptime(date, "%Y-%m-%d")
        except ValueError:
            raise ValueError("updatedSinceDate must be a calendar date written YYYY-MM-DD") from None
        clauses.append("updated >= " + _jql_quote(date))
    order = {
        None: "created DESC",
        "": "created DESC",
        "createdDescending": "created DESC",
        "updatedDescending": "updated DESC",
        "createdAscending": "created ASC",
    }.get(input.order)
    if order is None:
        raise ValueError("order must be createdDescending, updatedDescending, or createdAscending")
    return " AND ".join(clauses) + " ORDER BY " + order


class FindCustomerByEmailOperation:
    """Look up exact customer email matches in one service desk."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated lookup definition."""
        return defs.FIND_CUSTOMER_BY_EMAIL_DEFINITION

    async def invoke(self, call: Call, input: FindCustomerByEmailInput) -> QueryAttempt[CustomerMatches]:
        """Read and filter the first customer page."""
        try:
            desk_id = _numeric_id(input.service_desk_id, "serviceDeskId")
            email = _email_address(input.email)
        except ValueError as error:
            return QueryAttempt.branch_result(
                defs.DEFECT,
                CustomerMatches(),
                Receipt(),
                failure("findCustomerByEmail", FailureKind.VALIDATION, str(error)),
            )
        output = CustomerMatches(email)
        credentials, session_error = await self.client.resolve(call, "findCustomerByEmail")
        if session_error:
            return _query_failure(session_error, output, Receipt())
        assert credentials is not None
        response = await self.client.exchange(
            call,
            credentials,
            "GET",
            "service",
            f"/servicedesk/{path_escape(desk_id)}/customer",
            query={"query": email, "start": "0", "limit": "50"},
            experimental=True,
        )
        classification = self.client.classify_read("findCustomerByEmail", "customer lookup", response)
        receipt = self.client.receipt(call, response)
        if classification.outcome == "notFound":
            return QueryAttempt.branch_result(
                defs.PROVIDER_REJECTED,
                output,
                receipt,
                failure(
                    "findCustomerByEmail",
                    FailureKind.NOT_FOUND,
                    f"Jira Service Management has no service desk {desk_id} visible to the connection",
                ),
            )
        if classification.outcome != "success":
            return _query_failure(classification, output, receipt)
        try:
            values, is_last = _page(response.body)
            matches = []
            for raw in values:
                customer = _object(raw)
                account_id = _str_field(customer, "accountId")
                if _str_field(
                    customer, "emailAddress"
                ).strip().casefold() == email.casefold() and _ACCOUNT_ID.fullmatch(account_id):
                    active = customer.get("active")
                    matches.append(
                        Customer(
                            account_id,
                            _str_field(customer, "displayName"),
                            True if active is None else _boolean(active),
                        )
                    )
            output = CustomerMatches(email, tuple(matches), not is_last)
        except (ValueError, TypeError, KeyError) as error:
            return QueryAttempt.branch_result(
                defs.INVALID_RESPONSE,
                output,
                receipt,
                failure(
                    "findCustomerByEmail",
                    FailureKind.PROTOCOL,
                    "Jira Service Management returned an invalid customer page: " + str(error),
                ),
            )
        if not matches:
            return QueryAttempt.branch_result(
                defs.NOT_FOUND,
                output,
                receipt,
                failure(
                    "findCustomerByEmail",
                    FailureKind.NOT_FOUND,
                    "no customer of the service desk has this email address",
                ),
            )
        return QueryAttempt.branch_result(
            defs.FOUND, output, replace(receipt, provider_object_id=matches[0].account_id)
        )


class SearchTicketsOperation:
    """Search one bounded Jira issue page."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated search definition."""
        return defs.SEARCH_TICKETS_DEFINITION

    async def invoke(self, call: Call, input: SearchTicketsInput) -> QueryAttempt[SearchTicketsOutput]:
        """Send a typed JQL search and decode its page."""
        try:
            jql = _jql(input)
            page_size = input.page_size or 50
            if not 1 <= page_size <= 100:
                raise ValueError("pageSize must be between 1 and 100")
            token = input.next_page_token.strip()
            if len(token.encode()) > 4096 or any(not 33 <= ord(character) <= 126 for character in token):
                raise ValueError("nextPageToken must be the printable token from a previous search page")
        except ValueError as error:
            return QueryAttempt.branch_result(
                defs.DEFECT,
                SearchTicketsOutput(),
                Receipt(),
                failure("searchTickets", FailureKind.VALIDATION, str(error)),
            )
        output = SearchTicketsOutput(jql)
        credentials, session_error = await self.client.resolve(call, "searchTickets")
        if session_error:
            return _query_failure(session_error, output, Receipt(), not_found=defs.PROVIDER_REJECTED)
        assert credentials is not None
        payload = {"jql": jql, "fields": _TICKET_FIELDS.split(","), "maxResults": page_size}
        if token:
            payload["nextPageToken"] = token
        response = await self.client.exchange(
            call, credentials, "POST", "jira", "/search/jql", payload=payload
        )
        classification = self.client.classify_read("searchTickets", "ticket search", response)
        receipt = self.client.receipt(call, response)
        if classification.outcome != "success":
            return _query_failure(classification, output, receipt, not_found=defs.PROVIDER_REJECTED)
        try:
            page = _json_object(response.body)
            next_token = _str_field(page, "nextPageToken")
            if len(next_token.encode()) > 4096:
                raise ValueError("invalid page token")
            issues = page.get("issues")
            tickets = tuple(_ticket(item) for item in _array(issues)) if issues is not None else ()
            return QueryAttempt.branch_result(
                defs.SEARCHED, SearchTicketsOutput(jql, tickets, next_token), receipt
            )
        except (ValueError, TypeError, KeyError) as error:
            message = "Jira returned an invalid search page"
            if str(error) == "issue ID or key is invalid":
                message = "Jira returned an invalid request in the search page: " + str(error)
            return QueryAttempt.branch_result(
                defs.INVALID_RESPONSE,
                output,
                receipt,
                failure("searchTickets", FailureKind.PROTOCOL, message),
            )


def _comment(value: object) -> TicketComment:
    item = _object(value)
    comment_id = _str_field(item, "id")
    public = item.get("public")
    created = _service_date(item.get("created"))
    if (
        not comment_id
        or len(comment_id) > 128
        or any(not " " < char <= "~" for char in comment_id)
        or public is None
        or created is None
    ):
        raise ValueError("comment ID, visibility, or creation time is missing")
    body = _str_field(item, "body")
    author = item.get("author")
    author_view = _account(author)
    if author_view is not None and not _ACCOUNT_ID.fullmatch(author_view.account_id):
        author_view = None
    return TicketComment(comment_id, _boolean(public), body[:32767], len(body) > 32767, author_view, created)


def _sla_cycle(value: object, ongoing: bool) -> SLACycle:
    cycle = _object(value)
    started = _service_date(cycle.get("startTime"))
    if started is None:
        raise ValueError("SLA cycle start time is missing")

    def duration(name: str) -> int:
        item = cycle.get(name)
        return _int_field(_object(item), "millis") if item is not None else 0

    return SLACycle(
        started,
        _service_date(cycle.get("breachTime")),
        None if ongoing else _service_date(cycle.get("stopTime")),
        _bool_field(cycle, "breached"),
        _bool_field(cycle, "paused"),
        _bool_field(cycle, "withinCalendarHours"),
        duration("goalDuration"),
        duration("elapsedTime"),
        duration("remainingTime"),
    )


def _sla(value: object) -> TicketSLA:
    item = _object(value)
    sla_id = _str_field(item, "id")
    if not sla_id or len(sla_id) > 128 or any(not " " < char <= "~" for char in sla_id):
        raise ValueError("SLA ID is missing")
    ongoing = item.get("ongoingCycle")
    completed = item.get("completedCycles")
    return TicketSLA(
        sla_id,
        _str_field(item, "name"),
        _sla_cycle(ongoing, True) if ongoing is not None else None,
        tuple(_sla_cycle(cycle, False) for cycle in _array(completed)[:20]) if completed is not None else (),
    )


class GetTicketOperation:
    """Read an issue, its first comments, and its SLA cycles."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated ticket read definition."""
        return defs.GET_TICKET_DEFINITION

    async def invoke(self, call: Call, input: GetTicketInput) -> QueryAttempt[TicketDetails]:
        """Read a customer request through the Jira and service desk APIs."""
        empty = TicketDetails()
        try:
            issue = _issue_id(input.issue_id_or_key)
            limit = input.comment_limit or 10
            if not 1 <= limit <= 50:
                raise ValueError("commentLimit must be between 1 and 50")
        except ValueError as error:
            return QueryAttempt.branch_result(
                defs.DEFECT, empty, Receipt(), failure("getTicket", FailureKind.VALIDATION, str(error))
            )
        credentials, session_error = await self.client.resolve(call, "getTicket")
        if session_error:
            return _query_failure(session_error, empty, Receipt())
        assert credentials is not None
        response = await self.client.exchange(
            call,
            credentials,
            "GET",
            "jira",
            "/issue/" + path_escape(issue),
            query={"fields": _TICKET_FIELDS + ",description"},
        )
        receipt = self.client.receipt(call, response, issue)
        classification = self.client.classify_read("getTicket", "request", response)
        if classification.outcome != "success":
            return _query_failure(classification, empty, receipt)
        try:
            ticket = _ticket(_json_object(response.body), True)
        except (ValueError, TypeError, KeyError) as error:
            return QueryAttempt.branch_result(
                defs.INVALID_RESPONSE,
                empty,
                receipt,
                failure(
                    "getTicket", FailureKind.PROTOCOL, "Jira returned an invalid request issue: " + str(error)
                ),
            )
        receipt = replace(receipt, provider_object_id=ticket.key)
        comment_response = await self.client.exchange(
            call,
            credentials,
            "GET",
            "service",
            "/request/" + path_escape(ticket.key) + "/comment",
            query={"public": "true", "internal": "true", "start": "0", "limit": str(limit)},
        )
        classification = self.client.classify_read("getTicket", "request comments", comment_response)
        if classification.outcome != "success":
            return _query_failure(classification, empty, receipt)
        try:
            values, is_last = _page(comment_response.body)
            comments = tuple(_comment(value) for value in values)
            if len(comments) > limit:
                raise ValueError("too many comments")
        except (ValueError, TypeError, KeyError):
            return QueryAttempt.branch_result(
                defs.INVALID_RESPONSE,
                empty,
                receipt,
                failure(
                    "getTicket",
                    FailureKind.PROTOCOL,
                    "Jira Service Management returned an invalid comment page",
                ),
            )
        sla_response = await self.client.exchange(
            call,
            credentials,
            "GET",
            "service",
            "/request/" + path_escape(ticket.key) + "/sla",
            query={"start": "0", "limit": "50"},
        )
        classification = self.client.classify_read("getTicket", "request SLAs", sla_response)
        if classification.outcome != "success":
            return _query_failure(classification, empty, receipt)
        try:
            values, _ = _page(sla_response.body)
            slas = tuple(_sla(value) for value in values)
        except (ValueError, TypeError, KeyError):
            return QueryAttempt.branch_result(
                defs.INVALID_RESPONSE,
                empty,
                receipt,
                failure(
                    "getTicket", FailureKind.PROTOCOL, "Jira Service Management returned an invalid SLA page"
                ),
            )
        return QueryAttempt.branch_result(
            defs.FOUND, TicketDetails(ticket, comments, not is_last, slas), receipt
        )


@dataclass(frozen=True, slots=True)
class _DispatchMarker:
    dispatched_call_id: CallID = dataclass_field(
        metadata={"dex_json_name": "jiraServiceManagementDispatchedCallId"}
    )
    attempt: int
    can_resend: bool = dataclass_field(
        default=False, metadata={"dex_json_name": "canResend", "dex_json_omit_default": True}
    )


async def _claim_dispatch(call: Call, operation: str, output: Any) -> MutationAttempt[Any] | None:
    context = call.context
    if context is None:
        return MutationAttempt.uncertain(
            output,
            failure(
                operation,
                FailureKind.TRANSPORT,
                "an earlier attempt of this Step may have sent the request, so it is not sent again",
            ),
            Receipt(),
        )
    try:
        found = context.has_last_heartbeat_value()
        marker = context.get_last_heartbeat_value(_DispatchMarker) if found else None
        attempt = context.attempt
        allowed = attempt >= 1 and (
            (attempt == 1 and not found)
            or (
                attempt > 1
                and marker is not None
                and marker.dispatched_call_id == call.id
                and marker.attempt == attempt - 1
                and marker.can_resend
            )
        )
        if not allowed:
            raise ValueError("prior dispatch")
    except Exception:
        receipt = Receipt(call.id, "jira-service-management", datetime.now(UTC), call.idempotency_key)
        return MutationAttempt.uncertain(
            output,
            failure(
                operation,
                FailureKind.TRANSPORT,
                "an earlier attempt of this Step may have sent the request, so it is not sent again",
            ),
            receipt,
        )
    try:
        await context.heartbeat(_DispatchMarker(call.id, attempt))
    except Exception:
        return MutationAttempt.retry(
            failure(
                operation,
                FailureKind.AVAILABILITY,
                "Dex did not record the dispatch checkpoint; nothing was sent",
            )
        )
    return None


async def _release_dispatch(call: Call) -> None:
    if call.context is None:
        return
    try:
        await call.context.heartbeat(_DispatchMarker(call.id, call.context.attempt, True))
    except Exception:
        # A lost release marker fails closed on the next attempt.
        pass


async def _write_attempt(
    call: Call,
    operation: str,
    classification: Classification,
    output: Any,
    receipt: Receipt,
    *,
    not_found: str = defs.PROVIDER_REJECTED,
) -> MutationAttempt[Any] | None:
    if classification.outcome == "success":
        return None
    assert classification.failure is not None
    if classification.outcome == "retry":
        await _release_dispatch(call)
        return MutationAttempt.retry(classification.failure, classification.retry_after)
    if classification.outcome == "uncertain":
        return MutationAttempt.uncertain(output, classification.failure, receipt)
    if classification.outcome == "defect":
        await _release_dispatch(call)
        return MutationAttempt.branch_result(defs.DEFECT, output, receipt, classification.failure)
    branch = not_found if classification.outcome == "notFound" else defs.PROVIDER_REJECTED
    return MutationAttempt.branch_result(branch, output, receipt, classification.failure)


def _created_request(value: dict[str, Any]) -> tuple[str, str, datetime | None, RequestStatus | None]:
    issue_id = _str_field(value, "issueId")
    issue_key = _str_field(value, "issueKey")
    current = value.get("currentStatus")
    status = None
    if current is not None:
        item = _object(current)
        status = RequestStatus(_str_field(item, "status"), _str_field(item, "statusCategory"))
    return issue_id, issue_key, _service_date(value.get("createdDate")), status


class CreateTicketOperation:
    """Create one customer request with a durable dispatch checkpoint."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated create definition."""
        return defs.CREATE_TICKET_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreateTicketInput) -> IdempotencyKey:
        """Use the Call ID for the receipt; Jira accepts no idempotency header."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: CreateTicketInput) -> MutationAttempt[CreateTicketOutput]:
        """Send a single request and read back a recoverable provider reference."""
        requested = CreateTicketOutput(
            service_desk_id=input.service_desk_id.strip(),
            request_type_id=input.request_type_id.strip(),
            summary=input.summary.strip(),
            raise_on_behalf_of_account_id=input.raise_on_behalf_of_account_id.strip(),
        )
        try:
            desk_id = _numeric_id(requested.service_desk_id, "serviceDeskId")
            type_id = _numeric_id(requested.request_type_id, "requestTypeId")
            summary = _single_line(input.summary, "summary", 255)
            if requested.raise_on_behalf_of_account_id and not _ACCOUNT_ID.fullmatch(
                requested.raise_on_behalf_of_account_id
            ):
                raise ValueError("raiseOnBehalfOfAccountId must be an Atlassian account ID")
            _plain_text(input.description, "description", False)
            if len(input.additional_field_values) > 20:
                raise ValueError("additionalFieldValues accepts at most 20 fields")
            fields: dict[str, Any] = {}
            for name, value in input.additional_field_values.items():
                if not _FIELD_ID.fullmatch(name):
                    raise ValueError(
                        "additionalFieldValues keys must be Jira field IDs such as customfield_10050"
                    )
                if name in ("summary", "description"):
                    raise ValueError(
                        "set summary and description with their own fields, not additionalFieldValues"
                    )
                if len(json.dumps(value, separators=(",", ":")).encode()) > 16384:
                    raise ValueError("each additionalFieldValues value must be valid JSON of at most 16 KiB")
                fields[name] = value
            fields["summary"] = summary
            if input.description.strip():
                fields["description"] = input.description
        except (ValueError, TypeError) as error:
            return MutationAttempt.branch_result(
                defs.DEFECT, requested, Receipt(), failure("createTicket", FailureKind.VALIDATION, str(error))
            )
        claim = await _claim_dispatch(call, "createTicket", requested)
        if claim:
            return claim
        credentials, session_error = await self.client.resolve(call, "createTicket")
        if session_error:
            if session_error.outcome == "retry":
                await _release_dispatch(call)
            return _mutation_failure(session_error, requested, Receipt(), not_found=defs.PROVIDER_REJECTED)
        assert credentials is not None
        payload = {"serviceDeskId": desk_id, "requestTypeId": type_id, "requestFieldValues": fields}
        if requested.raise_on_behalf_of_account_id:
            payload["raiseOnBehalfOf"] = requested.raise_on_behalf_of_account_id
        response = await self.client.exchange(
            call, credentials, "POST", "service", "/request", payload=payload
        )
        classification = self.client.classify_write("createTicket", "customer request", response)
        receipt = self.client.receipt(call, response)
        if response.status_code == 409:
            try:
                recovered = await self._read_back(call, credentials, _json_object(response.body), requested)
            except (ValueError, TypeError, KeyError):
                recovered = None
            if recovered:
                return self._created(requested, recovered, receipt)
        if classification.outcome == "providerRejected":
            requested = replace(
                requested,
                rejected_field_ids=classification.field_ids,
                provider_error_key=classification.error_key,
            )
        attempt = await _write_attempt(call, "createTicket", classification, requested, receipt)
        if attempt:
            return attempt
        try:
            created = _json_object(response.body)
            issue_id, issue_key, _, _ = _created_request(created)
        except (ValueError, TypeError, KeyError):
            return MutationAttempt.uncertain(
                requested,
                failure(
                    "createTicket",
                    FailureKind.PROTOCOL,
                    "Jira Service Management accepted the customer request but "
                    "returned an unusable request reference",
                ),
                receipt,
            )
        if not _NUMERIC_ID.fullmatch(issue_id) or not _ISSUE_KEY.fullmatch(issue_key):
            recovered = await self._read_back(call, credentials, created, requested)
            if not recovered:
                return MutationAttempt.uncertain(
                    requested,
                    failure(
                        "createTicket",
                        FailureKind.PROTOCOL,
                        "Jira Service Management accepted the customer request but "
                        "its reference could not be confirmed",
                    ),
                    receipt,
                )
            created = recovered
        return self._created(requested, created, receipt)

    def _created(
        self, requested: CreateTicketOutput, created: dict[str, Any], receipt: Receipt
    ) -> MutationAttempt[CreateTicketOutput]:
        issue_id, issue_key, created_at, status = _created_request(created)
        output = replace(
            requested, issue_id=issue_id, issue_key=issue_key, created_at=created_at, request_status=status
        )
        return MutationAttempt.branch_result(
            defs.CREATED, output, replace(receipt, provider_object_id=issue_key)
        )

    async def _read_back(
        self, call: Call, credentials: Any, accepted: dict[str, Any], requested: CreateTicketOutput
    ) -> dict[str, Any] | None:
        issue_id = _str_field(accepted, "issueId")
        issue_key = _str_field(accepted, "issueKey")
        reference = issue_id if _NUMERIC_ID.fullmatch(issue_id) else issue_key
        if not _NUMERIC_ID.fullmatch(reference) and not _ISSUE_KEY.fullmatch(reference):
            return None
        response = await self.client.exchange(
            call, credentials, "GET", "service", "/request/" + path_escape(reference)
        )
        if self.client.classify_read("createTicket", "customer request", response).outcome != "success":
            return None
        try:
            recovered = _json_object(response.body)
            recovered_id, recovered_key, _, _ = _created_request(recovered)
            reporter = recovered.get("reporter")
            if not _NUMERIC_ID.fullmatch(recovered_id) or not _ISSUE_KEY.fullmatch(recovered_key):
                return None
            if (
                _str_field(recovered, "serviceDeskId") != requested.service_desk_id
                or _str_field(recovered, "requestTypeId") != requested.request_type_id
                or _str_field(recovered, "summary") != requested.summary
            ):
                return None
            if requested.raise_on_behalf_of_account_id and (
                reporter is None
                or _str_field(_object(reporter), "accountId") != requested.raise_on_behalf_of_account_id
            ):
                return None
            if (_NUMERIC_ID.fullmatch(issue_id) and recovered_id != issue_id) or (
                _ISSUE_KEY.fullmatch(issue_key) and recovered_key != issue_key
            ):
                return None
            return recovered
        except (ValueError, TypeError, KeyError):
            return None


class AddCommentOperation:
    """Add one public reply or internal note with single dispatch."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated comment definition."""
        return defs.ADD_COMMENT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: AddCommentInput) -> IdempotencyKey:
        """Use the Call ID for the receipt; Jira accepts no idempotency header."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: AddCommentInput) -> MutationAttempt[AddCommentOutput]:
        """Send one comment and confirm a conflict only by matching its read back."""
        requested = AddCommentOutput(input.issue_id_or_key, is_public=input.is_public)
        try:
            issue = _issue_id(input.issue_id_or_key)
            requested = replace(requested, issue_id_or_key=issue)
            _plain_text(input.body, "body", True)
        except ValueError as error:
            return MutationAttempt.branch_result(
                defs.DEFECT, requested, Receipt(), failure("addComment", FailureKind.VALIDATION, str(error))
            )
        claim = await _claim_dispatch(call, "addComment", requested)
        if claim:
            return claim
        credentials, session_error = await self.client.resolve(call, "addComment")
        if session_error:
            if session_error.outcome == "retry":
                await _release_dispatch(call)
            return _mutation_failure(session_error, requested, Receipt())
        assert credentials is not None
        response = await self.client.exchange(
            call,
            credentials,
            "POST",
            "service",
            "/request/" + path_escape(issue) + "/comment",
            payload={"body": input.body, "public": input.is_public},
        )
        classification = self.client.classify_write("addComment", "comment", response)
        receipt = self.client.receipt(call, response, issue)
        if response.status_code == 409:
            try:
                conflict = _json_object(response.body)
                recovered = await self._read_back(call, credentials, issue, input, _str_field(conflict, "id"))
            except (ValueError, TypeError, KeyError):
                recovered = None
            if recovered:
                return self._added(requested, recovered, receipt)
        attempt = await _write_attempt(
            call, "addComment", classification, requested, receipt, not_found=defs.NOT_FOUND
        )
        if attempt:
            return attempt
        try:
            created = _json_object(response.body)
            if not _NUMERIC_ID.fullmatch(_str_field(created, "id")):
                raise ValueError("invalid comment ID")
        except (ValueError, TypeError, KeyError):
            return MutationAttempt.uncertain(
                requested,
                failure(
                    "addComment",
                    FailureKind.PROTOCOL,
                    "Jira Service Management accepted the comment but returned an unusable comment reference",
                ),
                receipt,
            )
        return self._added(requested, created, receipt)

    def _added(
        self, requested: AddCommentOutput, created: dict[str, Any], receipt: Receipt
    ) -> MutationAttempt[AddCommentOutput]:
        public = created.get("public")
        output = replace(
            requested,
            comment_id=_str_field(created, "id"),
            is_public=requested.is_public if public is None else _boolean(public),
            created_at=_service_date(created.get("created")),
        )
        return MutationAttempt.branch_result(defs.ADDED, output, receipt)

    async def _read_back(
        self, call: Call, credentials: Any, issue: str, input: AddCommentInput, comment_id: str
    ) -> dict[str, Any] | None:
        if not _NUMERIC_ID.fullmatch(comment_id):
            return None
        response = await self.client.exchange(
            call, credentials, "GET", "service", "/request/" + path_escape(issue) + "/comment/" + comment_id
        )
        if self.client.classify_read("addComment", "comment", response).outcome != "success":
            return None
        try:
            recovered = _json_object(response.body)
            if _str_field(recovered, "id") != comment_id or recovered.get("public") is None:
                return None
            if (
                _str_field(recovered, "body") != input.body
                or _boolean(recovered["public"]) != input.is_public
            ):
                return None
            return recovered
        except (ValueError, TypeError, KeyError):
            return None


class UpdateTicketOperation:
    """Apply label and priority changes after reading current issue state."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated update definition."""
        return defs.UPDATE_TICKET_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpdateTicketInput) -> IdempotencyKey:
        """Derive the stable Call ID receipt key."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: UpdateTicketInput) -> MutationAttempt[UpdateTicketOutput]:
        """Read current values and send only changes not already applied."""
        empty = UpdateTicketOutput()
        try:
            issue = _issue_id(input.issue_id_or_key)
            add = _labels(input.add_labels, "addLabels")
            remove = _labels(input.remove_labels, "removeLabels")
            if set(add) & set(remove):
                raise ValueError("a label cannot be both added and removed")
            priority_name = (
                _single_line(input.priority_name, "priorityName", 255) if input.priority_name.strip() else ""
            )
            if not add and not remove and not priority_name:
                raise ValueError("set addLabels, removeLabels, or priorityName")
        except ValueError as error:
            return MutationAttempt.branch_result(
                defs.DEFECT, empty, Receipt(), failure("updateTicket", FailureKind.VALIDATION, str(error))
            )
        credentials, session_error = await self.client.resolve(call, "updateTicket")
        if session_error:
            return _mutation_failure(session_error, empty, Receipt())
        assert credentials is not None
        response = await self.client.exchange(
            call,
            credentials,
            "GET",
            "jira",
            "/issue/" + path_escape(issue),
            query={"fields": "labels,priority"},
        )
        receipt = self.client.receipt(call, response, issue)
        classification = self.client.classify_read("updateTicket", "request", response)
        if classification.outcome != "success":
            return _mutation_failure(classification, empty, receipt)
        try:
            current = _json_object(response.body)
            issue_id = _str_field(current, "id")
            issue_key = _str_field(current, "key")
            if not _NUMERIC_ID.fullmatch(issue_id) or not _ISSUE_KEY.fullmatch(issue_key):
                raise ValueError("invalid issue")
            fields = _optional_object(current.get("fields"))
            existing_labels = fields.get("labels")
            labels = (
                tuple(_string(value) for value in _array(existing_labels))
                if existing_labels is not None
                else ()
            )
            output = UpdateTicketOutput(issue_id, issue_key, labels, _priority(fields.get("priority")))
        except (ValueError, TypeError, KeyError):
            return MutationAttempt.branch_result(
                defs.INVALID_RESPONSE,
                empty,
                receipt,
                failure("updateTicket", FailureKind.PROTOCOL, "Jira returned an invalid request issue"),
            )
        receipt = replace(receipt, provider_object_id=issue_key)
        present = set(output.labels)
        removed: set[str] = set()
        operations: list[dict[str, str]] = []
        for label in remove:
            if label in present and label not in removed:
                removed.add(label)
                operations.append({"remove": label})
        labels_after = [label for label in output.labels if label not in removed]
        for label in add:
            if label not in present:
                present.add(label)
                operations.append({"add": label})
                labels_after.append(label)
        payload: dict[str, Any] = {}
        if operations:
            payload["update"] = {"labels": operations}
        if priority_name and (
            output.priority is None or output.priority.name.casefold() != priority_name.casefold()
        ):
            payload["fields"] = {"priority": {"name": priority_name}}
        if not payload:
            return MutationAttempt.branch_result(
                defs.UPDATED, replace(output, was_already_applied=True), receipt
            )
        response = await self.client.exchange(
            call, credentials, "PUT", "jira", "/issue/" + path_escape(issue_key), payload=payload
        )
        receipt = self.client.receipt(call, response, issue_key)
        classification = self.client.classify_read("updateTicket", "issue update", response)
        if classification.outcome != "success":
            if classification.outcome == "providerRejected":
                output = replace(output, rejected_field_ids=classification.field_ids)
            return _mutation_failure(classification, output, receipt)
        return MutationAttempt.branch_result(
            defs.UPDATED,
            replace(
                output,
                labels=tuple(labels_after),
                priority=PriorityReference(name=priority_name) if "fields" in payload else output.priority,
            ),
            receipt,
        )


class TransitionTicketOperation:
    """Select and reconcile a Jira workflow transition."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated transition definition."""
        return defs.TRANSITION_TICKET_DEFINITION

    def idempotency_key(self, call_id: CallID, input: TransitionTicketInput) -> IdempotencyKey:
        """Derive the stable Call ID receipt key."""
        return IdempotencyKey(call_id)

    async def _snapshot(
        self, call: Call, credentials: Any, issue: str
    ) -> tuple[
        tuple[str, str, TicketStatus, tuple[TransitionReference, ...]] | None,
        ProviderResponse,
        Classification,
    ]:
        response = await self.client.exchange(
            call,
            credentials,
            "GET",
            "jira",
            "/issue/" + path_escape(issue),
            query={"fields": "status", "expand": "transitions"},
        )
        classification = self.client.classify_read("transitionTicket", "request", response)
        if classification.outcome != "success":
            return None, response, classification
        try:
            resource = _json_object(response.body)
            issue_id = _str_field(resource, "id")
            issue_key = _str_field(resource, "key")
            fields = _optional_object(resource.get("fields"))
            raw_status = fields.get("status")
            if (
                raw_status is None
                or not _NUMERIC_ID.fullmatch(issue_id)
                or not _ISSUE_KEY.fullmatch(issue_key)
            ):
                raise ValueError("invalid issue or transition list")
            status = _status(raw_status)
            transitions: list[TransitionReference] = []
            raw_transitions = resource.get("transitions")
            for raw in _array(raw_transitions) if raw_transitions is not None else []:
                transition = _object(raw)
                transition_id = _str_field(transition, "id")
                destination = transition.get("to")
                if transition_id and destination is not None and len(transitions) < 50:
                    transitions.append(
                        TransitionReference(
                            transition_id, _str_field(transition, "name"), _status(destination)
                        )
                    )
            return (issue_id, issue_key, status, tuple(transitions)), response, classification
        except (ValueError, TypeError, KeyError):
            return (
                None,
                response,
                Classification(
                    "invalidResponse",
                    failure(
                        "transitionTicket",
                        FailureKind.PROTOCOL,
                        "Jira returned an invalid issue or transition list",
                    ),
                ),
            )

    async def invoke(
        self, call: Call, input: TransitionTicketInput
    ) -> MutationAttempt[TransitionTicketOutput]:
        """Read transitions, send one selected transition, and reconcile ambiguity."""
        empty = TransitionTicketOutput()
        try:
            issue = _issue_id(input.issue_id_or_key)
            transition_id = input.transition_id.strip()
            if transition_id and not _NUMERIC_ID.fullmatch(transition_id):
                raise ValueError("transitionId must be a numeric transition ID")
            transition_name = (
                _single_line(input.transition_name, "transitionName", 255)
                if input.transition_name.strip()
                else ""
            )
            destination_name = (
                _single_line(input.destination_status_name, "destinationStatusName", 255)
                if input.destination_status_name.strip()
                else ""
            )
            resolution_name = (
                _single_line(input.resolution_name, "resolutionName", 255)
                if input.resolution_name.strip()
                else ""
            )
            if not transition_id and not transition_name and not destination_name:
                raise ValueError("set transitionId, transitionName, or destinationStatusName")
        except ValueError as error:
            return MutationAttempt.branch_result(
                defs.DEFECT, empty, Receipt(), failure("transitionTicket", FailureKind.VALIDATION, str(error))
            )
        credentials, session_error = await self.client.resolve(call, "transitionTicket")
        if session_error:
            return _mutation_failure(session_error, empty, Receipt())
        assert credentials is not None
        snapshot, read_response, classification = await self._snapshot(call, credentials, issue)
        receipt = self.client.receipt(call, read_response, issue)
        if snapshot is None:
            return _mutation_failure(classification, empty, receipt)
        issue_id, issue_key, status, transitions = snapshot
        receipt = replace(receipt, provider_object_id=issue_key)
        output = TransitionTicketOutput(issue_id, issue_key, status, status)
        if destination_name and status.name.casefold() == destination_name.casefold():
            return MutationAttempt.branch_result(
                defs.TRANSITIONED, replace(output, is_already_in_destination_status=True), receipt
            )
        matches = tuple(
            transition
            for transition in transitions
            if (not transition_id or transition.id == transition_id)
            and (not transition_name or transition.name.strip().casefold() == transition_name.casefold())
            and (
                not destination_name
                or transition.destination_status.name.casefold() == destination_name.casefold()
            )
        )
        if len(matches) != 1:
            if matches:
                reason = failure(
                    "transitionTicket",
                    FailureKind.VALIDATION,
                    f"{len(matches)} transitions available from status {status.id} "
                    "match the request; set transitionId",
                )
            else:
                reason = failure(
                    "transitionTicket",
                    FailureKind.CONFLICT,
                    f"no transition available from status {status.id} matches the request",
                )
            return MutationAttempt.branch_result(
                defs.TRANSITION_UNAVAILABLE,
                replace(output, available_transitions=transitions),
                receipt,
                reason,
            )
        chosen = matches[0]
        output = replace(output, transition=chosen)
        if chosen.destination_status.id == status.id:
            return MutationAttempt.branch_result(
                defs.TRANSITIONED, replace(output, is_already_in_destination_status=True), receipt
            )
        payload: dict[str, Any] = {"transition": {"id": chosen.id}}
        if resolution_name:
            payload["fields"] = {"resolution": {"name": resolution_name}}
        response = await self.client.exchange(
            call,
            credentials,
            "POST",
            "jira",
            "/issue/" + path_escape(issue) + "/transitions",
            payload=payload,
        )
        receipt = self.client.receipt(call, response, issue_key)
        if response.request_not_built:
            return MutationAttempt.branch_result(
                defs.DEFECT,
                output,
                receipt,
                failure("transitionTicket", FailureKind.LOCAL_DEFECT, "Jira request could not be built"),
            )
        if not response.transport_error and 200 <= response.status_code < 300:
            return MutationAttempt.branch_result(
                defs.TRANSITIONED, replace(output, status=chosen.destination_status), receipt
            )
        if not response.transport_error and response.status_code == 404:
            return MutationAttempt.branch_result(
                defs.NOT_FOUND,
                output,
                receipt,
                failure(
                    "transitionTicket",
                    FailureKind.NOT_FOUND,
                    "Jira issue was not found or is not visible to the connection",
                ),
            )
        if not response.transport_error and response.status_code == 429:
            return MutationAttempt.retry(
                failure(
                    "transitionTicket",
                    FailureKind.RATE_LIMIT,
                    "Jira rate limited the transition before performing it",
                ),
                self.client.retry_after(response),
            )
        if response.transport_error and not response.is_dispatched:
            return MutationAttempt.retry(
                failure(
                    "transitionTicket",
                    FailureKind.TRANSPORT,
                    "Jira could not be reached, so the issue was not transitioned",
                )
            )
        reread, _, reread_classification = await self._snapshot(call, credentials, issue)
        if reread is not None and reread[2].id == chosen.destination_status.id:
            return MutationAttempt.branch_result(
                defs.TRANSITIONED, replace(output, status=reread[2]), receipt
            )
        if reread is None:
            return MutationAttempt.retry(
                failure(
                    "transitionTicket",
                    FailureKind.AVAILABILITY,
                    "Jira transition outcome could not be read back",
                )
            )
        if (
            not response.transport_error
            and 400 <= response.status_code < 500
            and response.status_code not in (408, 409, 429)
        ):
            classification = self.client.classify_read("transitionTicket", "transition", response)
            return MutationAttempt.branch_result(
                defs.PROVIDER_REJECTED,
                replace(output, status=reread[2], rejected_field_ids=classification.field_ids),
                receipt,
                classification.failure,
            )
        if not response.transport_error and response.status_code < 400:
            return MutationAttempt.branch_result(
                defs.INVALID_RESPONSE,
                output,
                receipt,
                failure(
                    "transitionTicket",
                    FailureKind.PROTOCOL,
                    f"Jira returned unexpected HTTP {response.status_code} for the transition",
                ),
            )
        if not response.transport_error and response.status_code == 409:
            return MutationAttempt.retry(
                failure(
                    "transitionTicket",
                    FailureKind.CONFLICT,
                    "Jira reported a conflicting update, so the transition is retried",
                ),
                self.client.retry_after(response),
            )
        return MutationAttempt.retry(
            failure(
                "transitionTicket",
                FailureKind.AVAILABILITY,
                "Jira did not confirm the transition and the issue has not moved yet",
            ),
            self.client.retry_after(response),
        )
