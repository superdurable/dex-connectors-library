# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""ClickUp Query and Mutation invocations."""

import asyncio
import json
from dataclasses import dataclass, field
from datetime import UTC, datetime
from email import policy
from email.utils import parseaddr
from typing import Any, TypeVar

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

from ._generated import definitions as definitions
from .client import (
    Client,
    Exchange,
    decode_object,
    decode_search_page,
    decode_task,
    failure,
    flexible_id,
    flexible_integer,
    numeric_id,
    path_escape,
    task_id,
    unix_milliseconds,
)
from .models import (
    AddCommentInput,
    AddCommentOutput,
    CreateTaskInput,
    CreateTaskOutput,
    FindMemberByEmailInput,
    GetTaskInput,
    Member,
    SearchTasksInput,
    SearchTasksOutput,
    Task,
    UpdateTaskInput,
    UpdateTaskOutput,
    UpdateTaskTagsInput,
    UpdateTaskTagsOutput,
)

T = TypeVar("T")


def _equal_fold(left: str, right: str) -> bool:
    return len(left) == len(right) and all(
        a.casefold() == b.casefold() for a, b in zip(left, right, strict=True)
    )


def _bare_email(value: str) -> bool:
    parsed = policy.default.header_factory("To", value)
    display_name, address = parseaddr(value)
    return (
        not parsed.defects
        and len(parsed.addresses) == 1
        and not display_name
        and address == value
        and bool(address)
        and "@" in address
    )


async def _budgeted_exchange(
    client: Client, deadline: float, credentials: Any, method: str, path: str
) -> Exchange:
    try:
        return await asyncio.wait_for(
            client.exchange("updateTaskTags", credentials, method, path),
            timeout=max(0.0, deadline - asyncio.get_running_loop().time()),
        )
    except TimeoutError:
        return Exchange(
            "unconfirmed",
            failure(
                "updateTaskTags", FailureKind.TRANSPORT, "ClickUp request failed before a response arrived"
            ),
        )


def _query_failure(result: Exchange, output: T, receipt: Receipt, not_found: bool = True) -> QueryAttempt[T]:
    if result.is_retryable:
        assert result.failure is not None
        return QueryAttempt.retry(result.failure, result.retry_after.total_seconds())
    branch = (
        ("notFound" if not_found else "providerRejected")
        if result.kind == "not_found"
        else (
            "invalidResponse"
            if result.kind == "invalid"
            else "defect"
            if result.kind == "defect"
            else "providerRejected"
        )
    )
    return QueryAttempt.branch_result(branch, output, receipt, result.failure)


def _mutation_failure(
    result: Exchange, output: T, receipt: Receipt, invalid_response: bool = True
) -> MutationAttempt[T]:
    if result.is_retryable:
        assert result.failure is not None
        return MutationAttempt.retry(result.failure, result.retry_after.total_seconds())
    branch = (
        "notFound"
        if result.kind == "not_found"
        else (
            "invalidResponse"
            if result.kind == "invalid" and invalid_response
            else "defect"
            if result.kind == "defect"
            else "providerRejected"
        )
    )
    return MutationAttempt.branch_result(branch, output, receipt, result.failure)


def _query_defect(operation: str, output: T, message: str) -> QueryAttempt[T]:
    return QueryAttempt.branch_result(
        "defect", output, Receipt(), failure(operation, FailureKind.VALIDATION, message)
    )


def _mutation_defect(operation: str, output: T, message: str) -> MutationAttempt[T]:
    return MutationAttempt.branch_result(
        "defect", output, Receipt(), failure(operation, FailureKind.VALIDATION, message)
    )


def _query_credentials_failure(output: T, cause: Failure) -> QueryAttempt[T]:
    return QueryAttempt.branch_result("defect", output, Receipt(), cause)


def _mutation_credentials_failure(output: T, cause: Failure) -> MutationAttempt[T]:
    return MutationAttempt.branch_result("defect", output, Receipt(), cause)


def _exchange_failure(result: Exchange) -> Failure:
    assert result.failure is not None
    return result.failure


def _validate_task_id(value: str) -> None:
    if not task_id(value):
        raise ValueError("taskId must be a ClickUp task ID of letters, digits, '-', or '_'")


def _validate_numeric_id(value: str) -> None:
    if not numeric_id(value):
        raise ValueError("ID must be 1 to 20 digits")


def _validate_name(value: str) -> None:
    if not value.strip() or len(value.encode()) > 1024:
        raise ValueError("name must be non-empty and at most 1024 bytes")


def _validate_status(value: str) -> None:
    if value and (not value.strip() or len(value.encode()) > 255):
        raise ValueError("status is invalid")


def _validate_priority(value: int | None) -> None:
    if value not in (None, 0, 1, 2, 3, 4):
        raise ValueError("priority must be 1 through 4")


def _validate_users(values: tuple[int, ...]) -> None:
    if (
        len(values) > 50
        or len(values) != len(set(values))
        or any(type(value) is not int or value <= 0 for value in values)
    ):
        raise ValueError("invalid user ID")


def _validate_tags(values: tuple[str, ...]) -> None:
    if len(values) != len(set(values)):
        raise ValueError("duplicate tag")
    for tag in values:
        if (
            not tag
            or tag.strip() != tag
            or len(tag.encode()) > 100
            or tag in (".", "..")
            or "/" in tag
            or any(character in tag for character in "\x00\r\n\t")
        ):
            raise ValueError("invalid tag name")


def _milliseconds(value: datetime | None) -> int | None:
    if value is None:
        return None
    if value.tzinfo is None:
        raise ValueError("date must have a time-zone offset")
    elapsed = value.astimezone(UTC) - datetime(1970, 1, 1, tzinfo=UTC)
    result = elapsed.days * 86_400_000 + elapsed.seconds * 1000 + elapsed.microseconds // 1000
    if result <= 0:
        raise ValueError("date must be after the Unix epoch")
    return result


def _task_path(value: str) -> str:
    return "/task/" + path_escape(value)


class SearchTasksOperation:
    """Search one ClickUp Workspace page."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated search identity and branches."""
        return definitions.SEARCH_TASKS_DEFINITION

    async def invoke(self, call: Call, input: SearchTasksInput) -> QueryAttempt[SearchTasksOutput]:
        """Validate filters, read a page, and classify its result."""
        output = SearchTasksOutput(page=input.page)
        try:
            _validate_numeric_id(input.workspace_id)
            for ids in (input.list_ids, input.folder_ids, input.space_ids):
                if len(ids) > 50:
                    raise ValueError("too many IDs")
                for identifier in ids:
                    _validate_numeric_id(identifier)
            if any(len(values) > 50 for values in (input.statuses, input.assignee_ids, input.tags)):
                raise ValueError("too many filters")
            if any(not status for status in input.statuses):
                raise ValueError("empty status")
            for status in input.statuses:
                _validate_status(status)
            _validate_users(input.assignee_ids)
            _validate_tags(input.tags)
            if input.order_by and input.order_by not in ("created", "updated", "due_date", "id"):
                raise ValueError("invalid orderBy")
            if input.page < 0 or input.page > 1000:
                raise ValueError("invalid page")
            _milliseconds(input.updated_after)
        except (TypeError, ValueError) as error:
            return _query_defect("searchTasks", output, str(error))
        credentials, cause = await self.client.resolve_credentials(call, "searchTasks")
        if cause:
            return _query_credentials_failure(output, cause)
        query: dict[str, str | list[str]] = {"page": str(input.page)}
        for key, values in (
            ("list_ids[]", input.list_ids),
            ("project_ids[]", input.folder_ids),
            ("space_ids[]", input.space_ids),
            ("statuses[]", input.statuses),
            ("assignees[]", tuple(str(number) for number in input.assignee_ids)),
            ("tags[]", input.tags),
        ):
            if values:
                query[key] = list(values)
        if input.updated_after:
            query["date_updated_gt"] = str(_milliseconds(input.updated_after))
        if input.is_closed_included:
            query["include_closed"] = "true"
        if input.are_subtasks_included:
            query["subtasks"] = "true"
        if input.order_by:
            query["order_by"] = input.order_by
        if input.is_reverse:
            query["reverse"] = "true"
        assert credentials is not None
        result = await self.client.exchange(
            "searchTasks", credentials, "GET", f"/team/{input.workspace_id}/task", query
        )
        receipt = self.client.receipt(call, input.workspace_id)
        if result.kind != "succeeded":
            return _query_failure(result, output, receipt)
        try:
            return QueryAttempt.branch_result(
                "searched", decode_search_page(result.body, input.page), receipt
            )
        except (TypeError, ValueError, KeyError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                output,
                receipt,
                failure("searchTasks", FailureKind.PROTOCOL, "ClickUp returned an invalid page of tasks"),
            )


class GetTaskOperation:
    """Read a ClickUp task with custom fields."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return generated task-read definition."""
        return definitions.GET_TASK_DEFINITION

    async def invoke(self, call: Call, input: GetTaskInput) -> QueryAttempt[Task]:
        """Read a task by ordinary or custom ID."""
        output = Task()
        try:
            _validate_task_id(input.task_id)
            if input.is_custom_task_id:
                _validate_numeric_id(input.workspace_id)
            elif input.workspace_id:
                raise ValueError("workspaceId is only used with isCustomTaskId")
        except ValueError as error:
            return _query_defect("getTask", output, str(error))
        credentials, cause = await self.client.resolve_credentials(call, "getTask")
        if cause:
            return _query_credentials_failure(output, cause)
        query = {"include_markdown_description": "true"}
        if input.is_custom_task_id:
            query.update({"custom_task_ids": "true", "team_id": input.workspace_id})
        assert credentials is not None
        result = await self.client.exchange("getTask", credentials, "GET", _task_path(input.task_id), query)
        receipt = self.client.receipt(call, input.task_id)
        if result.kind != "succeeded":
            return _query_failure(result, output, receipt)
        try:
            task = decode_task(decode_object(result.body), True)
            return QueryAttempt.branch_result("found", task, self.client.receipt(call, task.id))
        except (TypeError, ValueError, KeyError) as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                output,
                receipt,
                failure("getTask", FailureKind.PROTOCOL, f"ClickUp returned an invalid task: {error}"),
            )


class FindMemberByEmailOperation:
    """Find a member in an authorized Workspace."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return generated member lookup definition."""
        return definitions.FIND_MEMBER_BY_EMAIL_DEFINITION

    async def invoke(self, call: Call, input: FindMemberByEmailInput) -> QueryAttempt[Member]:
        """Read all authorized Workspaces and match one bare address."""
        output = Member()
        try:
            _validate_numeric_id(input.workspace_id)
            if not _bare_email(input.email):
                raise ValueError("email must be one bare email address")
        except ValueError as error:
            return _query_defect("findMemberByEmail", output, str(error))
        credentials, cause = await self.client.resolve_credentials(call, "findMemberByEmail")
        if cause:
            return _query_credentials_failure(output, cause)
        assert credentials is not None
        result = await self.client.exchange("findMemberByEmail", credentials, "GET", "/team")
        receipt = self.client.receipt(call, input.workspace_id)
        if result.kind != "succeeded":
            return _query_failure(result, output, receipt, False)
        try:
            teams = decode_object(result.body).get("teams")
            if not isinstance(teams, list):
                raise ValueError("missing teams")
            for team in teams:
                if team is None:
                    continue
                if not isinstance(team, dict):
                    raise ValueError("invalid team")
                flexible_id(team.get("id"))
                members = team.get("members")
                if members is None:
                    continue
                if not isinstance(members, list):
                    raise ValueError("invalid members")
                for entry in members:
                    if entry is None:
                        continue
                    if not isinstance(entry, dict):
                        raise ValueError("invalid member")
                    user = entry.get("user")
                    if user is None:
                        continue
                    if not isinstance(user, dict):
                        raise ValueError("invalid user")
                    flexible_integer(user.get("id"))
                    for name in ("username", "email"):
                        if user.get(name) is not None and type(user[name]) is not str:
                            raise ValueError(f"invalid member {name}")
                    role = user.get("role")
                    if role is not None and (type(role) is not int or not -(2**63) <= role < 2**63):
                        raise ValueError("invalid member role")
            for team in teams:
                if team is None:
                    continue
                if flexible_id(team.get("id")) != input.workspace_id:
                    continue
                members = team.get("members") or []
                for entry in members:
                    if entry is None:
                        continue
                    user = entry.get("user") or {}
                    email = user.get("email", "")
                    email = email or ""
                    if not _equal_fold(email.strip(), input.email):
                        continue
                    identifier = flexible_integer(user.get("id"))
                    if identifier <= 0:
                        raise ValueError("invalid member ID")
                    username = user.get("username")
                    role = user.get("role")
                    member = Member(
                        identifier,
                        username or "",
                        email.strip(),
                        role or None,
                        input.workspace_id,
                    )
                    return QueryAttempt.branch_result(
                        "found", member, self.client.receipt(call, str(identifier))
                    )
                return QueryAttempt.branch_result(
                    "notFound",
                    output,
                    receipt,
                    failure(
                        "findMemberByEmail",
                        FailureKind.NOT_FOUND,
                        "no member of the Workspace has the email address",
                    ),
                )
            return QueryAttempt.branch_result(
                "notFound",
                output,
                receipt,
                failure(
                    "findMemberByEmail",
                    FailureKind.NOT_FOUND,
                    "the token's user is not a member of the Workspace",
                ),
            )
        except (TypeError, ValueError, AttributeError, KeyError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                output,
                receipt,
                failure(
                    "findMemberByEmail",
                    FailureKind.PROTOCOL,
                    "ClickUp returned an invalid list of Workspaces",
                ),
            )


class UpdateTaskOperation:
    """Apply repeat-safe absolute task field changes."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated update definition."""
        return definitions.UPDATE_TASK_DEFINITION

    def idempotency_key(self, call_id: CallID, _input: UpdateTaskInput) -> IdempotencyKey:
        """Correlate the mutation with its Dex Call ID."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: UpdateTaskInput) -> MutationAttempt[UpdateTaskOutput]:
        """Validate, update, and decode one task."""
        output = UpdateTaskOutput()
        try:
            _validate_task_id(input.task_id)
            if (
                input.name is None
                and input.markdown_description is None
                and not input.status
                and not input.priority
                and input.due_at is None
                and not input.add_assignee_ids
                and not input.remove_assignee_ids
            ):
                raise ValueError("updateTask needs at least one change")
            if input.name is not None:
                _validate_name(input.name)
            if input.markdown_description is not None and len(input.markdown_description.encode()) > 65536:
                raise ValueError("description too long")
            _validate_status(input.status)
            _validate_priority(input.priority)
            _milliseconds(input.due_at)
            _validate_users(input.add_assignee_ids)
            _validate_users(input.remove_assignee_ids)
            if len(input.add_assignee_ids) + len(input.remove_assignee_ids) > 50 or set(
                input.add_assignee_ids
            ) & set(input.remove_assignee_ids):
                raise ValueError("invalid assignee changes")
        except (TypeError, ValueError) as error:
            return _mutation_defect("updateTask", output, str(error))
        credentials, cause = await self.client.resolve_credentials(call, "updateTask")
        if cause:
            return _mutation_credentials_failure(output, cause)
        payload: dict[str, Any] = {}
        if input.name is not None:
            payload["name"] = input.name
        if input.markdown_description is not None:
            payload["description" if input.markdown_description == "" else "markdown_content"] = (
                " " if input.markdown_description == "" else input.markdown_description
            )
        if input.status:
            payload["status"] = input.status
        if input.priority:
            payload["priority"] = input.priority
        if input.due_at:
            payload.update(due_date=_milliseconds(input.due_at), due_date_time=True)
        if input.add_assignee_ids or input.remove_assignee_ids:
            payload["assignees"] = {
                "add": list(input.add_assignee_ids),
                "rem": list(input.remove_assignee_ids),
            }
        assert credentials is not None
        result = await self.client.exchange(
            "updateTask", credentials, "PUT", _task_path(input.task_id), payload=payload
        )
        receipt = self.client.receipt(call, input.task_id)
        if result.kind != "succeeded":
            return _mutation_failure(result, output, receipt)
        try:
            task = decode_task(decode_object(result.body))
            return MutationAttempt.branch_result("updated", UpdateTaskOutput(task), receipt)
        except (TypeError, ValueError, KeyError):
            return MutationAttempt.branch_result(
                "invalidResponse",
                output,
                receipt,
                failure(
                    "updateTask",
                    FailureKind.PROTOCOL,
                    "ClickUp accepted the update but returned an invalid task",
                ),
            )


class UpdateTaskTagsOperation:
    """Apply repeat-safe task tag membership changes."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return generated tag update definition."""
        return definitions.UPDATE_TASK_TAGS_DEFINITION

    def idempotency_key(self, call_id: CallID, _input: UpdateTaskTagsInput) -> IdempotencyKey:
        """Correlate the mutation with its Dex Call ID."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: UpdateTaskTagsInput) -> MutationAttempt[UpdateTaskTagsOutput]:
        """Add and remove tags, then read their final state."""
        output = UpdateTaskTagsOutput(task_id=input.task_id)
        try:
            _validate_task_id(input.task_id)
            if not (1 <= len(input.add_tags) + len(input.remove_tags) <= 10):
                raise ValueError("addTags and removeTags need 1 to 10 tags together")
            _validate_tags(input.add_tags)
            _validate_tags(input.remove_tags)
            if set(input.add_tags) & set(input.remove_tags):
                raise ValueError("tag added and removed")
        except ValueError as error:
            return _mutation_defect("updateTaskTags", output, str(error))
        credentials, cause = await self.client.resolve_credentials(call, "updateTaskTags")
        if cause:
            return _mutation_credentials_failure(output, cause)
        assert credentials is not None
        receipt = self.client.receipt(call, input.task_id)
        deadline = asyncio.get_running_loop().time() + 25.0
        for method, tag in (("POST", value) for value in input.add_tags):
            path = _task_path(input.task_id) + "/tag/" + path_escape(tag)
            result = await _budgeted_exchange(
                self.client, deadline, credentials, method, path
            )
            if result.kind not in ("succeeded", "invalid"):
                return _mutation_failure(result, output, receipt)
            output = UpdateTaskTagsOutput(input.task_id, (), output.applied_changes + 1)
        for tag in input.remove_tags:
            path = _task_path(input.task_id) + "/tag/" + path_escape(tag)
            result = await _budgeted_exchange(
                self.client, deadline, credentials, "DELETE", path,
            )
            if result.kind not in ("succeeded", "invalid"):
                return _mutation_failure(result, output, receipt)
            output = UpdateTaskTagsOutput(input.task_id, (), output.applied_changes + 1)
        result = await _budgeted_exchange(
            self.client, deadline, credentials, "GET", _task_path(input.task_id)
        )
        if result.kind != "succeeded":
            return _mutation_failure(result, output, receipt)
        try:
            task = decode_task(decode_object(result.body))
            return MutationAttempt.branch_result(
                "updated", UpdateTaskTagsOutput(input.task_id, task.tags, output.applied_changes), receipt
            )
        except (TypeError, ValueError, KeyError):
            return MutationAttempt.branch_result(
                "invalidResponse",
                output,
                receipt,
                failure(
                    "updateTaskTags",
                    FailureKind.PROTOCOL,
                    "ClickUp applied the tag changes but returned an invalid task",
                ),
            )


@dataclass(frozen=True)
class _DispatchCheckpoint:
    clickup_dispatched_call_id: str = field(metadata={"dex_json_name": "clickupDispatchedCallId"})
    clickup_dispatched_at: int = field(metadata={"dex_json_name": "clickupDispatchedAt"})


async def _clear_dispatch(call: Call) -> None:
    assert call.context is not None
    try:
        await call.context.heartbeat()
    except Exception:
        # A failed clear keeps the checkpoint, so the next attempt reconciles.
        pass


def _prior_dispatch(call: Call) -> _DispatchCheckpoint | None:
    assert call.context is not None
    if not call.context.has_last_heartbeat_value():
        return None
    try:
        return call.context.get_last_heartbeat_value(_DispatchCheckpoint)
    except Exception:
        # An unreadable persisted marker still prohibits a duplicate unkeyed write.
        return _DispatchCheckpoint("", 0)


def _dispatch_time(client: Client) -> int:
    return int(client.now().timestamp() * 1000)


def _since(checkpoint: _DispatchCheckpoint) -> datetime | None:
    if checkpoint.clickup_dispatched_at <= 0:
        return None
    return datetime.fromtimestamp((checkpoint.clickup_dispatched_at - 300000) / 1000, UTC)


class CreateTaskOperation:
    """Create a task once, reconciling any ambiguous earlier send."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return generated create-task definition."""
        return definitions.CREATE_TASK_DEFINITION

    def idempotency_key(self, call_id: CallID, _input: CreateTaskInput) -> IdempotencyKey:
        """Correlate the write with its Dex Call ID."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: CreateTaskInput) -> MutationAttempt[CreateTaskOutput]:
        """Record a dispatch checkpoint before sending a task create."""
        output = CreateTaskOutput(list_id=input.list_id, name=input.name)
        try:
            _validate_numeric_id(input.list_id)
            _validate_name(input.name)
            if len(input.markdown_description.encode()) > 65536:
                raise ValueError("description too long")
            _validate_status(input.status)
            _validate_priority(input.priority)
            _milliseconds(input.due_at)
            _validate_users(input.assignee_ids)
            _validate_tags(input.tags)
            if len(input.tags) > 20 or len(input.custom_fields) > 50:
                raise ValueError("too many values")
            if input.parent_task_id:
                _validate_task_id(input.parent_task_id)
            field_ids = [
                field["id"] if isinstance(field, dict) else field.id for field in input.custom_fields
            ]
            if any(not task_id(identifier) for identifier in field_ids) or len(field_ids) != len(
                set(field_ids)
            ):
                raise ValueError("invalid custom field")
            for custom_field in input.custom_fields:
                custom_field_id = custom_field["id"] if isinstance(custom_field, dict) else custom_field.id
                if (
                    isinstance(custom_field, dict) and "value" not in custom_field
                    or not isinstance(custom_field, dict) and custom_field.value is None
                ):
                    raise ValueError(f"custom field {custom_field_id} needs a JSON value")
                custom_field_value = (
                    custom_field["value"] if isinstance(custom_field, dict) else custom_field.value
                )
                try:
                    json.dumps(custom_field_value, allow_nan=False)
                except (TypeError, ValueError) as error:
                    raise ValueError(f"custom field {custom_field_id} needs a JSON value") from error
        except (TypeError, ValueError) as error:
            return _mutation_defect("createTask", output, str(error))
        credentials, cause = await self.client.resolve_credentials(call, "createTask")
        if cause:
            return _mutation_credentials_failure(output, cause)
        checkpoint = _prior_dispatch(call)
        if checkpoint is not None:
            return await self._reconcile(call, credentials, input, output, checkpoint)
        assert call.context is not None
        try:
            await call.context.heartbeat(_DispatchCheckpoint(str(call.id), _dispatch_time(self.client)))
        except Exception:
            return MutationAttempt.retry(
                failure(
                    "createTask",
                    FailureKind.AVAILABILITY,
                    "Dex did not record the dispatch checkpoint; nothing was sent",
                )
            )
        payload: dict[str, Any] = {"name": input.name}
        if input.markdown_description:
            payload["markdown_content"] = input.markdown_description
        if input.status:
            payload["status"] = input.status
        if input.priority:
            payload["priority"] = input.priority
        if input.due_at:
            payload.update(due_date=_milliseconds(input.due_at), due_date_time=True)
        if input.assignee_ids:
            payload["assignees"] = list(input.assignee_ids)
        if input.tags:
            payload["tags"] = list(input.tags)
        if input.parent_task_id:
            payload["parent"] = input.parent_task_id
        if input.is_creator_notified:
            payload["notify_all"] = True
        if input.custom_fields:
            payload["custom_fields"] = [
                {
                    "id": field["id"] if isinstance(field, dict) else field.id,
                    "value": field["value"] if isinstance(field, dict) else field.value,
                }
                for field in input.custom_fields
            ]
        assert credentials is not None
        result = await self.client.exchange(
            "createTask", credentials, "POST", f"/list/{input.list_id}/task", payload=payload
        )
        receipt = self.client.receipt(call, input.list_id)
        if result.kind in ("not_sent", "rate_limited"):
            await _clear_dispatch(call)
            return MutationAttempt.retry(_exchange_failure(result), result.retry_after.total_seconds())
        if result.kind in ("unconfirmed", "invalid"):
            return MutationAttempt.retry(_exchange_failure(result), result.retry_after.total_seconds())
        if result.kind == "defect":
            await _clear_dispatch(call)
        if result.kind != "succeeded":
            return _mutation_failure(result, output, receipt, False)
        try:
            task = decode_task(decode_object(result.body))
            return MutationAttempt.branch_result(
                "created",
                CreateTaskOutput(input.list_id, input.name, task),
                self.client.receipt(call, task.id),
            )
        except (TypeError, ValueError, KeyError):
            return MutationAttempt.retry(
                failure(
                    "createTask",
                    FailureKind.PROTOCOL,
                    "ClickUp accepted the task but returned an invalid task, "
                    "so the next attempt reads the List back",
                )
            )

    async def _reconcile(
        self,
        call: Call,
        credentials: Any,
        input: CreateTaskInput,
        output: CreateTaskOutput,
        checkpoint: _DispatchCheckpoint,
    ) -> MutationAttempt[CreateTaskOutput]:
        since = _since(checkpoint)
        query: dict[str, str] = {"include_closed": "true", "subtasks": "true", "page": "0"}
        if since:
            query["date_created_gt"] = str(int(since.timestamp() * 1000))
        assert credentials is not None
        result = await self.client.exchange(
            "createTask", credentials, "GET", f"/list/{input.list_id}/task", query
        )
        receipt = self.client.receipt(call, input.list_id)
        if result.is_retryable:
            return MutationAttempt.retry(_exchange_failure(result), result.retry_after.total_seconds())
        if result.kind == "not_found":
            return MutationAttempt.branch_result("notFound", output, receipt, _exchange_failure(result))
        if result.kind != "succeeded":
            return MutationAttempt.uncertain(
                output,
                failure(
                    "createTask",
                    _exchange_failure(result).kind,
                    "an earlier attempt of this Step sent the create, and the List could not be read back: "
                    + _exchange_failure(result).message,
                ),
                receipt,
            )
        try:
            page = decode_search_page(result.body, 0)
        except (TypeError, ValueError, KeyError):
            return MutationAttempt.uncertain(
                output,
                failure(
                    "createTask",
                    FailureKind.PROTOCOL,
                    "an earlier attempt of this Step sent the create, and the List read back is invalid",
                ),
                receipt,
            )
        matches = [
            task
            for task in page.tasks
            if task.name.strip() == input.name.strip()
            and task.parent_task_id == input.parent_task_id
            and (since is None or task.created_at >= since)
        ]
        if len(matches) != 1:
            return MutationAttempt.uncertain(
                output,
                failure(
                    "createTask",
                    FailureKind.TRANSPORT,
                    "an earlier attempt of this Step sent the create without a confirmed outcome, "
                    f"and the List shows {len(matches)} tasks with the name created since, "
                    "so it is not sent again",
                ),
                receipt,
            )
        task = matches[0]
        return MutationAttempt.branch_result(
            "created",
            CreateTaskOutput(input.list_id, input.name, task, True),
            self.client.receipt(call, task.id),
        )


class AddCommentOperation:
    """Add a comment once, reconciling any ambiguous earlier send."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return generated comment definition."""
        return definitions.ADD_COMMENT_DEFINITION

    def idempotency_key(self, call_id: CallID, _input: AddCommentInput) -> IdempotencyKey:
        """Correlate the write with its Dex Call ID."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: AddCommentInput) -> MutationAttempt[AddCommentOutput]:
        """Record a dispatch checkpoint before sending a comment."""
        output = AddCommentOutput(task_id=input.task_id)
        try:
            _validate_task_id(input.task_id)
            if not input.text.strip() or len(input.text.encode()) > 32768:
                raise ValueError("text must be non-empty and at most 32768 bytes")
        except ValueError as error:
            return _mutation_defect("addComment", output, str(error))
        credentials, cause = await self.client.resolve_credentials(call, "addComment")
        if cause:
            return _mutation_credentials_failure(output, cause)
        checkpoint = _prior_dispatch(call)
        if checkpoint is not None:
            return await self._reconcile(call, credentials, input, output, checkpoint)
        assert call.context is not None
        try:
            await call.context.heartbeat(_DispatchCheckpoint(str(call.id), _dispatch_time(self.client)))
        except Exception:
            return MutationAttempt.retry(
                failure(
                    "addComment",
                    FailureKind.AVAILABILITY,
                    "Dex did not record the dispatch checkpoint; nothing was sent",
                )
            )
        assert credentials is not None
        result = await self.client.exchange(
            "addComment",
            credentials,
            "POST",
            _task_path(input.task_id) + "/comment",
            payload={"comment_text": input.text, "notify_all": input.is_creator_notified},
        )
        receipt = self.client.receipt(call, input.task_id)
        if result.kind in ("not_sent", "rate_limited"):
            await _clear_dispatch(call)
            return MutationAttempt.retry(_exchange_failure(result), result.retry_after.total_seconds())
        if result.kind in ("unconfirmed", "invalid"):
            return MutationAttempt.retry(_exchange_failure(result), result.retry_after.total_seconds())
        if result.kind == "defect":
            await _clear_dispatch(call)
        if result.kind != "succeeded":
            return _mutation_failure(result, output, receipt, False)
        try:
            wire = decode_object(result.body)
            comment_id = flexible_id(wire.get("id"))
            if not task_id(comment_id):
                raise ValueError("invalid comment ID")
            created_at = unix_milliseconds(wire.get("date"))
            return MutationAttempt.branch_result(
                "added",
                AddCommentOutput(input.task_id, comment_id, created_at),
                self.client.receipt(call, input.task_id, {"commentId": comment_id}),
            )
        except (TypeError, ValueError, KeyError):
            return MutationAttempt.retry(
                failure(
                    "addComment",
                    FailureKind.PROTOCOL,
                    "ClickUp accepted the comment but returned no valid comment ID, "
                    "so the next attempt reads the comments back",
                )
            )

    async def _reconcile(
        self,
        call: Call,
        credentials: Any,
        input: AddCommentInput,
        output: AddCommentOutput,
        checkpoint: _DispatchCheckpoint,
    ) -> MutationAttempt[AddCommentOutput]:
        assert credentials is not None
        result = await self.client.exchange(
            "addComment", credentials, "GET", _task_path(input.task_id) + "/comment"
        )
        receipt = self.client.receipt(call, input.task_id)
        if result.is_retryable:
            return MutationAttempt.retry(_exchange_failure(result), result.retry_after.total_seconds())
        if result.kind == "not_found":
            return MutationAttempt.branch_result("notFound", output, receipt, _exchange_failure(result))
        if result.kind != "succeeded":
            return MutationAttempt.uncertain(
                output,
                failure(
                    "addComment",
                    _exchange_failure(result).kind,
                    "an earlier attempt of this Step sent the comment, "
                    "and the task's comments could not be read back: " + _exchange_failure(result).message,
                ),
                receipt,
            )
        try:
            comments = decode_object(result.body).get("comments")
            if not isinstance(comments, list):
                raise ValueError("missing comments")
            since = _since(checkpoint)
            wanted = " ".join(input.text.split())
            matches = []
            for comment in comments:
                if not isinstance(comment, dict):
                    raise ValueError("invalid comment")
                created_at = unix_milliseconds(comment.get("date"))
                text = comment.get("comment_text", "")
                if not isinstance(text, str):
                    raise ValueError("invalid comment text")
                if " ".join(text.split()) == wanted and (
                    since is None or (created_at is not None and created_at >= since)
                ):
                    matches.append((flexible_id(comment.get("id")), created_at))
        except (TypeError, ValueError, KeyError):
            return MutationAttempt.uncertain(
                output,
                failure(
                    "addComment",
                    FailureKind.PROTOCOL,
                    "an earlier attempt of this Step sent the comment, "
                    "and the comments read back are invalid",
                ),
                receipt,
            )
        if len(matches) != 1 or not task_id(matches[0][0]):
            return MutationAttempt.uncertain(
                output,
                failure(
                    "addComment",
                    FailureKind.TRANSPORT,
                    "an earlier attempt of this Step sent the comment without a confirmed outcome, "
                    f"and the task shows {len(matches)} matching comments since, "
                    "so it is not sent again",
                ),
                receipt,
            )
        comment_id, created_at = matches[0]
        return MutationAttempt.branch_result(
            "added",
            AddCommentOutput(input.task_id, comment_id, created_at, True),
            self.client.receipt(call, input.task_id, {"commentId": comment_id}),
        )
