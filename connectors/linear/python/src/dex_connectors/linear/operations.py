# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Linear Query and Mutation invocations matching the Go connector."""

from __future__ import annotations

import re
from dataclasses import fields
from datetime import UTC, datetime
from typing import Any, Literal, TypeVar, overload

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

from ._generated.definitions import (
    ADD_COMMENT_DEFINITION,
    CREATE_ISSUE_DEFINITION,
    FIND_USER_BY_EMAIL_DEFINITION,
    GET_ISSUE_DEFINITION,
    LIST_WORKFLOW_STATES_DEFINITION,
    SEARCH_ISSUES_DEFINITION,
    UPDATE_ISSUE_DEFINITION,
)
from .client import Client, Exchange, _failure
from .documents import (
    ADDCOMMENTDOCUMENT,
    CREATEISSUEDOCUMENT,
    FINDUSERBYEMAILDOCUMENT,
    GETISSUEDOCUMENT,
    LISTWORKFLOWSTATESDOCUMENT,
    READCOMMENTDOCUMENT,
    READISSUESUMMARYDOCUMENT,
    SEARCHISSUESDOCUMENT,
    UPDATEISSUEDOCUMENT,
)
from .models import (
    AddCommentInput,
    AddCommentOutput,
    CreateIssueInput,
    CreateIssueOutput,
    FindUserByEmailInput,
    GetIssueInput,
    Issue,
    IssueSearchFilter,
    IssueSummary,
    ListWorkflowStatesInput,
    ListWorkflowStatesOutput,
    SearchIssuesInput,
    SearchIssuesOutput,
    UpdateIssueInput,
    UpdateIssueOutput,
    User,
    WorkflowState,
    WorkflowStateReference,
)
from .wire import (
    client_entity_id,
    due_date,
    email,
    issue,
    issue_locator,
    issue_summary,
    linear_time,
    markdown,
    one_line,
    priority,
    uuid_field,
    uuid_list,
)

_CURSOR = re.compile(r"[A-Za-z0-9_\-.~=+/:]{1,512}\Z")
_STATE_TYPES = ("triage", "backlog", "unstarted", "started", "completed", "canceled", "duplicate")
T = TypeVar("T")


def _zero_issue() -> Issue:
    return Issue(state=WorkflowStateReference(type=""))


def _query_outcome(
    exchange: Exchange,
    empty: T,
    receipt: Receipt,
) -> QueryAttempt[T]:
    assert exchange.failure is not None
    if exchange.outcome == "retry":
        return QueryAttempt.retry(exchange.failure, exchange.retry_after)
    branch = {"invalid": "invalidResponse", "defect": "defect"}.get(exchange.outcome, "providerRejected")
    return QueryAttempt.branch_result(branch, empty, receipt, exchange.failure)


def _write_outcome(
    exchange: Exchange,
    empty: T,
    receipt: Receipt,
    *,
    invalid_is_retry: bool,
) -> MutationAttempt[T]:
    assert exchange.failure is not None
    if exchange.outcome == "retry" or (exchange.outcome == "invalid" and invalid_is_retry):
        return MutationAttempt.retry(exchange.failure, exchange.retry_after)
    branch = {"invalid": "invalidResponse", "defect": "defect"}.get(exchange.outcome, "providerRejected")
    return MutationAttempt.branch_result(branch, empty, receipt, exchange.failure)


@overload
def _local_failure(
    operation: str, output: T, message: str, *, mutation: Literal[True]
) -> MutationAttempt[T]: ...


@overload
def _local_failure(
    operation: str, output: T, message: str, *, mutation: Literal[False] = False
) -> QueryAttempt[T]: ...


def _local_failure(operation: str, output: Any, message: str, *, mutation: bool = False) -> Any:
    failure = _failure(operation, FailureKind.VALIDATION, message)
    if mutation:
        return MutationAttempt.branch_result("defect", output, Receipt(), failure)
    return QueryAttempt.branch_result("defect", output, Receipt(), failure)


def _nodes(data: Any, field: str, maximum: int) -> list[Any]:
    if not isinstance(data, dict) or not isinstance(data.get(field), dict):
        raise ValueError(f"Linear returned a malformed {field} list")
    nodes = data[field].get("nodes")
    if nodes is None:
        return []
    if not isinstance(nodes, list) or len(nodes) > maximum:
        raise ValueError(f"Linear returned a malformed {field} list")
    return nodes


def _readback_worthy(exchange: Exchange) -> bool:
    if exchange.outcome == "invalid":
        return True
    if exchange.failure is None:
        return False
    if exchange.outcome == "retry":
        return exchange.failure.kind in (FailureKind.AVAILABILITY, FailureKind.TRANSPORT)
    return exchange.outcome == "rejected" and exchange.failure.kind in (
        FailureKind.VALIDATION,
        FailureKind.PROVIDER_REJECTION,
        FailureKind.QUOTA_EXHAUSTED,
    )


def _mutation_summary(exchange: Exchange, field: str) -> IssueSummary | None:
    if not isinstance(exchange.data, dict):
        return None
    payload = exchange.data.get(field)
    if not isinstance(payload, dict) or payload.get("success") is not True or payload.get("issue") is None:
        return None
    try:
        return issue_summary(payload["issue"])
    except ValueError:
        return None


def _classify_unusable(exchange: Exchange, field: str, operation: str) -> Exchange:
    if exchange.outcome != "succeeded":
        return exchange
    payload = exchange.data.get(field) if isinstance(exchange.data, dict) else None
    if isinstance(payload, dict) and payload.get("success") is False:
        return Exchange(
            "rejected",
            status=exchange.status,
            request_id=exchange.request_id,
            refreshed=exchange.refreshed,
            credentials=exchange.credentials,
            failure=_failure(
                operation, FailureKind.PROVIDER_REJECTION, "Linear reported that the write did not succeed"
            ),
        )
    return Exchange(
        "invalid",
        status=exchange.status,
        request_id=exchange.request_id,
        refreshed=exchange.refreshed,
        credentials=exchange.credentials,
        failure=_failure(
            operation, FailureKind.PROTOCOL, "Linear accepted the write but returned an unusable record"
        ),
    )


async def _read_issue_summary(
    client: Client,
    call: Call,
    credentials: Any,
    locator: tuple[str, dict[str, Any]],
    operation: str,
    *,
    allow_refresh: bool = True,
) -> tuple[Exchange, IssueSummary | None]:
    exchange = await client.exchange(
        call,
        operation,
        credentials,
        "LinearReadIssueSummary",
        READISSUESUMMARYDOCUMENT,
        {"filter": locator[1]},
        allow_refresh=allow_refresh,
    )
    if exchange.outcome != "succeeded":
        return exchange, None
    try:
        nodes = _nodes(exchange.data, "issues", 1)
        if not nodes:
            return exchange, None
        summary = issue_summary(nodes[0])
        if summary.id.lower() != locator[0].lower() and summary.identifier.lower() != locator[0].lower():
            raise ValueError("another issue")
        return exchange, summary
    except ValueError:
        return Exchange(
            "invalid",
            status=exchange.status,
            request_id=exchange.request_id,
            refreshed=exchange.refreshed,
            credentials=exchange.credentials,
            failure=_failure(
                operation,
                FailureKind.PROTOCOL,
                "Linear returned a malformed issue or another issue than requested",
            ),
        ), None


class SearchIssuesOperation:
    """Read one bounded page of filtered issues."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated search contract."""
        return SEARCH_ISSUES_DEFINITION

    async def invoke(self, call: Call, input: SearchIssuesInput) -> QueryAttempt[SearchIssuesOutput]:
        """Validate the filter, send a GraphQL query, and decode one page."""
        empty = SearchIssuesOutput()
        try:
            page_size = input.page_size or 50
            if not 1 <= page_size <= 100:
                raise ValueError("pageSize must be between 1 and 100")
            order = input.order or "createdAt"
            if order not in ("createdAt", "updatedAt"):
                raise ValueError("order must be createdAt or updatedAt")
            filter_value = _issue_filter(input.filter)
            variables: dict[str, Any] = {
                "first": page_size,
                "orderBy": order,
                "includeArchived": input.includes_archived,
            }
            if filter_value:
                variables["filter"] = filter_value
            if input.cursor.strip():
                cursor = input.cursor.strip()
                if not _CURSOR.fullmatch(cursor):
                    raise ValueError("cursor must be a NextCursor returned by searchIssues")
                variables["after"] = cursor
        except ValueError as error:
            return _local_failure("searchIssues", empty, str(error))
        credential = await self.client.resolve(call, "searchIssues")
        if isinstance(credential, Exchange):
            return _query_outcome(credential, empty, self.client.receipt(call, credential))
        result = await self.client.exchange(
            call, "searchIssues", credential, "LinearSearchIssues", SEARCHISSUESDOCUMENT, variables
        )
        receipt = self.client.receipt(call, result)
        if result.outcome != "succeeded":
            return _query_outcome(result, empty, receipt)
        try:
            nodes = _nodes(result.data, "issues", 100)
            issues = tuple(issue_summary(node) for node in nodes)
            page_info = result.data["issues"].get("pageInfo")
            if not isinstance(page_info, dict):
                page_info = {}
            cursor = ""
            if page_info.get("hasNextPage") is True:
                cursor = page_info.get("endCursor", "")
                if not isinstance(cursor, str) or not _CURSOR.fullmatch(cursor):
                    raise ValueError("Linear returned a continuing page without a usable cursor")
            return QueryAttempt.branch_result("searched", SearchIssuesOutput(issues, cursor), receipt)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "invalidResponse", empty, receipt, _failure("searchIssues", FailureKind.PROTOCOL, str(error))
            )


class GetIssueOperation:
    """Read one issue by UUID or team identifier."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated getIssue contract."""
        return GET_ISSUE_DEFINITION

    async def invoke(self, call: Call, input: GetIssueInput) -> QueryAttempt[Issue]:
        """Read and validate a full Linear issue."""
        empty = _zero_issue()
        try:
            locator = issue_locator(input.issue_id, "issueId")
        except ValueError as error:
            return _local_failure("getIssue", empty, str(error))
        credential = await self.client.resolve(call, "getIssue")
        if isinstance(credential, Exchange):
            return _query_outcome(credential, empty, self.client.receipt(call, credential))
        result = await self.client.exchange(
            call, "getIssue", credential, "LinearGetIssue", GETISSUEDOCUMENT, {"filter": locator[1]}
        )
        receipt = self.client.receipt(call, result, locator[0] if _is_uuid(locator[0]) else "")
        if result.outcome != "succeeded":
            return _query_outcome(result, empty, receipt)
        try:
            nodes = _nodes(result.data, "issues", 1)
            if not nodes:
                return QueryAttempt.branch_result(
                    "notFound",
                    empty,
                    receipt,
                    _failure(
                        "getIssue",
                        FailureKind.NOT_FOUND,
                        "Linear has no issue with this ID that the connection can see",
                    ),
                )
            decoded = issue(nodes[0])
            if decoded.id.lower() != locator[0].lower() and decoded.identifier.lower() != locator[0].lower():
                raise ValueError("Linear returned a malformed issue or another issue than requested")
        except ValueError:
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                _failure(
                    "getIssue",
                    FailureKind.PROTOCOL,
                    "Linear returned a malformed issue or another issue than requested",
                ),
            )
        return QueryAttempt.branch_result("found", decoded, self.client.receipt(call, result, decoded.id))


class FindUserByEmailOperation:
    """Resolve one workspace user by email."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated user lookup contract."""
        return FIND_USER_BY_EMAIL_DEFINITION

    async def invoke(self, call: Call, input: FindUserByEmailInput) -> QueryAttempt[User]:
        """Find one matching user, rejecting ambiguous answers."""
        empty = User()
        try:
            requested_email = email(input.email)
        except ValueError as error:
            return _local_failure("findUserByEmail", empty, str(error))
        credential = await self.client.resolve(call, "findUserByEmail")
        if isinstance(credential, Exchange):
            return _query_outcome(credential, empty, self.client.receipt(call, credential))
        result = await self.client.exchange(
            call,
            "findUserByEmail",
            credential,
            "LinearFindUserByEmail",
            FINDUSERBYEMAILDOCUMENT,
            {"email": requested_email, "includeDisabled": input.includes_disabled},
        )
        receipt = self.client.receipt(call, result)
        if result.outcome != "succeeded":
            return _query_outcome(result, empty, receipt)
        try:
            nodes = _nodes(result.data, "users", 1)
            if not nodes:
                return QueryAttempt.branch_result(
                    "notFound",
                    empty,
                    receipt,
                    _failure(
                        "findUserByEmail",
                        FailureKind.NOT_FOUND,
                        "no Linear user that the connection can see has this email",
                    ),
                )
            record = nodes[0]
            if (
                not isinstance(record, dict)
                or not _is_uuid(record.get("id"))
                or not isinstance(record.get("email"), str)
                or record["email"].strip().casefold() != requested_email.casefold()
            ):
                raise ValueError("Linear returned a malformed user or a user with another email")
            decoded = User(
                id=record["id"],
                name=_str(record.get("name", "")),
                display_name=_str(record.get("displayName", "")),
                email=record["email"],
                active=_bool(record.get("active", False)),
                admin=_bool(record.get("admin", False)),
                guest=_bool(record.get("guest", False)),
                url=_str(record.get("url", "")),
            )
        except ValueError as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                _failure("findUserByEmail", FailureKind.PROTOCOL, str(error)),
            )
        return QueryAttempt.branch_result("found", decoded, self.client.receipt(call, result, decoded.id))


class ListWorkflowStatesOperation:
    """List one team's workflow states in board order."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated workflow state contract."""
        return LIST_WORKFLOW_STATES_DEFINITION

    async def invoke(
        self, call: Call, input: ListWorkflowStatesInput
    ) -> QueryAttempt[ListWorkflowStatesOutput]:
        """Read and sort the team's states."""
        empty = ListWorkflowStatesOutput(team_id=input.team_id)
        try:
            team_id = uuid_field(input.team_id, "teamId", True)
        except ValueError as error:
            return _local_failure("listWorkflowStates", empty, str(error))
        empty = ListWorkflowStatesOutput(team_id=team_id)
        credential = await self.client.resolve(call, "listWorkflowStates")
        if isinstance(credential, Exchange):
            return _query_outcome(credential, empty, self.client.receipt(call, credential))
        result = await self.client.exchange(
            call,
            "listWorkflowStates",
            credential,
            "LinearListWorkflowStates",
            LISTWORKFLOWSTATESDOCUMENT,
            {"teamId": team_id},
        )
        receipt = self.client.receipt(call, result, team_id)
        if result.outcome != "succeeded":
            return _query_outcome(result, empty, receipt)
        try:
            nodes = _nodes(result.data, "workflowStates", 100)
            states: list[WorkflowState] = []
            for node in nodes:
                if (
                    not isinstance(node, dict)
                    or not _is_uuid(node.get("id"))
                    or not node.get("name")
                    or not node.get("type")
                    or not isinstance(node.get("team"), dict)
                    or str(node["team"].get("id", "")).lower() != team_id
                ):
                    raise ValueError("Linear returned a malformed workflow state or one of another team")
                states.append(
                    WorkflowState(
                        id=node["id"],
                        name=_str(node["name"]),
                        type=_str(node["type"]),
                        position=_number(node.get("position", 0)),
                        color=_str(node.get("color", "")),
                    )
                )
            if not states:
                return QueryAttempt.branch_result(
                    "notFound",
                    empty,
                    receipt,
                    _failure(
                        "listWorkflowStates",
                        FailureKind.NOT_FOUND,
                        "Linear has no team with this ID that the connection can see",
                    ),
                )
            states.sort(
                key=lambda state: (
                    _STATE_TYPES.index(state.type) if state.type in _STATE_TYPES else len(_STATE_TYPES),
                    state.position,
                )
            )
        except ValueError as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                _failure("listWorkflowStates", FailureKind.PROTOCOL, str(error)),
            )
        return QueryAttempt.branch_result("listed", ListWorkflowStatesOutput(team_id, tuple(states)), receipt)


def _is_uuid(value: Any) -> bool:
    return (
        isinstance(value, str)
        and re.fullmatch(r"[0-9a-fA-F]{8}(?:-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}", value) is not None
    )


def _str(value: Any) -> str:
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError("Linear returned a malformed record")
    return value


def _bool(value: Any) -> bool:
    if value is None:
        return False
    if not isinstance(value, bool):
        raise ValueError("Linear returned a malformed record")
    return value


def _number(value: Any) -> float:
    if value is None:
        return 0.0
    if type(value) not in (int, float):
        raise ValueError("Linear returned a malformed record")
    return float(value)


def _issue_filter(filter: IssueSearchFilter) -> dict[str, Any]:
    if isinstance(filter, dict):
        by_wire = {
            field.metadata.get("dex_json_name", field.name): field.name for field in fields(IssueSearchFilter)
        }
        values = {by_wire[name]: value for name, value in filter.items()}
        for name in ("updated_after", "created_after"):
            if isinstance(values.get(name), str):
                values[name] = datetime.fromisoformat(values[name].replace("Z", "+00:00"))
        filter = IssueSearchFilter(**values)
    clauses: dict[str, Any] = {}
    team_id = uuid_field(filter.team_id, "filter.teamId")
    team_key = filter.team_key.strip()
    if team_id and team_key:
        raise ValueError("set at most one of filter.teamId and filter.teamKey")
    if team_id:
        clauses["team"] = {"id": {"eq": team_id}}
    elif team_key:
        if not re.fullmatch(r"[A-Za-z][A-Za-z0-9]{0,9}", team_key):
            raise ValueError("filter.teamKey must be a team key such as ENG")
        clauses["team"] = {"key": {"eq": team_key.upper()}}
    states: dict[str, Any] = {}
    if len(filter.state_types) > 20:
        raise ValueError("filter.stateTypes accepts at most 20 values")
    if filter.state_types:
        if any(state not in _STATE_TYPES for state in filter.state_types):
            raise ValueError(
                "filter.stateTypes values must be triage, backlog, unstarted, started, "
                "completed, canceled, or duplicate"
            )
        states["type"] = {"in": list(filter.state_types)}
    state_ids = uuid_list(filter.state_ids, "filter.stateIds")
    if state_ids:
        states["id"] = {"in": state_ids}
    if states:
        clauses["state"] = states
    assignee_id = uuid_field(filter.assignee_id, "filter.assigneeId")
    if filter.unassigned and (assignee_id or filter.assignee_email):
        raise ValueError("filter.unassigned excludes filter.assigneeId and filter.assigneeEmail")
    if filter.unassigned:
        clauses["assignee"] = {"null": True}
    elif assignee_id or filter.assignee_email:
        assignee: dict[str, Any] = {}
        if assignee_id:
            assignee["id"] = {"eq": assignee_id}
        if filter.assignee_email:
            try:
                assignee["email"] = {"eqIgnoreCase": email(filter.assignee_email)}
            except ValueError as error:
                raise ValueError(f"filter.assigneeEmail: {error}") from None
        clauses["assignee"] = assignee
    label_ids = uuid_list(filter.label_ids, "filter.labelIds")
    if len(filter.label_names) > 20:
        raise ValueError("filter.labelNames accepts at most 20 names")
    label: dict[str, Any] = {}
    if label_ids:
        label["id"] = {"in": label_ids}
    if filter.label_names:
        label["name"] = {"in": [one_line(name, "filter.labelNames") for name in filter.label_names]}
    if label:
        clauses["labels"] = {"some": label}
    project_id = uuid_field(filter.project_id, "filter.projectId")
    if project_id:
        clauses["project"] = {"id": {"eq": project_id}}
    title: dict[str, Any] = {}
    if filter.title:
        title["eq"] = one_line(filter.title, "filter.title")
    if filter.title_contains:
        title["containsIgnoreCase"] = one_line(filter.title_contains, "filter.titleContains")
    if title:
        clauses["title"] = title
    for name, value in (("updatedAt", filter.updated_after), ("createdAt", filter.created_after)):
        if value is not None:
            timestamp = (
                datetime.fromisoformat(value.replace("Z", "+00:00")) if isinstance(value, str) else value
            )
            if not isinstance(timestamp, datetime) or timestamp.tzinfo is None:
                raise ValueError(f"filter.{name} must have a timezone")
            utc = timestamp.astimezone(UTC)
            clauses[name] = {"gt": utc.strftime("%Y-%m-%dT%H:%M:%S.") + f"{utc.microsecond // 1000:03d}Z"}
    return clauses


class CreateIssueOperation:
    """Create one issue under a stable client supplied UUID."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated createIssue contract."""
        return CREATE_ISSUE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreateIssueInput) -> IdempotencyKey:
        """Use the stable Dex Call ID to derive the Linear issue UUID."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: CreateIssueInput) -> MutationAttempt[CreateIssueOutput]:
        """Create or read back a stable issue after an ambiguous response."""
        requested = CreateIssueOutput(team_id=input.team_id, title=input.title)
        try:
            team_id = uuid_field(input.team_id, "teamId", True)
            title = one_line(input.title, "title")
            requested = CreateIssueOutput(team_id=team_id, title=title)
            description = markdown(input.description, "description")
            priority(input.priority)
            due = due_date(input.due_date)
            labels = uuid_list(input.label_ids, "labelIds")
            issue_id = client_entity_id(str(call.idempotency_key), "issue")
            body: dict[str, Any] = {"id": issue_id, "teamId": team_id, "title": title}
            for field, value in (
                ("stateId", input.state_id),
                ("assigneeId", input.assignee_id),
                ("projectId", input.project_id),
            ):
                validated = uuid_field(value, field)
                if validated:
                    body[field] = validated
            if input.parent_id:
                body["parentId"] = issue_locator(input.parent_id, "parentId")[0]
            if description:
                body["description"] = description
            if labels:
                body["labelIds"] = labels
            if input.priority is not None:
                body["priority"] = input.priority
            if due:
                body["dueDate"] = due
        except ValueError as error:
            return _local_failure("createIssue", requested, str(error), mutation=True)
        credential = await self.client.resolve(call, "createIssue")
        if isinstance(credential, Exchange):
            return _write_outcome(
                credential, requested, self.client.receipt(call, credential), invalid_is_retry=True
            )
        result = await self.client.exchange(
            call, "createIssue", credential, "LinearCreateIssue", CREATEISSUEDOCUMENT, {"input": body}
        )
        receipt = self.client.receipt(call, result, issue_id)
        summary = _mutation_summary(result, "issueCreate")
        if summary is not None:
            return MutationAttempt.branch_result(
                "created",
                CreateIssueOutput(team_id, title, summary.id, summary),
                self.client.receipt(call, result, summary.id),
            )
        result = _classify_unusable(result, "issueCreate", "createIssue")
        if _readback_worthy(result):
            read, summary = await _read_issue_summary(
                self.client,
                call,
                result.credentials or credential,
                (issue_id, {"id": {"eq": issue_id}}),
                "createIssue",
                allow_refresh=not result.refreshed,
            )
            if summary is not None:
                return MutationAttempt.branch_result(
                    "created",
                    CreateIssueOutput(team_id, title, summary.id, summary, True),
                    self.client.receipt(call, read, summary.id),
                )
            if read.outcome != "succeeded":
                assert read.failure is not None
                return MutationAttempt.retry(read.failure, read.retry_after)
            if result.outcome == "invalid":
                assert result.failure is not None
                return MutationAttempt.retry(result.failure)
        return _write_outcome(result, requested, receipt, invalid_is_retry=True)


class UpdateIssueOperation:
    """Apply absolute issue changes that are safe to repeat."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated updateIssue contract."""
        return UPDATE_ISSUE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpdateIssueInput) -> IdempotencyKey:
        """Use the stable Dex Call ID for the update attempt."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: UpdateIssueInput) -> MutationAttempt[UpdateIssueOutput]:
        """Set issue fields, checking read-back after an input rejection."""
        requested = UpdateIssueOutput(issue_id=input.issue_id)
        try:
            locator = issue_locator(input.issue_id, "issueId")
            requested = UpdateIssueOutput(issue_id=locator[0])
            body: dict[str, Any] = {}
            state_id = uuid_field(input.state_id, "stateId")
            if state_id:
                body["stateId"] = state_id
            assignee_id = uuid_field(input.assignee_id, "assigneeId")
            if assignee_id and input.unassigns:
                raise ValueError("set at most one of assigneeId and unassigns")
            if assignee_id:
                body["assigneeId"] = assignee_id
            elif input.unassigns:
                body["assigneeId"] = None
            added = uuid_list(input.added_label_ids, "addedLabelIds")
            removed = uuid_list(input.removed_label_ids, "removedLabelIds")
            if set(added) & set(removed):
                raise ValueError("a label cannot be both added and removed")
            if added:
                body["addedLabelIds"] = added
            if removed:
                body["removedLabelIds"] = removed
            priority(input.priority)
            if input.priority is not None:
                body["priority"] = input.priority
            due = due_date(input.due_date)
            if due and input.clears_due_date:
                raise ValueError("set at most one of dueDate and clearsDueDate")
            if due:
                body["dueDate"] = due
            elif input.clears_due_date:
                body["dueDate"] = None
            if input.title:
                body["title"] = one_line(input.title, "title")
            if not body:
                raise ValueError("updateIssue needs at least one change")
        except ValueError as error:
            return _local_failure("updateIssue", requested, str(error), mutation=True)
        credential = await self.client.resolve(call, "updateIssue")
        if isinstance(credential, Exchange):
            return _write_outcome(
                credential, requested, self.client.receipt(call, credential), invalid_is_retry=False
            )
        result = await self.client.exchange(
            call,
            "updateIssue",
            credential,
            "LinearUpdateIssue",
            UPDATEISSUEDOCUMENT,
            {"id": locator[0], "input": body},
        )
        receipt = self.client.receipt(call, result, locator[0])
        summary = _mutation_summary(result, "issueUpdate")
        if summary is not None:
            return MutationAttempt.branch_result(
                "updated",
                UpdateIssueOutput(locator[0], summary),
                self.client.receipt(call, result, summary.id),
            )
        result = _classify_unusable(result, "issueUpdate", "updateIssue")
        if result.outcome == "rejected" and _readback_worthy(result):
            read, summary = await _read_issue_summary(
                self.client,
                call,
                result.credentials or credential,
                locator,
                "updateIssue",
                allow_refresh=not result.refreshed,
            )
            if read.outcome == "succeeded" and summary is None:
                return MutationAttempt.branch_result(
                    "notFound",
                    requested,
                    receipt,
                    _failure(
                        "updateIssue",
                        FailureKind.NOT_FOUND,
                        "Linear has no issue with this ID that the connection can see",
                    ),
                )
            if read.outcome == "retry":
                assert read.failure is not None
                return MutationAttempt.retry(read.failure, read.retry_after)
        return _write_outcome(result, requested, receipt, invalid_is_retry=False)


class AddCommentOperation:
    """Add one comment under a stable client supplied UUID."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated addComment contract."""
        return ADD_COMMENT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: AddCommentInput) -> IdempotencyKey:
        """Use the stable Dex Call ID to derive the comment UUID."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: AddCommentInput) -> MutationAttempt[AddCommentOutput]:
        """Add or read back one comment after an ambiguous response."""
        requested = AddCommentOutput(issue_id=input.issue_id)
        try:
            locator = issue_locator(input.issue_id, "issueId")
            body = markdown(input.body, "body", True)
            comment_id = client_entity_id(str(call.idempotency_key), "comment")
            requested = AddCommentOutput(issue_id=locator[0])
        except ValueError as error:
            return _local_failure("addComment", requested, str(error), mutation=True)
        credential = await self.client.resolve(call, "addComment")
        if isinstance(credential, Exchange):
            return _write_outcome(
                credential, requested, self.client.receipt(call, credential), invalid_is_retry=True
            )
        result = await self.client.exchange(
            call,
            "addComment",
            credential,
            "LinearAddComment",
            ADDCOMMENTDOCUMENT,
            {"input": {"id": comment_id, "issueId": locator[0], "body": body}},
        )
        receipt = self.client.receipt(call, result, comment_id)
        payload = result.data.get("commentCreate") if isinstance(result.data, dict) else None
        if isinstance(payload, dict) and payload.get("success") is True:
            try:
                created_comment = _comment(payload.get("comment"), comment_id)
                return MutationAttempt.branch_result(
                    "added",
                    AddCommentOutput(
                        locator[0], comment_id, created_comment["url"], created_comment["createdAt"]
                    ),
                    receipt,
                )
            except ValueError:
                pass
        result = _classify_unusable(result, "commentCreate", "addComment")
        if not _readback_worthy(result):
            return _write_outcome(result, requested, receipt, invalid_is_retry=True)
        read = await self.client.exchange(
            call,
            "addComment",
            result.credentials or credential,
            "LinearReadComment",
            READCOMMENTDOCUMENT,
            {"filter": {"id": {"eq": comment_id}}},
            allow_refresh=not result.refreshed,
        )
        read_comment: dict[str, str] | None
        try:
            nodes = _nodes(read.data, "comments", 1) if read.outcome == "succeeded" else []
            read_comment = _comment(nodes[0], comment_id) if nodes else None
        except ValueError:
            read_comment = None
            if read.outcome == "succeeded":
                read = Exchange(
                    "invalid",
                    status=read.status,
                    request_id=read.request_id,
                    failure=_failure(
                        "addComment", FailureKind.PROTOCOL, "Linear returned an unusable comment read-back"
                    ),
                )
        if read.outcome != "succeeded":
            assert read.failure is not None
            return MutationAttempt.retry(read.failure, read.retry_after)
        if read_comment is not None:
            return MutationAttempt.branch_result(
                "added",
                AddCommentOutput(
                    locator[0], comment_id, read_comment["url"], read_comment["createdAt"], True
                ),
                self.client.receipt(call, read, comment_id),
            )
        if result.outcome == "rejected":
            issue_read, issue_record = await _read_issue_summary(
                self.client,
                call,
                read.credentials or result.credentials or credential,
                locator,
                "addComment",
                allow_refresh=not (result.refreshed or read.refreshed),
            )
            if issue_read.outcome == "retry":
                assert issue_read.failure is not None
                return MutationAttempt.retry(issue_read.failure, issue_read.retry_after)
            if issue_read.outcome == "succeeded" and issue_record is None:
                return MutationAttempt.branch_result(
                    "notFound",
                    requested,
                    receipt,
                    _failure(
                        "addComment",
                        FailureKind.NOT_FOUND,
                        "Linear has no issue with this ID that the connection can see",
                    ),
                )
        return _write_outcome(result, requested, receipt, invalid_is_retry=True)


def _comment(value: Any, comment_id: str) -> dict[str, str]:
    if (
        not isinstance(value, dict)
        or not isinstance(value.get("id"), str)
        or value["id"].lower() != comment_id.lower()
    ):
        raise ValueError("Linear returned a malformed comment or another comment than requested")
    try:
        created = linear_time(value.get("createdAt"))
    except ValueError:
        raise ValueError("Linear returned a comment without a creation time") from None
    return {"url": _str(value.get("url", "")), "createdAt": created}
