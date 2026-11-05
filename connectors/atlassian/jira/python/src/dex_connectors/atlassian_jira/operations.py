# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Five Jira Cloud operation invokes and their provider-safe outcomes."""

import hashlib
import json
import math
import re
from dataclasses import replace
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
from .client import Client, Exchange, SessionError, failure, path_escape, rejected_fields, rejection_failure
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
    result = _read_result(
        client, call, "transitionIssue", "issue", response, TransitionIssueOutput(), issue_id, True
    )
    if result is not None:
        return None, cast(MutationAttempt[TransitionIssueOutput], result)
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
        return None, _mutation(
            "invalidResponse",
            TransitionIssueOutput(),
            client.receipt(call, response, issue_id),
            failure(
                "transitionIssue", FailureKind.PROTOCOL, "Jira returned an invalid issue or transition list"
            ),
        )


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
        try:
            credentials, cloud_id = await self.client.session(call, "transitionIssue")
        except SessionError as error:
            return cast(
                MutationAttempt[TransitionIssueOutput], _session_result(error, TransitionIssueOutput(), True)
            )
        snapshot, early = await _transition_snapshot(self.client, call, credentials, cloud_id, issue_id)
        if early is not None:
            return early
        assert snapshot is not None
        found_id, key, status, available = snapshot
        receipt = self.client.receipt(call, object_id=key)
        output = TransitionIssueOutput(
            issue_id=found_id, issue_key=key, previous_status=status, status=status
        )
        if destination and _equal_fold(status.name, destination):
            return _mutation("transitioned", replace(output, is_already_in_destination_status=True), receipt)
        matches = [
            item
            for item in available
            if (not transition_id or item.id == transition_id)
            and (not transition_name or _equal_fold(item.name.strip(), transition_name))
            and (not destination or _equal_fold(item.destination_status.name, destination))
        ]
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
        output = replace(output, transition=chosen)
        if chosen.destination_status.id == status.id:
            return _mutation("transitioned", replace(output, is_already_in_destination_status=True), receipt)
        payload: dict[str, Any] = {"transition": {"id": chosen.id}}
        if resolution:
            payload["fields"] = {"resolution": {"name": resolution}}
        response = await self.client.request(
            call,
            credentials,
            "POST",
            "/issue/" + path_escape(issue_id) + "/transitions",
            cloud_id=cloud_id,
            payload=payload,
        )
        receipt = self.client.receipt(call, response, key)
        if response.request_defect:
            return _mutation(
                "defect", output, receipt,
                failure("transitionIssue", FailureKind.LOCAL_DEFECT, "Jira request could not be built"),
            )
        if not response.transport_error and 200 <= response.status < 300:
            return _mutation("transitioned", replace(output, status=chosen.destination_status), receipt)
        if response.status == 404:
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
        if response.status == 429:
            return MutationAttempt.retry(
                failure(
                    "transitionIssue",
                    FailureKind.RATE_LIMIT,
                    "Jira rate limited the transition before performing it",
                ),
                self.client.retry_after(response),
            )
        if response.transport_error and response.not_sent:
            return MutationAttempt.retry(
                failure(
                    "transitionIssue",
                    FailureKind.TRANSPORT,
                    "Jira could not be reached, so the issue was not transitioned",
                )
            )
        reread, read_failure = await _transition_snapshot(self.client, call, credentials, cloud_id, issue_id)
        if reread is not None and reread[2].id == chosen.destination_status.id:
            return _mutation("transitioned", replace(output, status=reread[2]), receipt)
        if read_failure is not None:
            return MutationAttempt.retry(
                failure(
                    "transitionIssue",
                    FailureKind.AVAILABILITY,
                    "Jira transition outcome could not be read back",
                )
            )
        assert reread is not None
        if not response.transport_error and response.status < 400:
            return _mutation(
                "invalidResponse",
                output,
                receipt,
                failure(
                    "transitionIssue",
                    FailureKind.PROTOCOL,
                    f"Jira returned unexpected HTTP {response.status} for the transition",
                ),
            )
        if 400 <= response.status < 500 and response.status not in (408, 409, 429):
            fields = rejected_fields(response.body)
            return _mutation(
                "providerRejected",
                replace(output, status=reread[2], rejected_field_ids=fields),
                receipt,
                rejection_failure("transitionIssue", "transition", response.status, fields),
            )
        if response.status == 409:
            return MutationAttempt.retry(
                failure(
                    "transitionIssue",
                    FailureKind.CONFLICT,
                    "Jira reported a conflicting update, so the transition is retried",
                ),
                self.client.retry_after(response),
            )
        return MutationAttempt.retry(
            failure(
                "transitionIssue",
                FailureKind.AVAILABILITY,
                "Jira did not confirm the transition and the issue has not moved yet",
            ),
            self.client.retry_after(response),
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
        )


class _FencedWrite:
    """Common single-dispatch and status path for Jira's two unkeyed writes."""

    operation: str
    subject: str

    def __init__(self, client: Client) -> None:
        self.client = client

    def _prepare(self, input: Any) -> tuple[Any, str, dict[str, Any]]:
        raise NotImplementedError

    async def _reconcile(
        self,
        call: Call,
        credentials: Any,
        cloud_id: str,
        requested: Any,
        marker: dict[str, str],
        provider_id: str,
        cause: Failure,
        receipt: Receipt,
    ) -> MutationAttempt[Any]:
        raise NotImplementedError

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
            fence = Fence(call, timedelta(seconds=30))
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
            return MutationAttempt.uncertain(
                requested,
                failure(self.operation, FailureKind.PROTOCOL, "Jira dispatch checkpoint cannot be trusted"),
                Receipt(),
            )
        try:
            credentials, cloud_id = await self.client.session(call, self.operation)
        except SessionError as error:
            if decision.kind == DecisionKind.SEND_ALLOWED:
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
                return cast(MutationAttempt[Any], _session_result(error, requested, True))
            if error.retryable:
                return MutationAttempt.retry(error.failure, error.retry_after)
            return MutationAttempt.uncertain(requested, error.failure, Receipt())
        marker = _marker(call)
        receipt = self.client.receipt(call)
        if decision.kind == DecisionKind.READ_BACK_REQUIRED:
            provider_id = (
                decision.marker.provider_id if decision.marker and decision.marker.call_id == call.id else ""
            )
            return await self._reconcile(
                call,
                credentials,
                cloud_id,
                requested,
                marker,
                provider_id,
                failure(
                    self.operation,
                    FailureKind.AVAILABILITY,
                    f"Jira {self.subject} dispatch may have succeeded",
                ),
                receipt,
            )
        payload["properties"] = [marker]
        dispatch = _WriteDispatch(self.client, call, credentials, cloud_id, path, payload)
        try:
            response = await fence.dispatch(dispatch.send)
        except TimeoutError:
            return await self._reconcile(
                call,
                credentials,
                cloud_id,
                requested,
                marker,
                "",
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
            return _mutation(
                "defect",
                requested,
                receipt,
                failure(self.operation, FailureKind.LOCAL_DEFECT, "Jira dispatch was not allowed"),
            )
        classified = self.client.classify_write(self.operation, self.subject, response)
        receipt = self.client.receipt(call, response)
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
                call, credentials, cloud_id, requested, marker, "", classified.failure, receipt
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
            if response.status == 409:
                assert classified.failure is not None
                return await self._reconcile(
                    call, credentials, cloud_id, requested, marker, "", classified.failure, receipt
                )
            branch = (
                "notFound"
                if classified.outcome == "notFound" and self.operation == "addComment"
                else "providerRejected"
            )
            return _mutation(
                branch, self._rejected(requested, classified.rejected_fields), receipt, classified.failure
            )
        try:
            resource = _object(json.loads(response.body))
            provider_id = _field_string(resource, "id")
        except (ValueError, TypeError):
            resource, provider_id = {}, ""
        if _NUMERIC.fullmatch(provider_id):
            try:
                await fence.record_provider_id(provider_id)
            except Exception:
                pass  # A lost identity checkpoint never authorizes a duplicate write.
            if self.operation == "addComment" or _ISSUE.fullmatch(_field_string(resource, "key")):
                return self._accepted(requested, resource, receipt)
            return await self._reconcile(
                call,
                credentials,
                cloud_id,
                requested,
                marker,
                provider_id,
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

    async def _reconcile(
        self,
        call: Call,
        credentials: Any,
        cloud_id: str,
        requested: CreateIssueOutput,
        marker: dict[str, str],
        provider_id: str,
        cause: Failure,
        receipt: Receipt,
    ) -> MutationAttempt[CreateIssueOutput]:
        candidate, status = await _find_created(
            self.client, call, credentials, cloud_id, requested, marker, provider_id
        )
        if status == "found" and candidate is not None:
            return self._accepted(requested, candidate, receipt)
        if status == "retry":
            return MutationAttempt.retry(
                failure(
                    self.operation,
                    FailureKind.AVAILABILITY,
                    "Jira issue read-back is temporarily unavailable",
                )
            )
        return MutationAttempt.uncertain(requested, cause, receipt)


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

    async def _reconcile(
        self,
        call: Call,
        credentials: Any,
        cloud_id: str,
        requested: AddCommentOutput,
        marker: dict[str, str],
        provider_id: str,
        cause: Failure,
        receipt: Receipt,
    ) -> MutationAttempt[AddCommentOutput]:
        candidate, status = await _find_comment(
            self.client, call, credentials, cloud_id, requested.issue_id_or_key, marker, provider_id
        )
        if status == "found" and candidate is not None:
            return self._accepted(requested, candidate, receipt)
        if status == "retry":
            return MutationAttempt.retry(
                failure(
                    self.operation,
                    FailureKind.AVAILABILITY,
                    "Jira comment read-back is temporarily unavailable",
                )
            )
        return MutationAttempt.uncertain(requested, cause, receipt)
