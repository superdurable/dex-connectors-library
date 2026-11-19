# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Five Jira Cloud operation invokes and their provider-safe outcomes."""

import hashlib
import json
import math
import re
from dataclasses import dataclass, replace
from datetime import UTC, datetime, timedelta
from decimal import Decimal
from typing import Any, TypeVar, cast

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

from ._generated.definitions import (
    ADD_COMMENT_DEFINITION,
    CREATE_ISSUE_DEFINITION,
    GET_ISSUE_DEFINITION,
    SEARCH_ISSUES_DEFINITION,
    TRANSITION_ISSUE_DEFINITION,
)
from .client import (
    DEFINITE_WRITE_REFUSAL_STATUSES,
    Classification,
    Client,
    Exchange,
    SessionError,
    failure,
    path_escape,
    rejected_fields,
    rejection_failure,
)
from .models import (
    AccountReference,
    AddCommentInput,
    AddCommentOutput,
    CreateIssueInput,
    CreateIssueOutput,
    GetIssueInput,
    Issue,
    IssueSearchFilter,
    IssueStatus,
    IssueTypeReference,
    PriorityReference,
    ProjectReference,
    SearchIssuesInput,
    SearchIssuesOutput,
    TransitionIssueInput,
    TransitionIssueOutput,
    TransitionReference,
)

T = TypeVar("T")
_NUMERIC = re.compile(r"[1-9][0-9]{0,17}\Z")
_ISSUE = re.compile(r"[A-Za-z][A-Za-z0-9_]*-[1-9][0-9]{0,17}\Z")
_FIELD = re.compile(r"[A-Za-z][A-Za-z0-9_.-]{0,63}\Z")
_JIRA_TIME = re.compile(
    r"(?P<year>[0-9]{4})-(?P<month>[0-9]{2})-(?P<day>[0-9]{2})T"
    r"(?P<hour>[0-9]{2}):(?P<minute>[0-9]{2}):(?P<second>[0-9]{2})"
    r"(?:(?P<fraction>[.,][0-9]+))?"
    r"(?P<offset>Z|[+-][0-9]{2}:?[0-9]{2})\Z"
)
_FIELDS = (
    "summary",
    "status",
    "issuetype",
    "project",
    "assignee",
    "reporter",
    "priority",
    "labels",
    "created",
    "updated",
)


def _query(branch: str, value: T, receipt: Receipt, reason: Failure | None = None) -> QueryAttempt[T]:
    return QueryAttempt.branch_result(branch, value, receipt, reason)


def _mutation(branch: str, value: T, receipt: Receipt, reason: Failure | None = None) -> MutationAttempt[T]:
    return MutationAttempt.branch_result(branch, value, receipt, reason)


def _session_result(
    error: SessionError, value: T, mutation: bool = False
) -> QueryAttempt[T] | MutationAttempt[T]:
    if error.retryable:
        if mutation:
            return MutationAttempt.retry(error.failure, error.retry_after)
        return QueryAttempt.retry(error.failure, error.retry_after)
    return (
        _mutation("defect", value, Receipt(), error.failure)
        if mutation
        else _query("defect", value, Receipt(), error.failure)
    )


def _identifier(value: str) -> str:
    result = value.strip()
    if not (_NUMERIC.fullmatch(result) or _ISSUE.fullmatch(result)):
        raise ValueError("issueIdOrKey must be an issue key such as OPS-441 or a numeric issue ID")
    return result


def _requested_fields(values: tuple[str, ...], description: bool) -> list[str]:
    if len(values) > 20:
        raise ValueError("additionalFields accepts at most 20 field IDs")
    fields = list(_FIELDS)
    if description:
        fields.append("description")
    for value in values:
        if not _FIELD.fullmatch(value):
            raise ValueError("additionalFields must be Jira field IDs such as customfield_10020 or duedate")
        if value not in fields:
            fields.append(value)
    return fields


def _object(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError("expected object")
    return value


def _array(value: Any) -> list[Any]:
    if not isinstance(value, list):
        raise ValueError("expected array")
    return value


def _string(value: Any) -> str:
    if not isinstance(value, str):
        raise ValueError("expected string")
    return value


def _field_string(value: dict[str, Any], field: str) -> str:
    item = value.get(field, "")
    return "" if item is None else _string(item)


def _exact_json_value(value: Any) -> Any:
    if isinstance(value, Decimal):
        converted = float(value)
        if not math.isfinite(converted) or str(value) != json.dumps(converted):
            raise ValueError("additional field number cannot be encoded exactly")
        return converted
    if isinstance(value, list):
        return [_exact_json_value(item) for item in value]
    if isinstance(value, dict):
        return {key: _exact_json_value(item) for key, item in value.items()}
    return value


def _issue_json(body: bytes) -> Any:
    return json.loads(body, parse_float=Decimal, parse_constant=_reject_json_constant)


def _reject_json_constant(value: str) -> Any:
    raise ValueError(f"invalid JSON constant {value}")


def _equal_fold(left: str, right: str) -> bool:
    return len(left) == len(right) and left.casefold() == right.casefold()


def _timestamp(value: str) -> datetime:
    if not value:
        return datetime.min.replace(tzinfo=UTC)
    try:
        match = _JIRA_TIME.fullmatch(value)
        if match is None:
            raise ValueError("invalid format")
        offset = match["offset"]
        fraction = match["fraction"]
        if fraction and len(fraction[1:]) > 6 and fraction[7:].strip("0"):
            raise ValueError("Jira timestamp cannot be represented exactly as datetime")
        microseconds = int(((fraction or "")[1:] + "000000")[:6]) if fraction else 0
        parsed = datetime(
            int(match["year"]),
            int(match["month"]),
            int(match["day"]),
            int(match["hour"]),
            int(match["minute"]),
            int(match["second"]),
            microseconds,
            tzinfo=UTC,
        )
        if offset == "Z":
            return parsed
        sign = 1 if offset[0] == "+" else -1
        hours, minutes = int(offset[1:3]), int(offset[-2:])
        if minutes > 59:
            raise ValueError("invalid offset")
        return parsed - sign * timedelta(hours=hours, minutes=minutes)
    except (ValueError, OverflowError):
        raise ValueError("timestamp is not a Jira date-time") from None


def _status(value: Any) -> IssueStatus:
    if value is None:
        return IssueStatus()
    item = _object(value)
    category = item.get("statusCategory")
    return IssueStatus(
        _field_string(item, "id"),
        _field_string(item, "name"),
        _field_string(_object(category), "key") if category is not None else None,
    )


def _account(value: Any) -> AccountReference | None:
    if value is None:
        return None
    item = _object(value)
    account_id = _field_string(item, "accountId")
    return AccountReference(account_id, _field_string(item, "displayName")) if account_id else None


def _document_text(value: Any) -> tuple[str, bool]:
    if value is None:
        return "", False
    if isinstance(value, str):
        return value[:32767], len(value) > 32767
    document = _object(value)

    def walk(nodes: Any, separator: str = "\n", depth: int = 0) -> str:
        if depth > 48:
            return ""
        parts: list[str] = []
        for raw in _array(nodes):
            node = _object(raw)
            kind = _field_string(node, "type")
            if kind == "text":
                parts.append(_field_string(node, "text"))
            elif kind == "hardBreak":
                parts.append("\n")
            elif kind in ("mention", "emoji", "status", "inlineCard", "blockCard", "embedCard"):
                attrs = _object(node.get("attrs") or {})
                parts.append(_field_string(attrs, "url" if "Card" in kind else "text"))
            elif kind == "rule":
                parts.append("---")
            elif kind in ("paragraph", "heading", "codeBlock"):
                parts.append(walk(node.get("content") or [], "", depth + 1))
            else:
                parts.append(walk(node.get("content") or [], "\n", depth + 1))
        return separator.join(parts)

    text = walk(document.get("content") or [], "\n\n").rstrip("\n")
    return text[:32767], len(text) > 32767


def _issue_resource(raw: Any, additional: tuple[str, ...], description: bool) -> Issue:
    item = _object(raw)
    issue_id, key = _field_string(item, "id"), _field_string(item, "key")
    if not _NUMERIC.fullmatch(issue_id) or not _ISSUE.fullmatch(key):
        raise ValueError("issue ID or key is invalid")
    fields = _object(item.get("fields") or {})
    issue_type = _object(fields["issuetype"]) if fields.get("issuetype") is not None else {}
    project = _object(fields["project"]) if fields.get("project") is not None else {}
    priority = _object(fields["priority"]) if fields.get("priority") is not None else None
    labels = _array(fields.get("labels") or [])
    if any(not isinstance(label, str) for label in labels):
        raise ValueError("invalid labels")
    subtask = issue_type.get("subtask", False)
    if type(subtask) is not bool:
        raise ValueError("invalid issue type")
    description_text, truncated = _document_text(fields.get("description")) if description else ("", False)
    return Issue(
        id=issue_id,
        key=key,
        summary=_field_string(fields, "summary"),
        status=_status(fields.get("status")),
        issue_type=IssueTypeReference(
            _field_string(issue_type, "id"), _field_string(issue_type, "name"), subtask
        ),
        project=ProjectReference(
            _field_string(project, "id"), _field_string(project, "key"), _field_string(project, "name")
        ),
        assignee=_account(fields.get("assignee")),
        reporter=_account(fields.get("reporter")),
        priority=PriorityReference(_field_string(priority, "id"), _field_string(priority, "name"))
        if priority is not None
        else None,
        labels=tuple(labels),
        description=description_text,
        is_description_truncated=truncated,
        created_at=_timestamp(_field_string(fields, "created")),
        updated_at=_timestamp(_field_string(fields, "updated")),
        additional_fields={name: _exact_json_value(fields[name]) for name in additional if name in fields},
    )


def _read_result(
    client: Client,
    call: Call,
    operation: str,
    subject: str,
    response: Exchange,
    empty: T,
    object_id: str = "",
    mutation: bool = False,
) -> QueryAttempt[T] | MutationAttempt[T] | None:
    classified = client.classify_read(operation, subject, response)
    if classified.outcome == "success":
        return None
    if classified.outcome == "retry":
        assert classified.failure is not None
        if mutation:
            return MutationAttempt.retry(classified.failure, classified.retry_after)
        return QueryAttempt.retry(classified.failure, classified.retry_after)
    receipt = client.receipt(call, response, object_id)
    if mutation:
        return _mutation(classified.outcome, empty, receipt, classified.failure)
    return _query(classified.outcome, empty, receipt, classified.failure)


class GetIssueOperation:
    """Read one Jira issue by key or numeric ID."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated getIssue definition."""
        return GET_ISSUE_DEFINITION

    async def invoke(self, call: Call, input: GetIssueInput) -> QueryAttempt[Issue]:
        """Read and decode an issue, retrying transient failures."""
        try:
            issue_id = _identifier(input.issue_id_or_key)
            fields = _requested_fields(input.additional_fields, True)
        except ValueError as error:
            return _query(
                "defect", Issue(), Receipt(), failure("getIssue", FailureKind.VALIDATION, str(error))
            )
        try:
            credentials, cloud_id = await self.client.session(call, "getIssue")
        except SessionError as error:
            return cast(QueryAttempt[Issue], _session_result(error, Issue()))
        response = await self.client.request(
            call,
            credentials,
            "GET",
            "/issue/" + path_escape(issue_id),
            cloud_id=cloud_id,
            query={"fields": ",".join(fields)},
        )
        result = _read_result(self.client, call, "getIssue", "issue", response, Issue(), issue_id)
        if result is not None:
            return cast(QueryAttempt[Issue], result)
        receipt = self.client.receipt(call, response, issue_id)
        try:
            issue = _issue_resource(_issue_json(response.body), input.additional_fields, True)
        except (ValueError, TypeError, KeyError):
            return _query(
                "invalidResponse",
                Issue(),
                receipt,
                failure("getIssue", FailureKind.PROTOCOL, "Jira returned an invalid issue"),
            )
        return _query("found", issue, replace(receipt, provider_object_id=issue.key))


def _quote_jql(value: str) -> str:
    if len(value) > 1024 or any(ord(char) < 32 or ord(char) == 127 for char in value):
        raise ValueError("JQL value is invalid")
    return '"' + value.replace("\\", "\\\\").replace('"', '\\"') + '"'


def _filter_jql(search: IssueSearchFilter) -> str:
    if isinstance(search, dict):
        search = IssueSearchFilter(
            project_keys=tuple(search.get("projectKeys", ())),
            issue_type_names=tuple(search.get("issueTypeNames", ())),
            status_names=tuple(search.get("statusNames", ())),
            status_categories=tuple(search.get("statusCategories", ())),
            assignee_account_ids=tuple(search.get("assigneeAccountIds", ())),
            includes_unassigned=search.get("includesUnassigned", False),
            labels=tuple(search.get("labels", ())),
            summary_phrase=search.get("summaryPhrase", ""),
            order=search.get("order"),
        )
    clauses: list[str] = []
    for field, values in (
        ("project", search.project_keys),
        ("issuetype", search.issue_type_names),
        ("status", search.status_names),
        ("labels", search.labels),
    ):
        if len(values) > 50:
            raise ValueError("filter accepts at most 50 values")
        if values:
            clauses.append(field + " in (" + ", ".join(_quote_jql(value) for value in values) + ")")
    if search.status_categories:
        categories = {"new": "2", "indeterminate": "4", "done": "3"}
        clauses.append(
            "statusCategory in (" + ", ".join(categories[value] for value in search.status_categories) + ")"
        )
    if search.assignee_account_ids or search.includes_unassigned:
        assignees = []
        if search.assignee_account_ids:
            assignees.append(
                "assignee in (" + ", ".join(_quote_jql(value) for value in search.assignee_account_ids) + ")"
            )
        if search.includes_unassigned:
            assignees.append("assignee is EMPTY")
        clauses.append("(" + " OR ".join(assignees) + ")" if len(assignees) > 1 else assignees[0])
    if search.summary_phrase:
        clauses.append("summary ~ " + _quote_jql(_quote_jql(search.summary_phrase.strip())))
    if not clauses:
        raise ValueError("filter must set at least one bounded search field")
    order = {
        None: "created DESC",
        "createdDescending": "created DESC",
        "updatedDescending": "updated DESC",
        "createdAscending": "created ASC",
    }.get(search.order)
    if order is None:
        raise ValueError("filter order is invalid")
    return " AND ".join(clauses) + " ORDER BY " + order


def _search_body(input: SearchIssuesInput) -> dict[str, Any]:
    if input.filter is not None and input.jql.strip():
        raise ValueError("set either filter or jql, not both")
    jql = _filter_jql(input.filter) if input.filter is not None else input.jql.strip()
    if not jql or len(jql) > 16384 or "\x00" in jql:
        raise ValueError("jql is required and must be bounded")
    fields = _requested_fields(input.additional_fields, False)
    size = input.page_size or 50
    if not 1 <= size <= 100:
        raise ValueError("pageSize must be between 1 and 100")
    token = input.next_page_token.strip()
    if len(token.encode()) > 4096 or (
        token and (not token.isascii() or any(not 33 <= ord(c) <= 126 for c in token))
    ):
        raise ValueError("nextPageToken must be the printable token from a previous search page")
    body: dict[str, Any] = {"jql": jql, "fields": fields, "maxResults": size}
    if token:
        body["nextPageToken"] = token
    return body


class SearchIssuesOperation:
    """Read one bounded enhanced search page."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated searchIssues definition."""
        return SEARCH_ISSUES_DEFINITION

    async def invoke(self, call: Call, input: SearchIssuesInput) -> QueryAttempt[SearchIssuesOutput]:
        """Search Jira using escaped fields or a caller-built bounded JQL query."""
        try:
            body = _search_body(input)
        except (ValueError, KeyError) as error:
            return _query(
                "defect",
                SearchIssuesOutput(),
                Receipt(),
                failure("searchIssues", FailureKind.VALIDATION, str(error)),
            )
        try:
            credentials, cloud_id = await self.client.session(call, "searchIssues")
        except SessionError as error:
            return cast(QueryAttempt[SearchIssuesOutput], _session_result(error, SearchIssuesOutput()))
        response = await self.client.request(
            call, credentials, "POST", "/search/jql", cloud_id=cloud_id, payload=body
        )
        output = SearchIssuesOutput(jql=body["jql"])
        result = _read_result(self.client, call, "searchIssues", "issue search", response, output)
        if result is not None:
            return cast(QueryAttempt[SearchIssuesOutput], result)
        receipt = self.client.receipt(call, response)
        try:
            page = _object(_issue_json(response.body))
            token = _field_string(page, "nextPageToken")
            if len(token.encode()) > 4096:
                raise ValueError("invalid page token")
            raw_issues = page.get("issues")
            issues = tuple(
                _issue_resource(value, input.additional_fields, False)
                for value in _array(raw_issues if raw_issues is not None else [])
            )
        except (ValueError, TypeError, KeyError):
            return _query(
                "invalidResponse",
                output,
                receipt,
                failure("searchIssues", FailureKind.PROTOCOL, "Jira returned an invalid search page"),
            )
        return _query("searched", SearchIssuesOutput(body["jql"], issues, token), receipt)


def _line(value: str, name: str, maximum: int, required: bool = True) -> str:
    trimmed = value.strip()
    if required and not trimmed:
        raise ValueError(name + " is required")
    if len(trimmed) > maximum:
        raise ValueError(f"{name} cannot exceed {maximum} characters")
    if any(ord(character) < 32 or ord(character) == 127 for character in trimmed):
        raise ValueError(name + " must be one line without control characters")
    return trimmed


def _plain(value: str, name: str, required: bool) -> None:
    if required and not value.strip():
        raise ValueError(name + " is required")
    if len(value) > 32767:
        raise ValueError(name + " cannot exceed 32767 characters")
    if any(ord(character) == 0 or (ord(character) < 32 and character not in "\r\n\t") for character in value):
        raise ValueError(name + " cannot contain control characters")


def _document(value: str) -> dict[str, Any]:
    normalized = value.replace("\r\n", "\n").replace("\r", "\n").strip("\n")
    paragraphs = []
    for paragraph in re.split(r"\n[ \t]*\n+", normalized):
        nodes: list[dict[str, str]] = []
        for index, line in enumerate(paragraph.split("\n")):
            if index:
                nodes.append({"type": "hardBreak"})
            if line:
                nodes.append({"type": "text", "text": line})
        if nodes:
            paragraphs.append({"type": "paragraph", "content": nodes})
    result: dict[str, Any] = {"type": "doc", "version": 1}
    if paragraphs:
        result["content"] = paragraphs
    return result


async def _transition_snapshot(
    client: Client, call: Call, credentials: Any, cloud_id: str, issue_id: str
) -> tuple[
    tuple[str, str, IssueStatus, tuple[TransitionReference, ...]] | None,
    MutationAttempt[TransitionIssueOutput] | None,
]:
    response = await client.request(
        call,
        credentials,
        "GET",
        "/issue/" + path_escape(issue_id),
        cloud_id=cloud_id,
        query={"fields": "status", "expand": "transitions"},
    )
    classified = _classify_definite_transition_read(client, response)
    if classified.outcome == "retry":
        assert classified.failure is not None
        return None, MutationAttempt.retry(classified.failure, classified.retry_after)
    if classified.outcome != "success":
        receipt = client.receipt(call, response, issue_id)
        return None, _mutation(classified.outcome, TransitionIssueOutput(), receipt, classified.failure)
    try:
        source = _object(json.loads(response.body))
        found_id, key = _field_string(source, "id"), _field_string(source, "key")
        fields = _object(source.get("fields") or {})
        if not _NUMERIC.fullmatch(found_id) or not _ISSUE.fullmatch(key) or fields.get("status") is None:
            raise ValueError("invalid issue")
        status = _status(fields["status"])
        transitions: list[TransitionReference] = []
        for value in _array(source.get("transitions") or []):
            item = _object(value)
            if _field_string(item, "id") and item.get("to") is not None and len(transitions) < 50:
                transitions.append(
                    TransitionReference(
                        _field_string(item, "id"), _field_string(item, "name"), _status(item["to"])
                    )
                )
        return (found_id, key, status, tuple(transitions)), None
    except (ValueError, TypeError, KeyError):
        return None, MutationAttempt.retry(
            failure(
                "transitionIssue", FailureKind.PROTOCOL, "Jira returned an invalid issue or transition list"
            )
        )


def _classify_definite_transition_read(client: Client, response: Exchange) -> Classification:
    """Keep a branch only for a complete 404 or definite refusal; other unusable reads Retry."""
    classified = client.classify_read("transitionIssue", "issue", response)
    if classified.outcome in ("success", "retry", "defect"):
        return classified
    if classified.outcome in ("notFound", "providerRejected"):
        if response.is_complete and response.status in DEFINITE_WRITE_REFUSAL_STATUSES:
            return classified
        return Classification(
            "retry",
            failure(
                "transitionIssue",
                FailureKind.AVAILABILITY,
                f"Jira answered the issue read with HTTP {response.status}, "
                "which proves nothing about the issue",
            ),
            client.retry_after(response),
        )
    return Classification("retry", classified.failure)


@dataclass(frozen=True)
class _TransitionCheckpoint:
    """The transition an attempt recorded before sending it; Go and TypeScript encode the same JSON."""

    transition_id: str
    transition_name: str
    source_status_id: str
    destination_status_id: str


@dataclass(frozen=True)
class _TransitionHistory:
    """What the last heartbeat proves about earlier attempts of this Call."""

    plan: _TransitionCheckpoint | None
    can_select: bool
    """No earlier attempt can have sent a transition."""

    async def record_not_sent(self, call: Call) -> None:
        """Let the next attempt select after a Retry before any send; never overwrite a possible send."""
        if not self.can_select or self.plan is not None or call.context is None:
            return
        try:
            await call.context.heartbeat(
                {"callId": str(call.id), "attempt": call.context.attempt, "state": "notSent"}
            )
        except Exception:
            pass  # A lost record only makes the next attempt read without transitioning.


def _read_transition_history(call: Call) -> _TransitionHistory:
    """Read this Call's state from the last heartbeat; raise when the heartbeat is unreadable.

    Dex can lose a recorded heartbeat, so a later attempt may select only after this Call's notSent
    record from the previous attempt, including an earlier release's notSent or canResend marker.
    """
    context = call.context
    assert context is not None
    attempt = context.attempt
    fresh = _TransitionHistory(None, attempt == 1)
    if not context.has_last_heartbeat_value():
        return fresh
    value = context.get_last_heartbeat_value(dict[str, Any])
    if not isinstance(value, dict):
        return fresh
    call_id = str(call.id)
    transition_id, transition_name = value.get("transitionId"), value.get("transitionName", "")
    source_status_id, destination_status_id = value.get("sourceStatusId"), value.get("destinationStatusId")
    identifiers = (transition_id, source_status_id, destination_status_id)
    if (
        "state" not in value
        and value.get("callId") == call_id
        and all(isinstance(item, str) and item for item in identifiers)
        and isinstance(transition_name, str)
    ):
        plan = _TransitionCheckpoint(
            str(transition_id), transition_name, str(source_status_id), str(destination_status_id)
        )
        return _TransitionHistory(plan, False)
    markers = value.get("singleDispatchMarkers")
    marker = value if "state" in value else markers.get(call_id) if isinstance(markers, dict) else None
    can_select = (
        isinstance(marker, dict)
        and marker.get("callId") == call_id
        and marker.get("attempt") == attempt - 1
        and not marker.get("providerId")
        and marker.get("state") in ("notSent", "canResend")
    )
    return _TransitionHistory(None, can_select or attempt == 1)


def _transition_checkpoint_heartbeat(
    call: Call, attempt: int, chosen: TransitionReference, source_status_id: str
) -> dict[str, Any]:
    """Encode the checkpoint exactly as the Go connector does."""
    return {
        "callId": str(call.id),
        "attempt": attempt,
        "transitionId": chosen.id,
        "transitionName": chosen.name,
        "sourceStatusId": source_status_id,
        "destinationStatusId": chosen.destination_status.id,
    }


class TransitionIssueOperation:
    """Move one issue through an available Jira transition."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated transitionIssue definition."""
        return TRANSITION_ISSUE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: TransitionIssueInput) -> IdempotencyKey:
        """Use the stable Call ID in the receipt without sending it to Jira."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: TransitionIssueInput) -> MutationAttempt[TransitionIssueOutput]:
        """Read transitions, send one move, and read back ambiguous outcomes."""
        try:
            issue_id = _identifier(input.issue_id_or_key)
            transition_id = input.transition_id.strip()
            if transition_id and not _NUMERIC.fullmatch(transition_id):
                raise ValueError("transitionId must be a numeric transition ID")
            transition_name = _line(input.transition_name, "transitionName", 255, False)
            destination = _line(input.destination_status_name, "destinationStatusName", 255, False)
            resolution = _line(input.resolution_name, "resolutionName", 255, False)
            if not (transition_id or transition_name or destination):
                raise ValueError("set transitionId, transitionName, or destinationStatusName")
        except ValueError as error:
            return _mutation(
                "defect",
                TransitionIssueOutput(),
                Receipt(),
                failure("transitionIssue", FailureKind.VALIDATION, str(error)),
            )
        if call.context is None:
            return _mutation(
                "defect",
                TransitionIssueOutput(),
                Receipt(),
                failure(
                    "transitionIssue", FailureKind.LOCAL_DEFECT, "Jira transition requires a Dex Step context"
                ),
            )
        try:
            history = _read_transition_history(call)
        except Exception:
            return MutationAttempt.retry(
                failure(
                    "transitionIssue",
                    FailureKind.AVAILABILITY,
                    "Jira transition checkpoint is unreadable, so nothing was sent in this attempt",
                )
            )
        try:
            credentials, cloud_id = await self.client.session(call, "transitionIssue")
        except SessionError as error:
            # After a possible earlier transition, a credential failure is no proof that nothing moved.
            if error.retryable or not history.can_select:
                await history.record_not_sent(call)
                return MutationAttempt.retry(error.failure, error.retry_after)
            return _mutation("defect", TransitionIssueOutput(), Receipt(), error.failure)
        snapshot, early = await _transition_snapshot(self.client, call, credentials, cloud_id, issue_id)
        if early is not None:
            if early.kind == "retry":
                await history.record_not_sent(call)
            # After a possible earlier transition, a local failure is no proof that nothing moved.
            if early.branch == "defect" and not history.can_select:
                assert early.failure is not None
                return MutationAttempt.retry(early.failure)
            return early
        assert snapshot is not None
        found_id, key, status, available = snapshot
        receipt = self.client.receipt(call, object_id=key)
        output = TransitionIssueOutput(
            issue_id=found_id, issue_key=key, previous_status=status, status=status
        )
        send = _TransitionSend(self.client, call, credentials, cloud_id, issue_id, resolution)
        if history.plan is not None:
            return await send.resume_recorded(history.plan, status, available, output, receipt)
        if destination and _equal_fold(status.name, destination):
            return _mutation("transitioned", replace(output, is_already_in_destination_status=True), receipt)
        matches = [
            item
            for item in available
            if (not transition_id or item.id == transition_id)
            and (not transition_name or _equal_fold(item.name.strip(), transition_name))
            and (not destination or _equal_fold(item.destination_status.name, destination))
        ]
        if len(matches) == 1 and matches[0].destination_status.id == status.id:
            return _mutation(
                "transitioned",
                replace(output, transition=matches[0], is_already_in_destination_status=True),
                receipt,
            )
        if not history.can_select:
            return MutationAttempt.retry(
                failure("transitionIssue", FailureKind.AVAILABILITY, _UNPROVEN_EARLIER_TRANSITION)
            )
        if len(matches) != 1:
            kind = FailureKind.CONFLICT if not matches else FailureKind.VALIDATION
            message = (
                f"no transition available from status {status.id} matches the request"
                if not matches
                else "several transitions match the request"
            )
            return _mutation(
                "transitionUnavailable",
                replace(output, available_transitions=available),
                receipt,
                failure("transitionIssue", kind, message),
            )
        chosen = matches[0]
        try:
            await call.context.heartbeat(
                _transition_checkpoint_heartbeat(call, call.context.attempt, chosen, status.id)
            )
        except Exception:
            await history.record_not_sent(call)
            return MutationAttempt.retry(
                failure(
                    "transitionIssue",
                    FailureKind.AVAILABILITY,
                    "Jira transition checkpoint could not be recorded, so the issue was not transitioned",
                )
            )
        return await send.send(chosen, output)


_UNPROVEN_EARLIER_TRANSITION = (
    "an earlier attempt may have transitioned the issue and its transition checkpoint is missing, "
    "so no transition is selected or sent; the Step retries until its budget ends"
)


class _TransitionSend:
    """Sends one Jira transition for an attempt and classifies the answer."""

    def __init__(
        self, client: Client, call: Call, credentials: Any, cloud_id: str, issue_id: str, resolution: str
    ) -> None:
        self.client, self.call, self.credentials = client, call, credentials
        self.cloud_id, self.issue_id, self.resolution = cloud_id, issue_id, resolution

    async def resume_recorded(
        self,
        checkpoint: _TransitionCheckpoint,
        status: IssueStatus,
        available: tuple[TransitionReference, ...],
        output: TransitionIssueOutput,
        receipt: Receipt,
    ) -> MutationAttempt[TransitionIssueOutput]:
        """Continue the transition an earlier attempt of this Call recorded."""
        if status.id == checkpoint.destination_status_id:
            recorded = TransitionReference(checkpoint.transition_id, checkpoint.transition_name, status)
            return _mutation(
                "transitioned",
                replace(output, transition=recorded, is_already_in_destination_status=True),
                receipt,
            )
        unavailable = replace(output, available_transitions=available)
        if status.id != checkpoint.source_status_id:
            return _mutation(
                "transitionUnavailable",
                unavailable,
                receipt,
                failure(
                    "transitionIssue",
                    FailureKind.CONFLICT,
                    f"the issue moved to status {status.id}, which is neither the source nor the destination "
                    f"of transition {checkpoint.transition_id}, so it was not sent again",
                ),
            )
        for transition in available:
            if (
                transition.id == checkpoint.transition_id
                and transition.destination_status.id == checkpoint.destination_status_id
            ):
                return await self.send(transition, output)
        return _mutation(
            "transitionUnavailable",
            unavailable,
            receipt,
            failure(
                "transitionIssue",
                FailureKind.CONFLICT,
                f"transition {checkpoint.transition_id} is no longer available from status {status.id}",
            ),
        )

    async def send(
        self, chosen: TransitionReference, output: TransitionIssueOutput
    ) -> MutationAttempt[TransitionIssueOutput]:
        """Post the chosen transition once in this attempt and classify Jira's answer."""
        output = replace(output, transition=chosen)
        payload: dict[str, Any] = {"transition": {"id": chosen.id}}
        if self.resolution:
            payload["fields"] = {"resolution": {"name": self.resolution}}
        path = "/issue/" + path_escape(self.issue_id) + "/transitions"
        # A rejected token refreshes and resends once: a 401 proves Jira did not apply the transition.
        response = await self.client.request(
            self.call, self.credentials, "POST", path, cloud_id=self.cloud_id, payload=payload
        )
        receipt = self.client.receipt(self.call, response, output.issue_key)
        if response.request_defect:
            return _mutation(
                "defect",
                output,
                receipt,
                failure("transitionIssue", FailureKind.LOCAL_DEFECT, "Jira request could not be built"),
            )
        if not response.transport_error and 200 <= response.status < 300:
            return _mutation("transitioned", replace(output, status=chosen.destination_status), receipt)
        if response.status == 404 and response.is_complete:
            return _mutation(
                "notFound",
                output,
                receipt,
                failure(
                    "transitionIssue",
                    FailureKind.NOT_FOUND,
                    "Jira issue was not found or is not visible to the connection",
                ),
            )
        if not response.transport_error and response.status == 429:
            return MutationAttempt.retry(
                failure(
                    "transitionIssue",
                    FailureKind.RATE_LIMIT,
                    "Jira rate limited the transition before performing it",
                ),
                self.client.retry_after(response),
            )
        if not response.transport_error and response.status == 503:
            return MutationAttempt.retry(
                failure(
                    "transitionIssue",
                    FailureKind.AVAILABILITY,
                    "Jira transitions are temporarily unavailable",
                ),
                self.client.retry_after(response),
            )
        if response.transport_error and response.not_sent:
            return MutationAttempt.retry(
                failure(
                    "transitionIssue",
                    FailureKind.TRANSPORT,
                    "Jira could not be reached, so the issue was not transitioned",
                ),
                0,
            )
        if response.is_complete and response.status in DEFINITE_WRITE_REFUSAL_STATUSES:
            return await self.classify_refused(chosen, output, response, receipt)
        if not response.transport_error and response.status < 400:
            return MutationAttempt.retry(
                failure(
                    "transitionIssue",
                    FailureKind.PROTOCOL,
                    f"Jira returned unexpected HTTP {response.status} for the transition",
                )
            )
        if not response.transport_error and response.status == 409:
            return MutationAttempt.retry(
                failure(
                    "transitionIssue",
                    FailureKind.CONFLICT,
                    "Jira reported a conflicting update; the outcome of the transition is unknown",
                ),
                self.client.retry_after(response),
            )
        return MutationAttempt.retry(
            failure(
                "transitionIssue",
                FailureKind.AVAILABILITY,
                "Jira did not confirm the transition; "
                "the next attempt reads the issue before sending it again",
            ),
            self.client.retry_after(response),
        )

    async def classify_refused(
        self, chosen: TransitionReference, output: TransitionIssueOutput, response: Exchange, receipt: Receipt
    ) -> MutationAttempt[TransitionIssueOutput]:
        """Reread the issue, because a concurrent duplicate may have made the transition first."""
        reread, read_failure = await _transition_snapshot(
            self.client, self.call, self.credentials, self.cloud_id, self.issue_id
        )
        if read_failure is not None or reread is None:
            return MutationAttempt.retry(
                failure(
                    "transitionIssue",
                    FailureKind.AVAILABILITY,
                    "Jira refused the transition and the issue could not be read back",
                )
            )
        if reread[2].id == chosen.destination_status.id:
            return _mutation("transitioned", replace(output, status=reread[2]), receipt)
        fields = rejected_fields(response.body)
        return _mutation(
            "providerRejected",
            replace(output, status=reread[2], rejected_field_ids=fields),
            receipt,
            rejection_failure("transitionIssue", "transition", response.status, fields),
        )


def _marker(call: Call) -> dict[str, str]:
    return {"key": "dex.connector.call", "value": hashlib.sha256(str(call.id).encode()).hexdigest()}


def _readback_status(response: Exchange) -> str:
    if (
        response.transport_error
        or response.body_failed
        or response.status in (408, 429)
        or response.status >= 500
    ):
        return "retry"
    return (
        "found"
        if response.status == 200 and not (response.too_large or response.reflects_secret)
        else "missing"
    )


def _matches_property(value: Any, expected: str) -> bool:
    if isinstance(value, str):
        return _equal_fold(value, expected)
    return (
        isinstance(value, dict)
        and isinstance(value.get("value"), str)
        and _equal_fold(value["value"], expected)
    )


async def _property_status(
    client: Client, call: Call, credentials: Any, cloud_id: str, path: str, expected: str
) -> str:
    response = await client.request(call, credentials, "GET", path, cloud_id=cloud_id)
    status = _readback_status(response)
    if status != "found":
        return status
    try:
        return "found" if _matches_property(json.loads(response.body), expected) else "missing"
    except ValueError:
        return "missing"


async def _find_created(
    client: Client,
    call: Call,
    credentials: Any,
    cloud_id: str,
    requested: CreateIssueOutput,
    marker: dict[str, str],
    provider_id: str,
) -> tuple[dict[str, Any] | None, str]:
    if provider_id:
        response = await client.request(
            call,
            credentials,
            "GET",
            "/issue/" + path_escape(provider_id),
            cloud_id=cloud_id,
            query={"fields": "summary", "properties": "dex.connector.call"},
        )
    else:
        project = requested.project_id or _quote_jql(requested.project_key)
        quoted_summary = _quote_jql(_quote_jql(requested.summary))
        body = {
            "jql": f"project = {project} AND summary ~ {quoted_summary} ORDER BY created DESC",
            "fields": ["summary"],
            "properties": ["dex.connector.call"],
            "maxResults": 50,
        }
        response = await client.request(
            call, credentials, "POST", "/search/jql", cloud_id=cloud_id, payload=body
        )
    status = _readback_status(response)
    if status != "found":
        return None, status
    try:
        document = _object(json.loads(response.body))
        candidates = [document] if provider_id else _array(document.get("issues") or [])
        for value in candidates:
            candidate = _object(value)
            issue_id, key = _field_string(candidate, "id"), _field_string(candidate, "key")
            if (
                (provider_id and issue_id != provider_id)
                or not _NUMERIC.fullmatch(issue_id)
                or not _ISSUE.fullmatch(key)
            ):
                continue
            if _object(candidate.get("fields") or {}).get("summary") != requested.summary:
                continue
            properties = _object(candidate.get("properties") or {})
            if "dex.connector.call" in properties:
                if _matches_property(properties["dex.connector.call"], marker["value"]):
                    return candidate, "found"
                continue
            status = await _property_status(
                client,
                call,
                credentials,
                cloud_id,
                "/issue/" + path_escape(issue_id) + "/properties/dex.connector.call",
                marker["value"],
            )
            if status == "found":
                return candidate, "found"
            if status == "retry":
                return None, "retry"
    except (ValueError, TypeError, KeyError):
        return None, "missing"
    return None, "missing"


async def _find_comment(
    client: Client,
    call: Call,
    credentials: Any,
    cloud_id: str,
    issue_id: str,
    marker: dict[str, str],
    provider_id: str,
) -> tuple[dict[str, Any] | None, str]:
    path = "/issue/" + path_escape(issue_id) + "/comment"
    if provider_id:
        response = await client.request(
            call,
            credentials,
            "GET",
            path + "/" + path_escape(provider_id),
            cloud_id=cloud_id,
            query={"expand": "properties"},
        )
    else:
        response = await client.request(
            call,
            credentials,
            "GET",
            path,
            cloud_id=cloud_id,
            query={"maxResults": "50", "orderBy": "-created", "expand": "properties"},
        )
    status = _readback_status(response)
    if status != "found":
        return None, status
    try:
        document = _object(json.loads(response.body))
        candidates = [document] if provider_id else _array(document.get("comments") or [])
        for value in candidates:
            candidate = _object(value)
            comment_id = _field_string(candidate, "id")
            if (provider_id and comment_id != provider_id) or not _NUMERIC.fullmatch(comment_id):
                continue
            properties = _array(candidate.get("properties") or [])
            if properties:
                if any(
                    _object(item).get("key") == marker["key"]
                    and _matches_property(_object(item).get("value"), marker["value"])
                    for item in properties
                ):
                    return candidate, "found"
                continue
            status = await _property_status(
                client,
                call,
                credentials,
                cloud_id,
                "/comment/" + path_escape(comment_id) + "/properties/dex.connector.call",
                marker["value"],
            )
            if status == "found":
                return candidate, "found"
            if status == "retry":
                return None, "retry"
    except (ValueError, TypeError, KeyError):
        return None, "missing"
    return None, "missing"


async def _refresh_after_rejection(client: Client, call: Call) -> bool:
    if not hasattr(client.credentials, "resolve_after_rejection"):
        return False
    try:
        replacement = await client.credentials.resolve_after_rejection(call)
        return bool(replacement.access_token.reveal())
    except Exception:
        return False


class _WriteDispatch:
    """One immutable fenced request callback and its explicit dependencies."""

    def __init__(
        self, client: Client, call: Call, credentials: Any, cloud_id: str, path: str, payload: dict[str, Any]
    ) -> None:
        self.client, self.call, self.credentials = client, call, credentials
        self.cloud_id, self.path, self.payload = cloud_id, path, payload
        self.observed_statuses: list[int] = []
        """Statuses seen when headers arrived, kept when the write window cuts the body read."""

    async def send(self, deadline: datetime) -> Exchange:
        """Send once after Fence has persisted the claim."""
        return await self.client.request(
            self.call,
            self.credentials,
            "POST",
            self.path,
            cloud_id=self.cloud_id,
            payload=self.payload,
            fenced=True,
            observed_statuses=self.observed_statuses,
        )

    def has_observed_success(self) -> bool:
        """Report that Jira answered 2xx before the write window expired."""
        return bool(self.observed_statuses) and 200 <= self.observed_statuses[-1] < 300


@dataclass(frozen=True)
class _SendCapPolicy:
    """One fenced attempt's send-cap facts from its Fence decision."""

    can_resend_unknown_outcome: bool = False
    """Another send stays within the cap."""
    has_possible_earlier_send: bool = False
    """An earlier send of this call may have taken effect."""
    has_uncheckpointed_acceptance: bool = False
    """Jira accepted this send without a recorded provider ID, so reconciliation never retries."""


def _accepted_write_policy(has_recorded_provider_id: bool) -> _SendCapPolicy:
    """Reconcile a write Jira accepted in this attempt; it is never sent again."""
    return _SendCapPolicy(
        has_possible_earlier_send=True, has_uncheckpointed_acceptance=not has_recorded_provider_id
    )


def _needs_read_back(decision: Decision) -> bool:
    """Report a possible earlier send, including SEND_ALLOWED after a canResend marker."""
    return (
        decision.kind != DecisionKind.SEND_ALLOWED
        or decision.prior_send_upper_bound > 0
        or bool(decision.marker and decision.marker.provider_id)
    )


def _resend_or_select_uncertain(
    policy: _SendCapPolicy, value: T, receipt: Receipt, cause: Failure
) -> MutationAttempt[T]:
    if policy.can_resend_unknown_outcome:
        # The claim stays the last marker, so the next attempt counts this send.
        return MutationAttempt.retry(cause)
    return MutationAttempt.uncertain(value, cause, receipt)


async def _reject_or_select_uncertain(
    fence: Fence,
    policy: _SendCapPolicy,
    operation: str,
    branch: str,
    value: T,
    receipt: Receipt,
    cause: Failure,
) -> MutationAttempt[T]:
    """Select a refusal branch only when no earlier send of this call may have written."""
    if policy.has_possible_earlier_send:
        return MutationAttempt.uncertain(value, cause, receipt)
    try:
        await fence.record_can_resend()
    except Exception:
        return MutationAttempt.retry(
            failure(operation, FailureKind.AVAILABILITY, "Jira rejection checkpoint could not be recorded")
        )
    return _mutation(branch, value, receipt, cause)


def _defect_or_select_uncertain(
    policy: _SendCapPolicy, value: T, receipt: Receipt, cause: Failure
) -> MutationAttempt[T]:
    if policy.has_possible_earlier_send:
        return MutationAttempt.uncertain(value, cause, receipt)
    return _mutation("defect", value, receipt, cause)


class _FencedWrite:
    """Common single-dispatch and status path for Jira's two unkeyed writes."""

    operation: str
    subject: str

    def __init__(self, client: Client) -> None:
        self.client = client

    def _prepare(self, input: Any) -> tuple[Any, str, dict[str, Any]]:
        raise NotImplementedError

    async def _find(
        self,
        call: Call,
        credentials: Any,
        cloud_id: str,
        requested: Any,
        marker: dict[str, str],
        provider_id: str,
    ) -> tuple[dict[str, Any] | None, str]:
        raise NotImplementedError

    async def _reconcile(
        self,
        call: Call,
        credentials: Any,
        cloud_id: str,
        requested: Any,
        marker: dict[str, str],
        provider_id: str,
        policy: _SendCapPolicy,
        cause: Failure,
        receipt: Receipt,
    ) -> MutationAttempt[Any]:
        """Read back a possible write; a missing write resends while the policy allows.

        A write Jira already identified is never sent again.
        """
        candidate, status = await self._find(call, credentials, cloud_id, requested, marker, provider_id)
        if status == "found" and candidate is not None:
            return self._accepted(requested, candidate, receipt)
        if status == "retry":
            if policy.has_uncheckpointed_acceptance:
                return MutationAttempt.uncertain(requested, cause, receipt)
            return self._read_back_retry()
        if provider_id:
            return MutationAttempt.uncertain(requested, cause, receipt)
        return _resend_or_select_uncertain(policy, requested, receipt, cause)

    async def _find_before_resend(
        self,
        call: Call,
        credentials: Any,
        cloud_id: str,
        requested: Any,
        marker: dict[str, str],
        receipt: Receipt,
    ) -> MutationAttempt[Any] | None:
        """Return the outcome of an earlier send that read-back finds, or None to resend."""
        candidate, status = await self._find(call, credentials, cloud_id, requested, marker, "")
        if status == "found" and candidate is not None:
            return self._accepted(requested, candidate, receipt)
        if status == "retry":
            return self._read_back_retry()
        return None

    def _read_back_retry(self) -> MutationAttempt[Any]:
        return MutationAttempt.retry(
            failure(
                self.operation,
                FailureKind.AVAILABILITY,
                f"Jira {self.subject} read-back is temporarily unavailable",
            )
        )

    def _accepted(self, requested: Any, resource: dict[str, Any], receipt: Receipt) -> MutationAttempt[Any]:
        raise NotImplementedError

    def _rejected(self, requested: Any, fields: tuple[str, ...]) -> Any:
        return replace(requested, rejected_field_ids=fields)

    async def invoke(self, call: Call, input: Any) -> MutationAttempt[Any]:
        """Validate, fence, send once, and read back ambiguous writes."""
        try:
            requested, path, payload = self._prepare(input)
        except ValueError as error:
            return _mutation(
                "defect",
                self._empty(input),
                Receipt(),
                failure(self.operation, FailureKind.VALIDATION, str(error)),
            )
        try:
            fence = Fence(call, timedelta(seconds=30), max_sends=call.max_sends)
            decision = await fence.decide()
        except ValueError:
            return _mutation(
                "defect",
                requested,
                Receipt(),
                failure(
                    self.operation, FailureKind.LOCAL_DEFECT, "Jira dispatch fence could not be constructed"
                ),
            )
        if decision.kind == DecisionKind.FAIL_CLOSED:
            return MutationAttempt.retry(
                failure(
                    self.operation,
                    FailureKind.AVAILABILITY,
                    "Jira dispatch checkpoint is unreadable, so nothing was sent in this attempt",
                )
            )
        policy = _SendCapPolicy(
            can_resend_unknown_outcome=decision.prior_send_upper_bound + 1 < call.max_sends,
            has_possible_earlier_send=decision.prior_send_upper_bound > 0,
        )
        try:
            credentials, cloud_id = await self.client.session(call, self.operation)
        except SessionError as error:
            # notSent after a possible earlier send would hide it, so only a first send records it.
            if not _needs_read_back(decision):
                try:
                    await fence.record_not_sent()
                except Exception:
                    return MutationAttempt.retry(
                        failure(
                            self.operation,
                            FailureKind.AVAILABILITY,
                            "Jira setup failed and the not-sent checkpoint could not be recorded",
                        ),
                        error.retry_after,
                    )
            if error.retryable:
                return MutationAttempt.retry(error.failure, error.retry_after)
            return _defect_or_select_uncertain(policy, requested, Receipt(), error.failure)
        marker = _marker(call)
        receipt = self.client.receipt(call)
        provider_id = decision.marker.provider_id if decision.marker else ""
        earlier_send = failure(
            self.operation, FailureKind.AVAILABILITY, f"Jira {self.subject} dispatch may have succeeded"
        )
        if decision.kind == DecisionKind.READ_BACK_REQUIRED or provider_id:
            return await self._reconcile(
                call,
                credentials,
                cloud_id,
                requested,
                marker,
                provider_id,
                _SendCapPolicy(),
                earlier_send,
                receipt,
            )
        if _needs_read_back(decision):
            found = await self._find_before_resend(call, credentials, cloud_id, requested, marker, receipt)
            if found is not None:
                return found
        payload["properties"] = [marker]
        dispatch = _WriteDispatch(self.client, call, credentials, cloud_id, path, payload)
        try:
            response = await fence.dispatch(dispatch.send)
        except TimeoutError:
            if dispatch.has_observed_success():
                # Jira accepted the write before the window cut its body, so it is never resent.
                return await self._reconcile(
                    call,
                    credentials,
                    cloud_id,
                    requested,
                    marker,
                    "",
                    _accepted_write_policy(False),
                    failure(
                        self.operation,
                        FailureKind.TRANSPORT,
                        f"Jira accepted the {self.subject} but the write deadline cut off its response",
                    ),
                    receipt,
                )
            return await self._reconcile(
                call,
                credentials,
                cloud_id,
                requested,
                marker,
                "",
                policy,
                failure(self.operation, FailureKind.AVAILABILITY, f"Jira {self.subject} dispatch timed out"),
                receipt,
            )
        except (CheckpointError, InsufficientAttemptTimeError):
            return MutationAttempt.retry(
                failure(
                    self.operation,
                    FailureKind.AVAILABILITY,
                    "Jira dispatch checkpoint or request budget is unavailable",
                )
            )
        except (DispatchNotAllowedError, ValueError):
            return _defect_or_select_uncertain(
                policy,
                requested,
                receipt,
                failure(self.operation, FailureKind.LOCAL_DEFECT, "Jira dispatch was not allowed"),
            )
        classified = self.client.classify_write(self.operation, self.subject, response)
        receipt = self.client.receipt(call, response)
        if classified.outcome == "defect":
            assert classified.failure is not None
            return _defect_or_select_uncertain(policy, requested, receipt, classified.failure)
        if classified.outcome == "retry":
            try:
                await fence.record_can_resend()
            except Exception:
                return MutationAttempt.retry(
                    failure(
                        self.operation,
                        FailureKind.AVAILABILITY,
                        "Jira retry checkpoint could not be recorded",
                    ),
                    classified.retry_after,
                )
            assert classified.failure is not None
            return MutationAttempt.retry(classified.failure, classified.retry_after)
        if classified.outcome == "uncertain":
            assert classified.failure is not None
            return await self._reconcile(
                call, credentials, cloud_id, requested, marker, "", policy, classified.failure, receipt
            )
        if classified.outcome == "acceptedUnreadable":
            assert classified.failure is not None
            return await self._reconcile(
                call,
                credentials,
                cloud_id,
                requested,
                marker,
                "",
                _accepted_write_policy(False),
                classified.failure,
                receipt,
            )
        if classified.outcome in ("rejected", "notFound"):
            if response.status == 401 and await _refresh_after_rejection(self.client, call):
                try:
                    await fence.record_can_resend()
                except Exception:
                    return MutationAttempt.retry(
                        failure(
                            self.operation,
                            FailureKind.AVAILABILITY,
                            "Jira credential retry checkpoint could not be recorded",
                        )
                    )
                return MutationAttempt.retry(
                    failure(
                        self.operation,
                        FailureKind.AUTHENTICATION,
                        "Jira access token was refreshed for retry",
                    )
                )
            branch = (
                "notFound"
                if classified.outcome == "notFound" and self.operation == "addComment"
                else "providerRejected"
            )
            assert classified.failure is not None
            return await _reject_or_select_uncertain(
                fence,
                policy,
                self.operation,
                branch,
                self._rejected(requested, classified.rejected_fields),
                receipt,
                classified.failure,
            )
        try:
            resource = _object(json.loads(response.body))
            provider_id = _field_string(resource, "id")
        except (ValueError, TypeError):
            resource, provider_id = {}, ""
        if _NUMERIC.fullmatch(provider_id):
            has_recorded_provider_id = True
            try:
                await fence.record_provider_id(provider_id)
            except Exception:
                # Without the identity checkpoint, reconciliation never retries.
                has_recorded_provider_id = False
            if self.operation == "addComment" or _ISSUE.fullmatch(_field_string(resource, "key")):
                return self._accepted(requested, resource, receipt)
            return await self._reconcile(
                call,
                credentials,
                cloud_id,
                requested,
                marker,
                provider_id,
                _accepted_write_policy(has_recorded_provider_id),
                failure(
                    self.operation,
                    FailureKind.PROTOCOL,
                    "Jira accepted the issue but returned an unusable issue key",
                ),
                receipt,
            )
        return await self._reconcile(
            call,
            credentials,
            cloud_id,
            requested,
            marker,
            "",
            _accepted_write_policy(False),
            failure(
                self.operation,
                FailureKind.PROTOCOL,
                f"Jira accepted the {self.subject} but returned an unusable {self.subject} reference",
            ),
            receipt,
        )

    def _empty(self, input: Any) -> Any:
        raise NotImplementedError


class CreateIssueOperation(_FencedWrite):
    """Create one Jira issue with a durable dispatch fence and property read-back."""

    operation = "createIssue"
    subject = "issue"

    def definition(self) -> MutationDefinition:
        """Return the generated createIssue definition."""
        return CREATE_ISSUE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreateIssueInput) -> IdempotencyKey:
        """Use the stable Call ID for the receipt; Jira receives no idempotency header."""
        return IdempotencyKey(call_id)

    def _empty(self, input: CreateIssueInput) -> CreateIssueOutput:
        return CreateIssueOutput(
            project_key=input.project_key.strip(),
            project_id=input.project_id.strip(),
            summary=input.summary.strip(),
        )

    def _prepare(self, input: CreateIssueInput) -> tuple[CreateIssueOutput, str, dict[str, Any]]:
        requested = self._empty(input)
        if bool(requested.project_key) == bool(requested.project_id):
            raise ValueError("set exactly one of projectKey and projectId")
        if requested.project_key and not re.fullmatch(r"[A-Z][A-Z0-9_]{0,254}", requested.project_key):
            raise ValueError("projectKey must be an uppercase project key such as OPS")
        if requested.project_id and not _NUMERIC.fullmatch(requested.project_id):
            raise ValueError("projectId must be a numeric project ID")
        type_name, type_id = input.issue_type_name.strip(), input.issue_type_id.strip()
        if bool(type_name) == bool(type_id):
            raise ValueError("set exactly one of issueTypeName and issueTypeId")
        if type_id and not _NUMERIC.fullmatch(type_id):
            raise ValueError("issueTypeId must be a numeric issue type ID")
        if type_name:
            type_name = _line(type_name, "issueTypeName", 255)
        summary = _line(input.summary, "summary", 255)
        _plain(input.description, "description", False)
        if len(input.labels) > 50 or any(
            not label
            or len(label) > 255
            or any(character.isspace() or ord(character) < 32 for character in label)
            for label in input.labels
        ):
            raise ValueError("each label must be 1 to 255 characters without whitespace")
        account_id = input.assignee_account_id.strip()
        if account_id and not re.fullmatch(r"[A-Za-z0-9:_-]{1,128}", account_id):
            raise ValueError("assigneeAccountId must be an Atlassian account ID")
        fields: dict[str, Any] = {
            "project": {"key": requested.project_key}
            if requested.project_key
            else {"id": requested.project_id},
            "issuetype": {"name": type_name} if type_name else {"id": type_id},
            "summary": summary,
        }
        if input.description.strip():
            fields["description"] = _document(input.description)
        if input.labels:
            fields["labels"] = list(input.labels)
        if account_id:
            fields["assignee"] = {"accountId": account_id}
        return requested, "/issue", {"fields": fields}

    def _accepted(
        self, requested: CreateIssueOutput, resource: dict[str, Any], receipt: Receipt
    ) -> MutationAttempt[CreateIssueOutput]:
        issue_id, key = _field_string(resource, "id"), _field_string(resource, "key")
        return _mutation(
            "created",
            replace(requested, issue_id=issue_id, issue_key=key),
            replace(receipt, provider_object_id=key),
        )

    async def _find(
        self,
        call: Call,
        credentials: Any,
        cloud_id: str,
        requested: CreateIssueOutput,
        marker: dict[str, str],
        provider_id: str,
    ) -> tuple[dict[str, Any] | None, str]:
        return await _find_created(self.client, call, credentials, cloud_id, requested, marker, provider_id)


class AddCommentOperation(_FencedWrite):
    """Add one Jira comment with a durable dispatch fence and property read-back."""

    operation = "addComment"
    subject = "comment"

    def definition(self) -> MutationDefinition:
        """Return the generated addComment definition."""
        return ADD_COMMENT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: AddCommentInput) -> IdempotencyKey:
        """Use the stable Call ID for the receipt; Jira receives no idempotency header."""
        return IdempotencyKey(call_id)

    def _empty(self, input: AddCommentInput) -> AddCommentOutput:
        return AddCommentOutput(issue_id_or_key=input.issue_id_or_key.strip())

    def _prepare(self, input: AddCommentInput) -> tuple[AddCommentOutput, str, dict[str, Any]]:
        issue_id = _identifier(input.issue_id_or_key)
        _plain(input.body, "body", True)
        return (
            AddCommentOutput(issue_id_or_key=issue_id),
            "/issue/" + path_escape(issue_id) + "/comment",
            {"body": _document(input.body)},
        )

    def _accepted(
        self, requested: AddCommentOutput, resource: dict[str, Any], receipt: Receipt
    ) -> MutationAttempt[AddCommentOutput]:
        comment_id = _field_string(resource, "id")
        author = _account(resource.get("author"))
        try:
            created_at = _timestamp(_field_string(resource, "created"))
        except ValueError:
            created_at = datetime.min.replace(tzinfo=UTC)
        return _mutation(
            "added",
            replace(
                requested,
                comment_id=comment_id,
                author_account_id=author.account_id if author else "",
                created_at=created_at,
            ),
            replace(receipt, provider_object_id=comment_id),
        )

    async def _find(
        self,
        call: Call,
        credentials: Any,
        cloud_id: str,
        requested: AddCommentOutput,
        marker: dict[str, str],
        provider_id: str,
    ) -> tuple[dict[str, Any] | None, str]:
        return await _find_comment(
            self.client, call, credentials, cloud_id, requested.issue_id_or_key, marker, provider_id
        )
