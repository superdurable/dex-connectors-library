# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Jira Service Management operation invocation and provider outcomes."""

import json
import re
from dataclasses import replace
from datetime import UTC, date, datetime, timedelta
from email.utils import parseaddr
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
from dex_connectors.sdk.providerhttp import parse_retry_after
from dex_connectors.sdk.singledispatch import (
    CheckpointError,
    DecisionKind,
    DispatchNotAllowedError,
    Fence,
    InsufficientAttemptTimeError,
)

from ._generated.definitions import (
    ADD_COMMENT_DEFINITION,
    CREATE_TICKET_DEFINITION,
    FIND_CUSTOMER_BY_EMAIL_DEFINITION,
    GET_TICKET_DEFINITION,
    SEARCH_TICKETS_DEFINITION,
    TRANSITION_TICKET_DEFINITION,
    UPDATE_TICKET_DEFINITION,
)
from .client import (
    Client,
    Exchange,
    Session,
    SessionProblem,
    failure,
    path_escape,
    rejection_fields,
    status_failure_kind,
)
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


def _object(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError("expected object")
    return value


def _string(value: Any, default: str = "") -> str:
    if value is None:
        return default
    if not isinstance(value, str):
        raise ValueError("expected string")
    return value


def _boolean(value: Any, default: bool = False) -> bool:
    if value is None:
        return default
    if type(value) is not bool:
        raise ValueError("expected boolean")
    return value


def _integer(value: Any, default: int = 0) -> int:
    if value is None:
        return default
    if type(value) is not int:
        raise ValueError("expected integer")
    return value


def _array(value: Any) -> list[Any]:
    if not isinstance(value, list):
        raise ValueError("expected array")
    return value


def _response_object(exchange: Exchange) -> dict[str, Any]:
    return _object(json.loads(exchange.body))


def _page(exchange: Exchange) -> tuple[list[Any], bool]:
    payload = _response_object(exchange)
    return _array(payload["values"]), _boolean(payload.get("isLastPage"))


def _issue_id(value: str) -> str:
    cleaned = value.strip()
    if not (_NUMERIC_ID.fullmatch(cleaned) or _ISSUE_KEY.fullmatch(cleaned)):
        raise ValueError("issueIdOrKey must be a Jira issue ID or key such as ITH-12")
    return cleaned


def _jira_time(value: str) -> datetime:
    if not value:
        return datetime.min.replace(tzinfo=UTC)
    match = re.fullmatch(
        r"([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})(?:[.,]([0-9]+))?(Z|[+-][0-9]{2}:?[0-9]{2})",
        value,
    )
    if match is None:
        raise ValueError("timestamp is not a Jira date-time")
    year, month, day, hour, minute, second = (int(match.group(index)) for index in range(1, 7))
    fraction = (match.group(7) or "")[:6].ljust(6, "0")
    instant = datetime(year, month, day, hour, minute, second, int(fraction), tzinfo=UTC)
    zone = match.group(8)
    if zone != "Z":
        sign = 1 if zone[0] == "+" else -1
        digits = zone[1:].replace(":", "")
        offset_hour, offset_minute = int(digits[:2]), int(digits[2:])
        if offset_minute > 59:
            raise ValueError("timestamp is not a Jira date-time")
        instant -= sign * timedelta(hours=offset_hour, minutes=offset_minute)
    return instant


def _service_desk_time(value: Any) -> datetime | None:
    if value is None:
        return None
    date_object = _object(value)
    millis = date_object.get("epochMillis")
    if millis is not None:
        return datetime.fromtimestamp(_integer(millis) / 1000, UTC)
    parsed = _jira_time(_string(date_object.get("iso8601")))
    return None if parsed == datetime.min.replace(tzinfo=UTC) else parsed


def _status(value: Any) -> TicketStatus:
    if value is None:
        return TicketStatus()
    status = _object(value)
    category = status.get("statusCategory")
    category_key = _string(_object(category).get("key")) if category is not None else None
    return TicketStatus(_string(status.get("id")), _string(status.get("name")), category_key)


def _account(value: Any) -> AccountReference | None:
    if value is None:
        return None
    account = _object(value)
    account_id = _string(account.get("accountId"))
    return AccountReference(account_id, _string(account.get("displayName"))) if account_id else None


def _priority(value: Any) -> PriorityReference | None:
    if value is None:
        return None
    priority = _object(value)
    return PriorityReference(_string(priority.get("id")), _string(priority.get("name")))


def _document_text(value: Any) -> tuple[str, bool]:
    if value is None:
        return "", False
    if isinstance(value, str):
        return value[:32767], len(value) > 32767
    document = _object(value)
    parts: list[str] = []

    def write_node(node: Any, depth: int) -> None:
        if depth > 48:
            return
        item = _object(node)
        kind = _string(item.get("type"))
        if kind == "text":
            parts.append(_string(item.get("text")))
        elif kind == "hardBreak":
            parts.append("\n")
        elif kind in ("mention", "emoji", "status", "inlineCard", "blockCard", "embedCard"):
            attrs = item.get("attrs")
            parts.append(
                _string(_object(attrs).get("text" if kind in ("mention", "emoji", "status") else "url"))
                if attrs is not None
                else ""
            )
        elif kind == "rule":
            parts.append("---")
        elif kind not in ("media", "mediaSingle", "mediaGroup", "mediaInline"):
            children = item.get("content")
            if children is not None:
                separator = "\n\n" if depth == 1 else "\n"
                for index, child in enumerate(_array(children)):
                    if index:
                        parts.append(separator)
                    write_node(child, depth + 1)

    for index, node in enumerate(_array(document.get("content", []))):
        if index:
            parts.append("\n\n")
        write_node(node, 1)
    result = "".join(parts).rstrip("\n")
    return result[:32767], len(result) > 32767


def _decode_ticket(issue: dict[str, Any], description: bool) -> Ticket:
    issue_id = _string(issue.get("id"))
    key = _string(issue.get("key"))
    if not _NUMERIC_ID.fullmatch(issue_id) or not _ISSUE_KEY.fullmatch(key):
        raise ValueError("issue ID or key is invalid")
    fields = _object(issue.get("fields")) if issue.get("fields") is not None else {}
    project = fields.get("project")
    labels = fields.get("labels")
    description_text, is_truncated = _document_text(fields.get("description")) if description else ("", False)
    return Ticket(
        id=issue_id,
        key=key,
        project_key=_string(_object(project).get("key")) if project is not None else "",
        summary=_string(fields.get("summary")),
        status=_status(fields.get("status")),
        priority=_priority(fields.get("priority")),
        assignee=_account(fields.get("assignee")),
        reporter=_account(fields.get("reporter")),
        labels=tuple(_string(label) for label in _array(labels)) if labels is not None else (),
        description=description_text,
        is_description_truncated=is_truncated,
        created_at=_jira_time(_string(fields.get("created"))),
        updated_at=_jira_time(_string(fields.get("updated"))),
    )


def _numeric_id(value: str, name: str) -> str:
    cleaned = value.strip()
    if not _NUMERIC_ID.fullmatch(cleaned):
        raise ValueError(f"{name} must be a positive numeric Jira ID")
    return cleaned


def _email(value: str) -> str:
    cleaned = value.strip()
    name, address = parseaddr(cleaned)
    local, separator, domain = cleaned.partition("@")
    local_atom = r"[\w!#$%&'*+\-/=?^_`{|}~]+"
    valid_local = re.fullmatch(local_atom + r"(?:\." + local_atom + r")*", local) is not None
    valid_domain = (
        re.fullmatch(r"[\w-]+(?:\.[\w-]+)*", domain) is not None
        or re.fullmatch(r"\[[^\]\s]+\]", domain) is not None
    )
    if (
        not separator
        or not valid_local
        or not valid_domain
        or name
        or (address != cleaned and not domain.startswith("["))
        or len(cleaned.encode()) > 254
    ):
        raise ValueError("email must be one bare address such as jane@example.com")
    return cleaned


def _retry_after(exchange: Exchange) -> float:
    return parse_retry_after(exchange.headers.get("Retry-After", ""), datetime.now(UTC))


def _query_outcome(operation: str, subject: str, exchange: Exchange) -> tuple[str, Failure | None, float]:
    if exchange.error == "notBuilt":
        return (
            "defect",
            failure(
                operation, FailureKind.LOCAL_DEFECT, "Jira Service Management request could not be built"
            ),
            0,
        )
    if exchange.error:
        return (
            "retry",
            failure(
                operation,
                FailureKind.TRANSPORT,
                f"Jira Service Management {subject} is temporarily unavailable",
            ),
            0,
        )
    status = exchange.status
    if status == 404:
        return (
            "notFound",
            failure(
                operation,
                FailureKind.NOT_FOUND,
                f"Jira Service Management {subject} was not found or is not visible to the connection",
            ),
            0,
        )
    if status == 429:
        return (
            "retry",
            failure(operation, FailureKind.RATE_LIMIT, f"Jira Service Management rate limited the {subject}"),
            _retry_after(exchange),
        )
    if status == 408 or status >= 500:
        return (
            "retry",
            failure(
                operation,
                FailureKind.AVAILABILITY,
                f"Jira Service Management {subject} is temporarily unavailable",
            ),
            _retry_after(exchange),
        )
    if 400 <= status < 500:
        return (
            "providerRejected",
            failure(
                operation,
                status_failure_kind(status),
                f"Jira Service Management rejected the {subject} with HTTP {status}",
            ),
            0,
        )
    if not 200 <= status < 300:
        return (
            "invalidResponse",
            failure(
                operation,
                FailureKind.PROTOCOL,
                f"Jira Service Management returned unexpected HTTP {status} for the {subject}",
            ),
            0,
        )
    if exchange.has_body_read_failed:
        return (
            "retry",
            failure(
                operation,
                FailureKind.TRANSPORT,
                f"Jira Service Management {subject} response was interrupted",
            ),
            0,
        )
    if exchange.is_body_too_large:
        return (
            "invalidResponse",
            failure(
                operation,
                FailureKind.RESPONSE_TOO_LARGE,
                f"Jira Service Management {subject} response exceeds the configured size limit",
            ),
            0,
        )
    if exchange.reflects_secret:
        return (
            "invalidResponse",
            failure(
                operation,
                FailureKind.PROTOCOL,
                f"Jira Service Management {subject} response contains credential material",
            ),
            0,
        )
    return "success", None, 0


def _query_problem(operation: str, problem: SessionProblem, output: Any) -> QueryAttempt[Any]:
    if problem.retry:
        return QueryAttempt.retry(problem.failure, problem.retry_after)
    return QueryAttempt.branch_result("defect", output, Receipt(), problem.failure)


def _mutation_problem(operation: str, problem: SessionProblem, output: Any) -> MutationAttempt[Any]:
    if problem.retry:
        return MutationAttempt.retry(problem.failure, problem.retry_after)
    return MutationAttempt.branch_result("defect", output, Receipt(), problem.failure)


def _query_classified(
    operation: str,
    exchange: Exchange,
    subject: str,
    output: Any,
    receipt: Receipt,
    *,
    not_found_branch: str = "providerRejected",
) -> QueryAttempt[Any] | None:
    outcome, problem, delay = _query_outcome(operation, subject, exchange)
    if outcome == "success":
        return None
    if outcome == "retry":
        assert problem is not None
        return QueryAttempt.retry(problem, delay)
    branch = not_found_branch if outcome == "notFound" else outcome
    return QueryAttempt.branch_result(branch, output, receipt, problem)


def _write_outcome(operation: str, subject: str, exchange: Exchange) -> tuple[str, Failure | None, float]:
    if exchange.error == "notBuilt":
        return (
            "defect",
            failure(
                operation, FailureKind.LOCAL_DEFECT, "Jira Service Management request could not be built"
            ),
            0,
        )
    if exchange.error == "beforeSend":
        return (
            "retry",
            failure(
                operation,
                FailureKind.TRANSPORT,
                f"Jira Service Management could not be reached, so no {subject} was written",
            ),
            0,
        )
    if exchange.error:
        return (
            "uncertain",
            failure(
                operation, FailureKind.TRANSPORT, f"Jira Service Management {subject} outcome is unknown"
            ),
            0,
        )
    status = exchange.status
    if 200 <= status < 300:
        if exchange.has_body_read_failed:
            return (
                "uncertain",
                failure(
                    operation,
                    FailureKind.TRANSPORT,
                    f"Jira Service Management accepted the {subject} but its response was interrupted",
                ),
                0,
            )
        if exchange.is_body_too_large:
            return (
                "uncertain",
                failure(
                    operation,
                    FailureKind.RESPONSE_TOO_LARGE,
                    f"Jira Service Management accepted the {subject} but its response exceeds "
                    "the configured size limit",
                ),
                0,
            )
        if exchange.reflects_secret:
            return (
                "uncertain",
                failure(
                    operation,
                    FailureKind.PROTOCOL,
                    f"Jira Service Management accepted the {subject} but its response "
                    "contains credential material",
                ),
                0,
            )
        return "success", None, 0
    if status == 429:
        return (
            "retry",
            failure(
                operation,
                FailureKind.RATE_LIMIT,
                f"Jira Service Management rate limited the {subject} before writing it",
            ),
            _retry_after(exchange),
        )
    if status == 503:
        return (
            "retry",
            failure(
                operation,
                FailureKind.AVAILABILITY,
                f"Jira Service Management is temporarily unavailable for the {subject}",
            ),
            _retry_after(exchange),
        )
    if status == 409:
        return (
            "uncertain",
            failure(
                operation,
                FailureKind.CONFLICT,
                f"Jira Service Management {subject} outcome requires read-back after HTTP 409",
            ),
            0,
        )
    if status == 404:
        return (
            "notFound",
            failure(
                operation,
                FailureKind.NOT_FOUND,
                f"Jira Service Management rejected the {subject} with HTTP 404",
            ),
            0,
        )
    if 400 <= status < 500 and status != 408:
        return (
            "providerRejected",
            failure(
                operation,
                status_failure_kind(status),
                f"Jira Service Management rejected the {subject} with HTTP {status}",
            ),
            0,
        )
    return (
        "uncertain",
        failure(
            operation,
            FailureKind.AVAILABILITY,
            f"Jira Service Management {subject} outcome is unknown after HTTP {status}",
        ),
        0,
    )


def _plain_text(value: str, field_name: str, required: bool) -> None:
    if required and not value.strip():
        raise ValueError(f"{field_name} is required")
    if len(value) > 32767:
        raise ValueError(f"{field_name} cannot exceed 32767 characters")
    if any(ord(character) == 0 or (ord(character) < 32 and character not in "\n\r\t") for character in value):
        raise ValueError(f"{field_name} cannot contain control characters")


async def _single_dispatch(
    client: Client,
    call: Call,
    operation: str,
    requested: Any,
    send: Any,
) -> tuple[Exchange | None, MutationAttempt[Any] | None, Fence]:
    fence = Fence(call, timedelta(seconds=30))
    decision = await fence.decide()
    if decision.kind != DecisionKind.SEND_ALLOWED:
        return (
            None,
            MutationAttempt.uncertain(
                requested,
                failure(
                    operation,
                    FailureKind.TRANSPORT,
                    "an earlier attempt of this Step may have sent the request, so it is not sent again",
                ),
                client.receipt(call, Exchange()),
            ),
            fence,
        )
    try:
        session = await client.session(call, operation)
    except SessionProblem as problem:
        if problem.retry:
            try:
                await fence.record_not_sent()
            except Exception:
                pass
            return None, MutationAttempt.retry(problem.failure, problem.retry_after), fence
        return None, MutationAttempt.branch_result("defect", requested, Receipt(), problem.failure), fence
    try:
        exchange = await fence.dispatch(lambda _deadline: send(session))
    except (CheckpointError, InsufficientAttemptTimeError):
        return (
            None,
            MutationAttempt.retry(
                failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "Dex did not record the dispatch checkpoint; nothing was sent",
                )
            ),
            fence,
        )
    except (TimeoutError, DispatchNotAllowedError):
        return (
            None,
            MutationAttempt.uncertain(
                requested,
                failure(
                    operation,
                    FailureKind.TRANSPORT,
                    f"Jira Service Management {operation} outcome is unknown",
                ),
                client.receipt(call, Exchange()),
            ),
            fence,
        )
    return exchange, None, fence


async def _single_dispatch_outcome(
    client: Client,
    call: Call,
    operation: str,
    subject: str,
    exchange: Exchange,
    requested: Any,
    receipt: Receipt,
    fence: Fence,
    *,
    not_found_branch: str = "providerRejected",
) -> MutationAttempt[Any] | None:
    route, problem, delay = _write_outcome(operation, subject, exchange)
    if route == "success":
        return None
    if route == "retry":
        assert problem is not None
        try:
            await fence.record_can_resend()
        except Exception:
            return MutationAttempt.uncertain(
                requested,
                failure(operation, FailureKind.LOCAL_DEFECT, "Jira retry checkpoint is unavailable"),
                receipt,
            )
        return MutationAttempt.retry(problem, delay)
    if route == "uncertain":
        assert problem is not None
        return MutationAttempt.uncertain(requested, problem, receipt)
    return MutationAttempt.branch_result(
        not_found_branch if route == "notFound" else route, requested, receipt, problem
    )


class FindCustomerByEmailOperation:
    """Find customers whose email matches exactly on one service desk."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the stable customer lookup contract."""
        return FIND_CUSTOMER_BY_EMAIL_DEFINITION

    async def invoke(self, call: Call, input: FindCustomerByEmailInput) -> QueryAttempt[CustomerMatches]:
        """Read one bounded customer page and select found or notFound."""
        try:
            desk_id = _numeric_id(input.service_desk_id, "serviceDeskId")
            email = _email(input.email)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect",
                CustomerMatches(),
                Receipt(),
                failure("findCustomerByEmail", FailureKind.VALIDATION, str(error)),
            )
        output = CustomerMatches(email=email)
        try:
            session = await self.client.session(call, "findCustomerByEmail")
        except SessionProblem as problem:
            return _query_problem("findCustomerByEmail", problem, output)
        exchange = await self.client.exchange(
            session,
            "GET",
            "desk",
            f"/servicedesk/{path_escape(desk_id)}/customer",
            {"query": email, "start": "0", "limit": "50"},
            experimental=True,
        )
        receipt = self.client.receipt(call, exchange)
        classified = _query_classified("findCustomerByEmail", exchange, "customer lookup", output, receipt)
        if classified is not None:
            return classified
        try:
            users, is_last = _page(exchange)
            customers = tuple(
                Customer(
                    account_id=_string(user.get("accountId")),
                    display_name=_string(user.get("displayName")),
                    is_active=_boolean(user.get("active"), True),
                )
                for raw_user in users
                if (user := _object(raw_user))
                and _string(user.get("emailAddress")).strip().casefold() == email.casefold()
                and _ACCOUNT_ID.fullmatch(_string(user.get("accountId")))
            )
        except (ValueError, TypeError, KeyError) as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                output,
                receipt,
                failure(
                    "findCustomerByEmail",
                    FailureKind.PROTOCOL,
                    f"Jira Service Management returned an invalid customer page: {error}",
                ),
            )
        output = CustomerMatches(email=email, customers=customers, has_more=not is_last)
        if not customers:
            return QueryAttempt.branch_result(
                "notFound",
                output,
                receipt,
                failure(
                    "findCustomerByEmail",
                    FailureKind.NOT_FOUND,
                    "no customer of the service desk has this email address",
                ),
            )
        return QueryAttempt.branch_result(
            "found", output, replace(receipt, provider_object_id=customers[0].account_id)
        )


def _jql_quote(value: str) -> str:
    return '"' + value.replace("\\", "\\\\").replace('"', '\\"') + '"'


def _jql_input(input: SearchTicketsInput) -> tuple[dict[str, Any], str]:
    project = input.project_key.strip()
    if not _PROJECT_KEY.fullmatch(project):
        raise ValueError("projectKey must be the service desk's uppercase project key such as ITH")
    clauses = ["project = " + _jql_quote(project)]
    if input.status_categories:
        category_ids = {"new": "2", "indeterminate": "4", "done": "3"}
        try:
            clauses.append(
                "statusCategory in ("
                + ", ".join(category_ids[value] for value in input.status_categories)
                + ")"
            )
        except KeyError as error:
            raise ValueError("statusCategories accepts new, indeterminate, and done") from error
    for account_id in input.reporter_account_ids:
        if not _ACCOUNT_ID.fullmatch(account_id):
            raise ValueError("reporterAccountIds must be Atlassian account IDs")
    for field_name, values in (
        ("status", input.status_names),
        ("labels", input.labels),
        ("reporter", input.reporter_account_ids),
    ):
        if len(values) > 50:
            raise ValueError(f"search accepts at most 50 {field_name} values")
        if values:
            cleaned = [value.strip() for value in values]
            if any(
                not value or len(value) > 1024 or any(ord(c) < 32 or ord(c) == 127 for c in value)
                for value in cleaned
            ):
                raise ValueError(f"search {field_name} values cannot be blank or contain control characters")
            clauses.append(f"{field_name} in (" + ", ".join(_jql_quote(value) for value in cleaned) + ")")
    phrase = input.summary_phrase.strip()
    if phrase:
        nested = _jql_quote(phrase)
        if len(nested) > 1024:
            raise ValueError("summaryPhrase value cannot exceed 1024 characters")
        clauses.append("summary ~ " + _jql_quote(nested))
    since = input.updated_since_date.strip()
    if since:
        try:
            if date.fromisoformat(since).isoformat() != since:
                raise ValueError("invalid date")
        except ValueError as error:
            raise ValueError("updatedSinceDate must be a calendar date written YYYY-MM-DD") from error
        clauses.append("updated >= " + _jql_quote(since))
    order = {
        None: "created DESC",
        "": "created DESC",
        "createdDescending": "created DESC",
        "updatedDescending": "updated DESC",
        "createdAscending": "created ASC",
    }.get(input.order)
    if order is None:
        raise ValueError("order must be createdDescending, updatedDescending, or createdAscending")
    jql = " AND ".join(clauses) + " ORDER BY " + order
    size = input.page_size or 50
    if type(size) is not int or not 1 <= size <= 100:
        raise ValueError("pageSize must be between 1 and 100")
    token = input.next_page_token.strip()
    if len(token.encode()) > 4096 or any(not 33 <= ord(c) <= 126 for c in token):
        raise ValueError("nextPageToken must be the printable token from a previous search page")
    body = {
        "jql": jql,
        "fields": [
            "summary",
            "status",
            "project",
            "assignee",
            "reporter",
            "priority",
            "labels",
            "created",
            "updated",
        ],
        "maxResults": size,
    }
    if token:
        body["nextPageToken"] = token
    return body, jql


class SearchTicketsOperation:
    """Search one bounded page of Jira requests."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the stable search contract."""
        return SEARCH_TICKETS_DEFINITION

    async def invoke(self, call: Call, input: SearchTicketsInput) -> QueryAttempt[SearchTicketsOutput]:
        """Build safe JQL and decode one enhanced search page."""
        try:
            body, jql = _jql_input(input)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect",
                SearchTicketsOutput(),
                Receipt(),
                failure("searchTickets", FailureKind.VALIDATION, str(error)),
            )
        output = SearchTicketsOutput(jql=jql)
        try:
            session = await self.client.session(call, "searchTickets")
        except SessionProblem as problem:
            return _query_problem("searchTickets", problem, output)
        exchange = await self.client.exchange(session, "POST", "jira", "/search/jql", payload=body)
        receipt = self.client.receipt(call, exchange)
        classified = _query_classified("searchTickets", exchange, "ticket search", output, receipt)
        if classified is not None:
            return classified
        try:
            page = _response_object(exchange)
            token = _string(page.get("nextPageToken"))
            if len(token.encode()) > 4096:
                raise ValueError("invalid next page token")
            tickets = tuple(_decode_ticket(_object(issue), False) for issue in _array(page.get("issues", [])))
        except (ValueError, TypeError, KeyError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                output,
                receipt,
                failure("searchTickets", FailureKind.PROTOCOL, "Jira returned an invalid search page"),
            )
        return QueryAttempt.branch_result("searched", SearchTicketsOutput(jql, tickets, token), receipt)


def _comment(raw: Any) -> TicketComment:
    comment = _object(raw)
    comment_id = _string(comment.get("id"))
    created = _service_desk_time(comment.get("created"))
    public = comment.get("public")
    if (
        not comment_id
        or not all(33 <= ord(c) <= 126 for c in comment_id)
        or created is None
        or type(public) is not bool
    ):
        raise ValueError("comment ID, visibility, or creation time is missing")
    body = _string(comment.get("body"))
    author = comment.get("author")
    return TicketComment(comment_id, public, body[:32767], len(body) > 32767, _account(author), created)


def _sla_cycle(raw: Any) -> SLACycle:
    cycle = _object(raw)
    started = _service_desk_time(cycle.get("startTime"))
    if started is None:
        raise ValueError("SLA cycle start time is missing")

    def millis(name: str) -> int:
        duration = cycle.get(name)
        return _integer(_object(duration).get("millis")) if duration is not None else 0

    return SLACycle(
        started_at=started,
        breach_at=_service_desk_time(cycle.get("breachTime")),
        stopped_at=_service_desk_time(cycle.get("stopTime")),
        is_breached=_boolean(cycle.get("breached")),
        is_paused=_boolean(cycle.get("paused")),
        is_within_calendar_hours=_boolean(cycle.get("withinCalendarHours")),
        goal_duration_millis=millis("goalDuration"),
        elapsed_millis=millis("elapsedTime"),
        remaining_millis=millis("remainingTime"),
    )


def _sla(raw: Any) -> TicketSLA:
    sla = _object(raw)
    sla_id = _string(sla.get("id"))
    if not sla_id or not all(33 <= ord(c) <= 126 for c in sla_id):
        raise ValueError("SLA ID is missing")
    ongoing = sla.get("ongoingCycle")
    converted = _sla_cycle(ongoing) if ongoing is not None else None
    if converted is not None:
        converted = replace(converted, stopped_at=None)
    completed = sla.get("completedCycles")
    return TicketSLA(
        sla_id,
        _string(sla.get("name")),
        converted,
        tuple(_sla_cycle(item) for item in _array(completed)[:20]) if completed is not None else (),
    )


class GetTicketOperation:
    """Read the request issue, comments, and SLA page."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the stable ticket detail contract."""
        return GET_TICKET_DEFINITION

    async def invoke(self, call: Call, input: GetTicketInput) -> QueryAttempt[TicketDetails]:
        """Read all three provider resources in one Execute attempt."""
        empty = TicketDetails()
        try:
            reference = _issue_id(input.issue_id_or_key)
            comment_limit = input.comment_limit or 10
            if type(comment_limit) is not int or not 1 <= comment_limit <= 50:
                raise ValueError("commentLimit must be between 1 and 50")
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", empty, Receipt(), failure("getTicket", FailureKind.VALIDATION, str(error))
            )
        try:
            session = await self.client.session(call, "getTicket")
        except SessionProblem as problem:
            return _query_problem("getTicket", problem, empty)
        exchange = await self.client.exchange(
            session,
            "GET",
            "jira",
            f"/issue/{path_escape(reference)}",
            {
                "fields": "summary,status,project,assignee,reporter,priority,labels,"
                "created,updated,description"
            },
        )
        receipt = self.client.receipt(call, exchange, reference)
        classified = _query_classified(
            "getTicket", exchange, "request", empty, receipt, not_found_branch="notFound"
        )
        if classified is not None:
            return classified
        try:
            ticket = _decode_ticket(_response_object(exchange), True)
        except (ValueError, TypeError, KeyError) as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                failure(
                    "getTicket", FailureKind.PROTOCOL, f"Jira returned an invalid request issue: {error}"
                ),
            )
        receipt = replace(receipt, provider_object_id=ticket.key)
        path = f"/request/{path_escape(ticket.key)}"
        comments_exchange = await self.client.exchange(
            session,
            "GET",
            "desk",
            path + "/comment",
            {"public": "true", "internal": "true", "start": "0", "limit": str(comment_limit)},
        )
        classified = _query_classified(
            "getTicket", comments_exchange, "request comments", empty, receipt, not_found_branch="notFound"
        )
        if classified is not None:
            return classified
        try:
            raw_comments, is_last = _page(comments_exchange)
            comments = tuple(_comment(value) for value in raw_comments)
            if len(comments) > comment_limit:
                raise ValueError("too many comments")
        except (ValueError, TypeError, KeyError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                failure(
                    "getTicket",
                    FailureKind.PROTOCOL,
                    "Jira Service Management returned an invalid comment page",
                ),
            )
        sla_exchange = await self.client.exchange(
            session, "GET", "desk", path + "/sla", {"start": "0", "limit": "50"}
        )
        classified = _query_classified(
            "getTicket", sla_exchange, "request SLAs", empty, receipt, not_found_branch="notFound"
        )
        if classified is not None:
            return classified
        try:
            raw_slas, _ = _page(sla_exchange)
            slas = tuple(_sla(value) for value in raw_slas)
        except (ValueError, TypeError, KeyError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                failure(
                    "getTicket", FailureKind.PROTOCOL, "Jira Service Management returned an invalid SLA page"
                ),
            )
        return QueryAttempt.branch_result(
            "found", TicketDetails(ticket, comments, not is_last, slas), receipt
        )


def _create_ticket_request(input: CreateTicketInput) -> tuple[dict[str, Any], CreateTicketOutput]:
    output = CreateTicketOutput(
        service_desk_id=input.service_desk_id.strip(),
        request_type_id=input.request_type_id.strip(),
        summary=input.summary.strip(),
        raise_on_behalf_of_account_id=input.raise_on_behalf_of_account_id.strip(),
    )
    _numeric_id(output.service_desk_id, "serviceDeskId")
    _numeric_id(output.request_type_id, "requestTypeId")
    if (
        not output.summary
        or len(output.summary) > 255
        or any(ord(c) < 32 or ord(c) == 127 for c in output.summary)
    ):
        raise ValueError("summary must be one line of at most 255 characters")
    if output.raise_on_behalf_of_account_id and not _ACCOUNT_ID.fullmatch(
        output.raise_on_behalf_of_account_id
    ):
        raise ValueError("raiseOnBehalfOfAccountId must be an Atlassian account ID")
    _plain_text(input.description, "description", False)
    additional = input.additional_field_values
    if len(additional) > 20:
        raise ValueError("additionalFieldValues accepts at most 20 fields")
    fields: dict[str, Any] = {}
    for field_id, value in additional.items():
        if not _FIELD_ID.fullmatch(field_id) or field_id in ("summary", "description"):
            raise ValueError(
                "additionalFieldValues keys must be Jira field IDs other than summary or description"
            )
        if len(json.dumps(value, separators=(",", ":")).encode()) > 16384:
            raise ValueError("each additionalFieldValues value must be valid JSON of at most 16 KiB")
        fields[field_id] = value
    fields["summary"] = output.summary
    if input.description.strip():
        fields["description"] = input.description
    body = {
        "serviceDeskId": output.service_desk_id,
        "requestTypeId": output.request_type_id,
        "requestFieldValues": fields,
    }
    if output.raise_on_behalf_of_account_id:
        body["raiseOnBehalfOf"] = output.raise_on_behalf_of_account_id
    return body, output


def _created_ticket_output(resource: dict[str, Any], requested: CreateTicketOutput) -> CreateTicketOutput:
    issue_id = _string(resource.get("issueId"))
    key = _string(resource.get("issueKey"))
    if not _NUMERIC_ID.fullmatch(issue_id) or not _ISSUE_KEY.fullmatch(key):
        raise ValueError("unusable request reference")
    status = resource.get("currentStatus")
    current_status = (
        RequestStatus(_string(_object(status).get("status")), _string(_object(status).get("statusCategory")))
        if status is not None
        else None
    )
    return replace(
        requested,
        issue_id=issue_id,
        issue_key=key,
        request_status=current_status,
        created_at=_service_desk_time(resource.get("createdDate")),
    )


class CreateTicketOperation:
    """Create one customer request under a Dex dispatch checkpoint."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the stable customer request contract."""
        return CREATE_TICKET_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreateTicketInput) -> IdempotencyKey:
        """Record the call ID without sending it to Jira."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: CreateTicketInput) -> MutationAttempt[CreateTicketOutput]:
        """Send at most once and read back a returned ambiguous reference."""
        requested = CreateTicketOutput(
            service_desk_id=input.service_desk_id.strip(),
            request_type_id=input.request_type_id.strip(),
            summary=input.summary.strip(),
            raise_on_behalf_of_account_id=input.raise_on_behalf_of_account_id.strip(),
        )
        try:
            body, requested = _create_ticket_request(input)
        except (ValueError, TypeError) as error:
            return MutationAttempt.branch_result(
                "defect", requested, Receipt(), failure("createTicket", FailureKind.VALIDATION, str(error))
            )
        exchange, terminal, fence = await _single_dispatch(
            self.client,
            call,
            "createTicket",
            requested,
            lambda session: self.client.exchange(session, "POST", "desk", "/request", payload=body),
        )
        if terminal is not None:
            return terminal
        assert exchange is not None
        receipt = self.client.receipt(call, exchange)
        if exchange.status == 409:
            try:
                conflict = _response_object(exchange)
                reference = _string(conflict.get("issueId"))
                if not _NUMERIC_ID.fullmatch(reference):
                    reference = _string(conflict.get("issueKey"))
                if _NUMERIC_ID.fullmatch(reference) or _ISSUE_KEY.fullmatch(reference):
                    session = await self.client.session(call, "createTicket")
                    read = await self.client.exchange(
                        session, "GET", "desk", f"/request/{path_escape(reference)}"
                    )
                    if _query_outcome("createTicket", "customer request", read)[0] == "success":
                        recovered = _response_object(read)
                        if (
                            _string(recovered.get("serviceDeskId")) == requested.service_desk_id
                            and _string(recovered.get("requestTypeId")) == requested.request_type_id
                            and _string(recovered.get("summary")) == requested.summary
                        ):
                            result = _created_ticket_output(recovered, requested)
                            return MutationAttempt.branch_result(
                                "created", result, replace(receipt, provider_object_id=result.issue_key)
                            )
            except (ValueError, SessionProblem, KeyError, TypeError):
                pass
        route, _, _ = _write_outcome("createTicket", "customer request", exchange)
        if route == "providerRejected":
            rejected_fields, error_key = rejection_fields(exchange)
            requested = replace(requested, rejected_field_ids=rejected_fields, provider_error_key=error_key)
        terminal = await _single_dispatch_outcome(
            self.client, call, "createTicket", "customer request", exchange, requested, receipt, fence
        )
        if terminal is not None:
            return terminal
        try:
            created = _response_object(exchange)
            result = _created_ticket_output(created, requested)
        except (ValueError, TypeError, KeyError):
            try:
                created = _response_object(exchange)
                reference = _string(created.get("issueId"))
                if not _NUMERIC_ID.fullmatch(reference):
                    reference = _string(created.get("issueKey"))
                if _NUMERIC_ID.fullmatch(reference) or _ISSUE_KEY.fullmatch(reference):
                    session = await self.client.session(call, "createTicket")
                    read = await self.client.exchange(
                        session, "GET", "desk", f"/request/{path_escape(reference)}"
                    )
                    if _query_outcome("createTicket", "customer request", read)[0] == "success":
                        recovered = _response_object(read)
                        if (
                            _string(recovered.get("serviceDeskId")) == requested.service_desk_id
                            and _string(recovered.get("requestTypeId")) == requested.request_type_id
                            and _string(recovered.get("summary")) == requested.summary
                        ):
                            result = _created_ticket_output(recovered, requested)
                            return MutationAttempt.branch_result(
                                "created", result, replace(receipt, provider_object_id=result.issue_key)
                            )
            except (ValueError, SessionProblem, KeyError, TypeError):
                pass
            return MutationAttempt.uncertain(
                requested,
                failure(
                    "createTicket",
                    FailureKind.PROTOCOL,
                    "Jira Service Management accepted the customer request but returned "
                    "an unusable request reference",
                ),
                receipt,
            )
        return MutationAttempt.branch_result(
            "created", result, replace(receipt, provider_object_id=result.issue_key)
        )


class AddCommentOperation:
    """Add a public reply or internal note exactly once."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the stable comment contract."""
        return ADD_COMMENT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: AddCommentInput) -> IdempotencyKey:
        """Record the call ID without sending it to Jira."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: AddCommentInput) -> MutationAttempt[AddCommentOutput]:
        """Checkpoint before POST and confirm a conflicting comment by read back."""
        requested = AddCommentOutput(issue_id_or_key=input.issue_id_or_key, is_public=input.is_public)
        try:
            reference = _issue_id(input.issue_id_or_key)
            _plain_text(input.body, "body", True)
            requested = replace(requested, issue_id_or_key=reference)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", requested, Receipt(), failure("addComment", FailureKind.VALIDATION, str(error))
            )
        path = f"/request/{path_escape(reference)}/comment"
        exchange, terminal, fence = await _single_dispatch(
            self.client,
            call,
            "addComment",
            requested,
            lambda session: self.client.exchange(
                session, "POST", "desk", path, payload={"body": input.body, "public": input.is_public}
            ),
        )
        if terminal is not None:
            return terminal
        assert exchange is not None
        receipt = self.client.receipt(call, exchange, reference)
        if exchange.status == 409:
            try:
                conflict = _response_object(exchange)
                comment_id = _string(conflict.get("id"))
                if _NUMERIC_ID.fullmatch(comment_id):
                    session = await self.client.session(call, "addComment")
                    read = await self.client.exchange(session, "GET", "desk", path + "/" + comment_id)
                    if _query_outcome("addComment", "comment", read)[0] == "success":
                        recovered = _response_object(read)
                        if (
                            _string(recovered.get("id")) == comment_id
                            and _string(recovered.get("body")) == input.body
                            and _boolean(recovered.get("public")) == input.is_public
                        ):
                            return MutationAttempt.branch_result(
                                "added",
                                replace(
                                    requested,
                                    comment_id=comment_id,
                                    created_at=_service_desk_time(recovered.get("created")),
                                ),
                                receipt,
                            )
            except (ValueError, SessionProblem, KeyError, TypeError):
                pass
        terminal = await _single_dispatch_outcome(
            self.client,
            call,
            "addComment",
            "comment",
            exchange,
            requested,
            receipt,
            fence,
            not_found_branch="notFound",
        )
        if terminal is not None:
            return terminal
        try:
            created = _response_object(exchange)
            comment_id = _string(created.get("id"))
            if not _NUMERIC_ID.fullmatch(comment_id):
                raise ValueError("invalid comment ID")
            public = _boolean(created.get("public"), input.is_public)
            return MutationAttempt.branch_result(
                "added",
                replace(
                    requested,
                    comment_id=comment_id,
                    is_public=public,
                    created_at=_service_desk_time(created.get("created")),
                ),
                receipt,
            )
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


def _validated_labels(labels: tuple[str, ...], field_name: str) -> tuple[str, ...]:
    if len(labels) > 50:
        raise ValueError(f"{field_name} accepts at most 50 labels")
    if any(
        not label
        or len(label) > 255
        or any(c.isspace() or ord(c) < 32 or ord(c) in (127, 133, 160) for c in label)
        for label in labels
    ):
        raise ValueError(f"each {field_name} label must be 1 to 255 characters without whitespace")
    return labels


class UpdateTicketOperation:
    """Apply label and priority differences to one Jira issue."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the stable issue update contract."""
        return UPDATE_TICKET_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpdateTicketInput) -> IdempotencyKey:
        """Record the stable call ID for the repeatable edit."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: UpdateTicketInput) -> MutationAttempt[UpdateTicketOutput]:
        """Read current fields, send only differences, and retry transient failures."""
        empty = UpdateTicketOutput()
        try:
            reference = _issue_id(input.issue_id_or_key)
            additions = _validated_labels(input.add_labels, "addLabels")
            removals = _validated_labels(input.remove_labels, "removeLabels")
            if set(additions) & set(removals):
                raise ValueError("a label cannot be both added and removed")
            priority_name = input.priority_name.strip()
            if priority_name and (
                len(priority_name) > 255 or any(ord(c) < 32 or ord(c) == 127 for c in priority_name)
            ):
                raise ValueError("priorityName must be one line of at most 255 characters")
            if not additions and not removals and not priority_name:
                raise ValueError("set addLabels, removeLabels, or priorityName")
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", empty, Receipt(), failure("updateTicket", FailureKind.VALIDATION, str(error))
            )
        try:
            session = await self.client.session(call, "updateTicket")
        except SessionProblem as problem:
            return _mutation_problem("updateTicket", problem, empty)
        read = await self.client.exchange(
            session, "GET", "jira", f"/issue/{path_escape(reference)}", {"fields": "labels,priority"}
        )
        receipt = self.client.receipt(call, read, reference)
        classified = self._classified(read, "request", empty, receipt)
        if classified is not None:
            return classified
        try:
            resource = _response_object(read)
            issue_id, issue_key = _string(resource.get("id")), _string(resource.get("key"))
            if not _NUMERIC_ID.fullmatch(issue_id) or not _ISSUE_KEY.fullmatch(issue_key):
                raise ValueError("invalid issue ID or key")
            fields = _object(resource.get("fields"))
            labels = (
                tuple(_string(label) for label in _array(fields.get("labels")))
                if fields.get("labels") is not None
                else ()
            )
            priority = _priority(fields.get("priority"))
        except (ValueError, TypeError, KeyError):
            return MutationAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                failure("updateTicket", FailureKind.PROTOCOL, "Jira returned an invalid request issue"),
            )
        output = UpdateTicketOutput(issue_id, issue_key, labels, priority)
        receipt = replace(receipt, provider_object_id=issue_key)
        present = set(labels)
        removed = set()
        operations: list[dict[str, str]] = []
        for label in removals:
            if label in present and label not in removed:
                removed.add(label)
                operations.append({"remove": label})
        labels_after = [label for label in labels if label not in removed]
        for label in additions:
            if label not in present:
                present.add(label)
                operations.append({"add": label})
                labels_after.append(label)
        body: dict[str, Any] = {}
        if operations:
            body["update"] = {"labels": operations}
        if priority_name and (priority is None or priority.name.casefold() != priority_name.casefold()):
            body["fields"] = {"priority": {"name": priority_name}}
        if not body:
            return MutationAttempt.branch_result(
                "updated", replace(output, was_already_applied=True), receipt
            )
        written = await self.client.exchange(
            session, "PUT", "jira", f"/issue/{path_escape(issue_key)}", payload=body
        )
        receipt = self.client.receipt(call, written, issue_key)
        classified = self._classified(written, "issue update", output, receipt)
        if classified is not None:
            return classified
        return MutationAttempt.branch_result(
            "updated",
            replace(
                output,
                labels=tuple(labels_after),
                priority=PriorityReference(name=priority_name) if "fields" in body else priority,
            ),
            receipt,
        )

    def _classified(
        self, exchange: Exchange, subject: str, output: UpdateTicketOutput, receipt: Receipt
    ) -> MutationAttempt[UpdateTicketOutput] | None:
        route, problem, delay = _query_outcome("updateTicket", subject, exchange)
        if route == "success":
            return None
        if route == "retry":
            assert problem is not None
            return MutationAttempt.retry(problem, delay)
        if route == "providerRejected":
            output = replace(output, rejected_field_ids=rejection_fields(exchange)[0])
        return MutationAttempt.branch_result(route, output, receipt, problem)


class TransitionTicketOperation:
    """Select and apply one Jira workflow transition by read back."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the stable transition contract."""
        return TRANSITION_TICKET_DEFINITION

    def idempotency_key(self, call_id: CallID, input: TransitionTicketInput) -> IdempotencyKey:
        """Record the call ID while using current status for repeatability."""
        return IdempotencyKey(call_id)

    async def _read(
        self, session: Session, reference: str
    ) -> tuple[Exchange, dict[str, Any], TicketStatus, tuple[TransitionReference, ...]]:
        read = await self.client.exchange(
            session,
            "GET",
            "jira",
            f"/issue/{path_escape(reference)}",
            {"fields": "status", "expand": "transitions"},
        )
        if _query_outcome("transitionTicket", "request", read)[0] != "success":
            return read, {}, TicketStatus(), ()
        try:
            resource = _response_object(read)
            issue_id, issue_key = _string(resource.get("id")), _string(resource.get("key"))
            if not _NUMERIC_ID.fullmatch(issue_id) or not _ISSUE_KEY.fullmatch(issue_key):
                raise ValueError("invalid issue reference")
            status_resource = _object(_object(resource.get("fields")).get("status"))
            status = _status(status_resource)
            transitions = tuple(
                TransitionReference(
                    _string(item.get("id")), _string(item.get("name")), _status(item.get("to"))
                )
                for raw in _array(resource.get("transitions"))[:50]
                if (item := _object(raw)).get("id") and item.get("to") is not None
            )
            return read, {"id": issue_id, "key": issue_key}, status, transitions
        except (ValueError, TypeError, KeyError):
            return replace(read, error="invalidResponse"), {}, TicketStatus(), ()

    async def invoke(
        self, call: Call, input: TransitionTicketInput
    ) -> MutationAttempt[TransitionTicketOutput]:
        """Read available transitions, write one, and reconcile uncertain results."""
        empty = TransitionTicketOutput()
        try:
            reference = _issue_id(input.issue_id_or_key)
            transition_id = input.transition_id.strip()
            if transition_id and not _NUMERIC_ID.fullmatch(transition_id):
                raise ValueError("transitionId must be a numeric transition ID")
            names = [
                input.transition_name.strip(),
                input.destination_status_name.strip(),
                input.resolution_name.strip(),
            ]
            for name in names:
                if name and (len(name) > 255 or any(ord(c) < 32 or ord(c) == 127 for c in name)):
                    raise ValueError("transition fields must be one line of at most 255 characters")
            if not transition_id and not names[0] and not names[1]:
                raise ValueError("set transitionId, transitionName, or destinationStatusName")
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", empty, Receipt(), failure("transitionTicket", FailureKind.VALIDATION, str(error))
            )
        try:
            session = await self.client.session(call, "transitionTicket")
        except SessionProblem as session_problem:
            return _mutation_problem("transitionTicket", session_problem, empty)
        read, resource, status, transitions = await self._read(session, reference)
        receipt = self.client.receipt(call, read, reference)
        route, problem, delay = _query_outcome("transitionTicket", "request", read)
        if read.error == "invalidResponse":
            route, problem = (
                "invalidResponse",
                failure(
                    "transitionTicket",
                    FailureKind.PROTOCOL,
                    "Jira returned an invalid issue or transition list",
                ),
            )
        if route != "success":
            if route == "retry":
                assert problem is not None
                return MutationAttempt.retry(problem, delay)
            return MutationAttempt.branch_result(route, empty, receipt, problem)
        receipt = replace(receipt, provider_object_id=resource["key"])
        output = TransitionTicketOutput(
            issue_id=resource["id"], issue_key=resource["key"], previous_status=status, status=status
        )
        if names[1] and status.name.casefold() == names[1].casefold():
            return MutationAttempt.branch_result(
                "transitioned", replace(output, is_already_in_destination_status=True), receipt
            )
        matching = tuple(
            item
            for item in transitions
            if (not transition_id or item.id == transition_id)
            and (not names[0] or item.name.strip().casefold() == names[0].casefold())
            and (not names[1] or item.destination_status.name.casefold() == names[1].casefold())
        )
        if len(matching) != 1:
            kind = FailureKind.CONFLICT if not matching else FailureKind.VALIDATION
            return MutationAttempt.branch_result(
                "transitionUnavailable",
                replace(output, available_transitions=transitions),
                receipt,
                failure("transitionTicket", kind, "no unique transition matches the request"),
            )
        chosen = matching[0]
        output = replace(output, transition=chosen)
        if chosen.destination_status.id == status.id:
            return MutationAttempt.branch_result(
                "transitioned", replace(output, is_already_in_destination_status=True), receipt
            )
        body: dict[str, Any] = {"transition": {"id": chosen.id}}
        if names[2]:
            body["fields"] = {"resolution": {"name": names[2]}}
        write = await self.client.exchange(
            session, "POST", "jira", f"/issue/{path_escape(reference)}/transitions", payload=body
        )
        receipt = self.client.receipt(call, write, resource["key"])
        if not write.error and 200 <= write.status < 300:
            return MutationAttempt.branch_result(
                "transitioned", replace(output, status=chosen.destination_status), receipt
            )
        if not write.error and write.status == 404:
            return MutationAttempt.branch_result(
                "notFound",
                output,
                receipt,
                failure(
                    "transitionTicket",
                    FailureKind.NOT_FOUND,
                    "Jira issue was not found or is not visible to the connection",
                ),
            )
        if not write.error and write.status == 429:
            return MutationAttempt.retry(
                failure(
                    "transitionTicket",
                    FailureKind.RATE_LIMIT,
                    "Jira rate limited the transition before performing it",
                ),
                self.client.retry_after(write),
            )
        if write.error == "beforeSend":
            return MutationAttempt.retry(
                failure(
                    "transitionTicket",
                    FailureKind.TRANSPORT,
                    "Jira could not be reached, so the issue was not transitioned",
                )
            )
        reread, resource_after, status_after, _ = await self._read(session, reference)
        if _query_outcome("transitionTicket", "request", reread)[0] != "success":
            return MutationAttempt.retry(
                failure(
                    "transitionTicket",
                    FailureKind.AVAILABILITY,
                    "Jira transition outcome could not be read back",
                )
            )
        if status_after.id == chosen.destination_status.id:
            return MutationAttempt.branch_result(
                "transitioned", replace(output, status=status_after), receipt
            )
        if not write.error and 400 <= write.status < 500 and write.status not in (408, 409, 429):
            return MutationAttempt.branch_result(
                "providerRejected",
                replace(output, status=status_after, rejected_field_ids=rejection_fields(write)[0]),
                receipt,
                failure(
                    "transitionTicket",
                    status_failure_kind(write.status),
                    f"Jira rejected the transition with HTTP {write.status}",
                ),
            )
        if not write.error and write.status == 409:
            return MutationAttempt.retry(
                failure(
                    "transitionTicket",
                    FailureKind.CONFLICT,
                    "Jira reported a conflicting update, so the transition is retried",
                ),
                self.client.retry_after(write),
            )
        return MutationAttempt.retry(
            failure(
                "transitionTicket",
                FailureKind.AVAILABILITY,
                "Jira did not confirm the transition and the issue has not moved yet",
            ),
            self.client.retry_after(write),
        )
