# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Asana Query and Mutation invocation with Go-compatible provider outcomes."""

import json
from dataclasses import replace
from datetime import UTC, timedelta
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
    DecisionKind,
    DispatchNotAllowedError,
    Fence,
    InsufficientAttemptTimeError,
)

from ._generated.definitions import (
    ADD_COMMENT_DEFINITION,
    CREATE_TASK_DEFINITION,
    GET_TASK_DEFINITION,
    LIST_TASKS_DEFINITION,
    UPDATE_TASK_DEFINITION,
)
from .client import Client, Exchange, data_object, failure, rejection
from .models import (
    AddCommentInput,
    AddCommentOutput,
    CreateTaskInput,
    CreateTaskOutput,
    CustomFieldValueInput,
    GetTaskInput,
    ListTasksInput,
    ListTasksOutput,
    Task,
    UpdateTaskInput,
    UpdateTaskOutput,
    UserReference,
)
from .task import (
    CREATED_STORY_FIELDS,
    CREATED_TASK_FIELDS,
    TASK_DETAIL_FIELDS,
    TASK_SUMMARY_FIELDS,
    assignee,
    custom_field_values,
    decode_task,
    due_date,
    gid,
    optional_gid,
    optional_time,
    plain_text,
    task_name,
)


def _read_outcome(
    client: Client, operation: str, subject: str, exchange: Exchange
) -> tuple[str, Failure | None, float]:
    delay = client.retry_after(exchange)
    if exchange.error == "notBuilt":
        return "defect", failure(operation, FailureKind.LOCAL_DEFECT, "Asana request could not be built"), 0
    if exchange.error:
        return (
            "retry",
            failure(operation, FailureKind.TRANSPORT, f"Asana {subject} is temporarily unavailable"),
            0,
        )
    status = exchange.status
    if status == 404:
        return (
            "notFound",
            failure(
                operation,
                FailureKind.NOT_FOUND,
                f"Asana {subject} was not found or is not visible to the connection",
            ),
            0,
        )
    if status == 429:
        return "retry", failure(operation, FailureKind.RATE_LIMIT, f"Asana rate limited the {subject}"), delay
    if status == 408 or status >= 500:
        return (
            "retry",
            failure(operation, FailureKind.AVAILABILITY, f"Asana {subject} is temporarily unavailable"),
            delay,
        )
    if 400 <= status < 500:
        return "providerRejected", rejection(operation, subject, status), 0
    if not 200 <= status < 300:
        return (
            "invalidResponse",
            failure(
                operation, FailureKind.PROTOCOL, f"Asana returned unexpected HTTP {status} for the {subject}"
            ),
            0,
        )
    if exchange.has_body_read_failed:
        return (
            "retry",
            failure(operation, FailureKind.TRANSPORT, f"Asana {subject} response was interrupted"),
            0,
        )
    if exchange.is_body_too_large:
        return (
            "invalidResponse",
            failure(
                operation,
                FailureKind.RESPONSE_TOO_LARGE,
                f"Asana {subject} response exceeds the configured size limit",
            ),
            0,
        )
    if exchange.reflects_secret:
        return (
            "invalidResponse",
            failure(
                operation, FailureKind.PROTOCOL, f"Asana {subject} response contains credential material"
            ),
            0,
        )
    return "success", None, 0


def _write_outcome(
    client: Client,
    operation: str,
    subject: str,
    exchange: Exchange,
    repeatable: bool,
) -> tuple[str, Failure | None, float]:
    delay = client.retry_after(exchange)
    if exchange.error == "notBuilt":
        return "defect", failure(operation, FailureKind.LOCAL_DEFECT, "Asana request could not be built"), 0
    if exchange.error:
        if repeatable:
            return (
                "retry",
                failure(
                    operation,
                    FailureKind.TRANSPORT,
                    f"Asana {subject} outcome is unknown, so the update is sent again",
                ),
                0,
            )
        if exchange.error == "beforeSend":
            return (
                "retry",
                failure(
                    operation,
                    FailureKind.TRANSPORT,
                    f"Asana could not be reached, so no {subject} was written",
                ),
                0,
            )
        return (
            "uncertain",
            failure(operation, FailureKind.TRANSPORT, f"Asana {subject} outcome is unknown"),
            0,
        )
    status = exchange.status
    if 200 <= status < 300:
        if exchange.has_body_read_failed:
            kind = "retry" if repeatable else "uncertain"
            return (
                kind,
                failure(operation, FailureKind.TRANSPORT, f"Asana {subject} response was interrupted"),
                0,
            )
        if exchange.is_body_too_large:
            kind = "invalidResponse" if repeatable else "uncertain"
            return (
                kind,
                failure(
                    operation,
                    FailureKind.RESPONSE_TOO_LARGE,
                    f"Asana {subject} response exceeds the configured size limit",
                ),
                0,
            )
        if exchange.reflects_secret:
            kind = "invalidResponse" if repeatable else "uncertain"
            return (
                kind,
                failure(
                    operation, FailureKind.PROTOCOL, f"Asana {subject} response contains credential material"
                ),
                0,
            )
        return "success", None, 0
    if status == 429:
        suffix = "" if repeatable else " before writing it"
        return (
            "retry",
            failure(operation, FailureKind.RATE_LIMIT, f"Asana rate limited the {subject}{suffix}"),
            delay,
        )
    if status == 503 or repeatable and (status == 408 or status >= 500):
        return (
            "retry",
            failure(operation, FailureKind.AVAILABILITY, f"Asana {subject} is temporarily unavailable"),
            delay,
        )
    if status == 404:
        return "notFound", rejection(operation, subject, status), 0
    if 400 <= status < 500 and status not in (408, 429):
        return "providerRejected", rejection(operation, subject, status), 0
    if repeatable:
        return (
            "invalidResponse",
            failure(
                operation, FailureKind.PROTOCOL, f"Asana returned unexpected HTTP {status} for the {subject}"
            ),
            0,
        )
    return (
        "uncertain",
        failure(
            operation, FailureKind.AVAILABILITY, f"Asana {subject} outcome is unknown after HTTP {status}"
        ),
        0,
    )


def _list_query(input_value: ListTasksInput) -> dict[str, str]:
    project_id = optional_gid(input_value.project_id, "projectId")
    section_id = optional_gid(input_value.section_id, "sectionId")
    if bool(project_id) == bool(section_id):
        raise ValueError("set exactly one of projectId and sectionId")
    if input_value.is_incomplete_only and input_value.completed_since is not None:
        raise ValueError("set at most one of isIncompleteOnly and completedSince")
    page_size = input_value.page_size or 50
    if page_size < 1 or page_size > 100:
        raise ValueError("pageSize must be between 1 and 100")
    query = {
        "opt_fields": TASK_SUMMARY_FIELDS,
        "limit": str(page_size),
        "project" if project_id else "section": project_id or section_id,
    }
    if input_value.is_incomplete_only:
        query["completed_since"] = "now"
    elif input_value.completed_since is not None:
        query["completed_since"] = (
            input_value.completed_since.astimezone(UTC).isoformat().replace("+00:00", "Z")
        )
    if input_value.modified_since is not None:
        query["modified_since"] = (
            input_value.modified_since.astimezone(UTC).isoformat().replace("+00:00", "Z")
        )
    offset = input_value.offset.strip()
    if offset:
        if not _valid_offset(offset):
            raise ValueError("offset must be the token from a previous task page")
        query["offset"] = offset
    return query


def _valid_offset(value: str) -> bool:
    return (
        bool(value)
        and len(value.encode()) <= 4096
        and all(33 <= ord(character) <= 126 for character in value)
    )


def _custom_inputs(items: tuple[CustomFieldValueInput, ...]) -> tuple[CustomFieldValueInput, ...]:
    return tuple(
        CustomFieldValueInput(
            **{
                {
                    "customFieldId": "custom_field_id",
                    "enumOptionId": "enum_option_id",
                    "multiEnumOptionIds": "multi_enum_option_ids",
                }.get(name, name): value
                for name, value in item.items()
            }
        )
        if isinstance(item, dict)
        else item
        for item in items
    )


def _create_fields(input_value: CreateTaskInput) -> dict[str, Any]:
    name = task_name(input_value.name)
    project_id = optional_gid(input_value.project_id, "projectId")
    section_id = optional_gid(input_value.section_id, "sectionId")
    workspace_id = optional_gid(input_value.workspace_id, "workspaceId")
    if bool(project_id) == bool(workspace_id):
        raise ValueError("set exactly one of projectId and workspaceId")
    if section_id and not project_id:
        raise ValueError("sectionId requires projectId")
    fields: dict[str, Any] = {"name": name}
    if plain_text(input_value.notes, "notes", False).strip():
        fields["notes"] = input_value.notes
    if section_id:
        fields["memberships"] = [{"project": project_id, "section": section_id}]
    elif project_id:
        fields["projects"] = [project_id]
    if workspace_id:
        fields["workspace"] = workspace_id
    if input_value.assignee_id.strip():
        fields["assignee"] = assignee(input_value.assignee_id, "assigneeId")
    if input_value.due_on.strip():
        fields["due_on"] = due_date(input_value.due_on, "dueOn")
    if custom := custom_field_values(_custom_inputs(input_value.custom_fields)):
        fields["custom_fields"] = custom
    return fields


def _update_fields(input_value: UpdateTaskInput) -> tuple[str, str, dict[str, Any]]:
    task_id = gid(input_value.task_id, "taskId")
    section_id = optional_gid(input_value.section_id, "sectionId")
    fields: dict[str, Any] = {}
    if input_value.is_completed is not None:
        fields["completed"] = input_value.is_completed
    if input_value.assignee_id is not None:
        fields["assignee"] = (
            assignee(input_value.assignee_id, "assigneeId") if input_value.assignee_id.strip() else None
        )
    if input_value.due_on is not None:
        fields["due_on"] = due_date(input_value.due_on, "dueOn") if input_value.due_on.strip() else None
    if custom := custom_field_values(_custom_inputs(input_value.custom_fields)):
        fields["custom_fields"] = custom
    if not fields and not section_id:
        raise ValueError("set at least one of isCompleted, assigneeId, dueOn, customFields, and sectionId")
    return task_id, section_id, fields


def _task_from_exchange(exchange: Exchange) -> Task:
    return decode_task(data_object(exchange), True)


class ListTasksOperation:
    """Read one bounded page of Asana project or section tasks."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Asana client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the stable task list Query definition."""
        return LIST_TASKS_DEFINITION

    async def invoke(self, call: Call, input: ListTasksInput) -> QueryAttempt[ListTasksOutput]:
        """Classify one task page request and decode its bounded output."""
        empty = ListTasksOutput()
        try:
            query = _list_query(input)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", empty, Receipt(), failure("listTasks", FailureKind.VALIDATION, str(error))
            )
        token = await self.client.token(call)
        if token is None:
            return QueryAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                failure(
                    "listTasks", FailureKind.AUTHENTICATION, "Asana connection credentials are unavailable"
                ),
            )
        exchange = await self.client.exchange(call, token, "GET", "/tasks", query)
        route, problem, delay = _read_outcome(self.client, "listTasks", "task list", exchange)
        if route == "retry":
            assert problem is not None
            return QueryAttempt.retry(problem, delay)
        receipt = self.client.receipt(call)
        if route != "success":
            return QueryAttempt.branch_result(route, empty, receipt, problem)
        try:
            page = json.loads(exchange.body)
            if not isinstance(page, dict) or not isinstance(page.get("data"), list):
                raise ValueError("response has no data array")
            if len(page["data"]) > 100:
                raise ValueError("page holds more tasks than requested")
            tasks = tuple(decode_task(resource, False) for resource in page["data"])
            next_page = page.get("next_page") or {}
            next_offset = next_page.get("offset", "") if isinstance(next_page, dict) else ""
            if next_offset and not _valid_offset(next_offset):
                raise ValueError("next page offset is invalid")
        except (ValueError, TypeError, KeyError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                failure("listTasks", FailureKind.PROTOCOL, "Asana returned an invalid task page"),
            )
        return QueryAttempt.branch_result("listed", ListTasksOutput(tasks, next_offset), receipt)


class GetTaskOperation:
    """Read one Asana task with its details and memberships."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Asana client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the stable task detail Query definition."""
        return GET_TASK_DEFINITION

    async def invoke(self, call: Call, input: GetTaskInput) -> QueryAttempt[Task]:
        """Classify one task read and decode its detail fields."""
        empty = Task()
        try:
            task_id = gid(input.task_id, "taskId")
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", empty, Receipt(), failure("getTask", FailureKind.VALIDATION, str(error))
            )
        token = await self.client.token(call)
        if token is None:
            return QueryAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                failure(
                    "getTask", FailureKind.AUTHENTICATION, "Asana connection credentials are unavailable"
                ),
            )
        exchange = await self.client.exchange(
            call, token, "GET", f"/tasks/{task_id}", {"opt_fields": TASK_DETAIL_FIELDS}
        )
        route, problem, delay = _read_outcome(self.client, "getTask", "task", exchange)
        if route == "retry":
            assert problem is not None
            return QueryAttempt.retry(problem, delay)
        receipt = self.client.receipt(call, task_id)
        if route != "success":
            return QueryAttempt.branch_result(route, empty, receipt, problem)
        try:
            return QueryAttempt.branch_result("found", _task_from_exchange(exchange), receipt)
        except ValueError:
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                failure("getTask", FailureKind.PROTOCOL, "Asana returned an invalid task"),
            )


class CreateTaskOperation:
    """Create one task with a heartbeat fence against duplicate POSTs."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Asana client is required")
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the stable task creation Mutation definition."""
        return CREATE_TASK_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreateTaskInput) -> IdempotencyKey:
        """Record the Call ID; Asana accepts no provider idempotency key."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: CreateTaskInput) -> MutationAttempt[CreateTaskOutput]:
        """Create once, retry proven-unsent attempts, or read back by checkpoint ID."""
        requested = CreateTaskOutput(
            name=input.name.strip(),
            project_id=input.project_id.strip(),
            section_id=input.section_id.strip(),
            workspace_id=input.workspace_id.strip(),
        )
        try:
            fields = _create_fields(input)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", requested, Receipt(), failure("createTask", FailureKind.VALIDATION, str(error))
            )
        fence = Fence(call, timedelta(seconds=30))
        decision = await fence.decide()
        if decision.kind == DecisionKind.FAIL_CLOSED:
            return MutationAttempt.uncertain(
                requested,
                failure(
                    "createTask", FailureKind.LOCAL_DEFECT, "Asana task dispatch checkpoint is unavailable"
                ),
                self.client.receipt(call),
            )
        task_id = decision.marker.provider_id if decision.marker else ""
        if task_id:
            try:
                gid(task_id, "taskId")
            except ValueError:
                return MutationAttempt.uncertain(
                    requested,
                    failure(
                        "createTask", FailureKind.PROTOCOL, "Asana task checkpoint has an invalid task ID"
                    ),
                    self.client.receipt(call),
                )
        if decision.kind == DecisionKind.READ_BACK_REQUIRED and not task_id:
            return MutationAttempt.uncertain(
                requested,
                failure(
                    "createTask",
                    FailureKind.TRANSPORT,
                    "Asana task dispatch checkpoint does not permit another create",
                ),
                self.client.receipt(call),
            )
        token = await self.client.token(call)
        if token is None:
            authentication_problem = failure(
                "createTask", FailureKind.AUTHENTICATION, "Asana connection credentials are unavailable"
            )
            return (
                MutationAttempt.retry(authentication_problem)
                if task_id
                else MutationAttempt.branch_result("defect", requested, Receipt(), authentication_problem)
            )
        if task_id:
            return await self._read_back(call, token, requested, task_id)
        try:
            exchange = await fence.dispatch(
                lambda _deadline: self.client.exchange(
                    call,
                    token,
                    "POST",
                    "/tasks",
                    {"opt_fields": CREATED_TASK_FIELDS},
                    {"data": fields},
                )
            )
        except (CheckpointError, InsufficientAttemptTimeError):
            return MutationAttempt.retry(
                failure(
                    "createTask",
                    FailureKind.AVAILABILITY,
                    "Asana task dispatch checkpoint could not be recorded",
                )
            )
        except (TimeoutError, DispatchNotAllowedError):
            return MutationAttempt.uncertain(
                requested,
                failure("createTask", FailureKind.TRANSPORT, "Asana task outcome is unknown"),
                self.client.receipt(call),
            )
        route, problem, delay = _write_outcome(self.client, "createTask", "task", exchange, False)
        receipt = self.client.receipt(call)
        if route == "retry":
            try:
                await fence.record_can_resend()
            except Exception:
                return MutationAttempt.uncertain(
                    requested,
                    failure(
                        "createTask", FailureKind.LOCAL_DEFECT, "Asana task retry checkpoint is unavailable"
                    ),
                    receipt,
                )
            assert problem is not None
            return MutationAttempt.retry(problem, delay)
        if route == "uncertain":
            assert problem is not None
            return MutationAttempt.uncertain(requested, problem, receipt)
        if route != "success":
            return MutationAttempt.branch_result(
                "providerRejected" if route == "notFound" else route, requested, receipt, problem
            )
        try:
            resource = data_object(exchange)
            task_id = gid(resource.get("gid", ""), "task gid")
        except (ValueError, TypeError):
            return MutationAttempt.uncertain(
                requested,
                failure(
                    "createTask",
                    FailureKind.PROTOCOL,
                    "Asana accepted the task but returned an unusable task reference",
                ),
                receipt,
            )
        try:
            await fence.record_provider_id(task_id)
        except Exception:
            pass
        created_at = None
        try:
            created_at = optional_time(resource.get("created_at"))
        except ValueError:
            pass
        output = replace(
            requested,
            task_id=task_id,
            permalink_url=resource.get("permalink_url") or "",
            created_at=created_at,
        )
        return MutationAttempt.branch_result("created", output, self.client.receipt(call, task_id))

    async def _read_back(
        self, call: Call, token: str, requested: CreateTaskOutput, task_id: str
    ) -> MutationAttempt[CreateTaskOutput]:
        exchange = await self.client.exchange(
            call, token, "GET", f"/tasks/{gid(task_id, 'taskId')}", {"opt_fields": CREATED_TASK_FIELDS}
        )
        route, problem, delay = _read_outcome(self.client, "createTask", "task", exchange)
        if route == "retry":
            assert problem is not None
            return MutationAttempt.retry(problem, delay)
        receipt = self.client.receipt(call, task_id)
        if route != "success":
            return MutationAttempt.uncertain(
                requested,
                failure("createTask", FailureKind.PROTOCOL, "Asana task could not be confirmed by ID"),
                receipt,
            )
        try:
            resource = data_object(exchange)
            if resource.get("gid") != task_id:
                raise ValueError("task ID differs")
        except ValueError:
            return MutationAttempt.uncertain(
                requested,
                failure(
                    "createTask",
                    FailureKind.PROTOCOL,
                    "Asana task read-back did not match the checkpointed ID",
                ),
                receipt,
            )
        created_at = None
        try:
            created_at = optional_time(resource.get("created_at"))
        except ValueError:
            pass
        output = replace(
            requested,
            task_id=task_id,
            permalink_url=resource.get("permalink_url") or "",
            created_at=created_at,
        )
        return MutationAttempt.branch_result("created", output, receipt)


class UpdateTaskOperation:
    """Apply repeatable task updates and optional section moves."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Asana client is required")
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the stable task update Mutation definition."""
        return UPDATE_TASK_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpdateTaskInput) -> IdempotencyKey:
        """Record one stable Call ID for this absolute update."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: UpdateTaskInput) -> MutationAttempt[UpdateTaskOutput]:
        """Move to a section, update absolute fields, then return the task."""
        output = UpdateTaskOutput(task_id=input.task_id.strip())
        try:
            task_id, section_id, fields = _update_fields(input)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", output, Receipt(), failure("updateTask", FailureKind.VALIDATION, str(error))
            )
        token = await self.client.token(call)
        if token is None:
            return MutationAttempt.branch_result(
                "defect",
                output,
                Receipt(),
                failure(
                    "updateTask", FailureKind.AUTHENTICATION, "Asana connection credentials are unavailable"
                ),
            )
        receipt = self.client.receipt(call, task_id)
        if section_id:
            exchange = await self.client.exchange(
                call, token, "POST", f"/sections/{section_id}/addTask", payload={"data": {"task": task_id}}
            )
            result = self._write_result(call, output, receipt, exchange, "section move")
            if result is not None:
                return result
            output = replace(output, is_moved_to_section=True)
        if fields:
            exchange = await self.client.exchange(
                call, token, "PUT", f"/tasks/{task_id}", {"opt_fields": TASK_DETAIL_FIELDS}, {"data": fields}
            )
            result = self._write_result(call, output, receipt, exchange, "task update")
            if result is not None:
                return result
            try:
                task = _task_from_exchange(exchange)
                if task.id != task_id:
                    raise ValueError("task ID differs")
            except ValueError:
                return MutationAttempt.branch_result(
                    "invalidResponse",
                    output,
                    receipt,
                    failure(
                        "updateTask", FailureKind.PROTOCOL, "Asana returned an invalid task after the update"
                    ),
                )
            return MutationAttempt.branch_result("updated", replace(output, task=task), receipt)
        exchange = await self.client.exchange(
            call, token, "GET", f"/tasks/{task_id}", {"opt_fields": TASK_DETAIL_FIELDS}
        )
        route, problem, delay = _read_outcome(self.client, "updateTask", "task", exchange)
        if route == "retry":
            assert problem is not None
            return MutationAttempt.retry(problem, delay)
        if route != "success":
            return MutationAttempt.branch_result(route, output, receipt, problem)
        try:
            task = _task_from_exchange(exchange)
        except ValueError:
            return MutationAttempt.branch_result(
                "invalidResponse",
                output,
                receipt,
                failure("updateTask", FailureKind.PROTOCOL, "Asana returned an invalid task"),
            )
        return MutationAttempt.branch_result("updated", replace(output, task=task), receipt)

    def _write_result(
        self, call: Call, output: UpdateTaskOutput, receipt: Receipt, exchange: Exchange, subject: str
    ) -> MutationAttempt[UpdateTaskOutput] | None:
        route, problem, delay = _write_outcome(self.client, "updateTask", subject, exchange, True)
        if route == "success":
            return None
        if route == "retry":
            assert problem is not None
            return MutationAttempt.retry(problem, delay)
        return MutationAttempt.branch_result(route, output, receipt, problem)


class AddCommentOperation:
    """Add one comment story with a heartbeat fence against duplicate POSTs."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Asana client is required")
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the stable comment Mutation definition."""
        return ADD_COMMENT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: AddCommentInput) -> IdempotencyKey:
        """Record the Call ID without sending it as an Asana header."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: AddCommentInput) -> MutationAttempt[AddCommentOutput]:
        """Add once, retry proven-unsent attempts, or read back a story ID."""
        requested = AddCommentOutput(task_id=input.task_id.strip())
        try:
            task_id = gid(input.task_id, "taskId")
            plain_text(input.text, "text", True)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", requested, Receipt(), failure("addComment", FailureKind.VALIDATION, str(error))
            )
        fence = Fence(call, timedelta(seconds=30))
        decision = await fence.decide()
        if decision.kind == DecisionKind.FAIL_CLOSED:
            return MutationAttempt.uncertain(
                requested,
                failure(
                    "addComment", FailureKind.LOCAL_DEFECT, "Asana comment dispatch checkpoint is unavailable"
                ),
                self.client.receipt(call),
            )
        story_id = decision.marker.provider_id if decision.marker else ""
        if story_id:
            try:
                gid(story_id, "storyId")
            except ValueError:
                return MutationAttempt.uncertain(
                    requested,
                    failure(
                        "addComment", FailureKind.PROTOCOL, "Asana comment checkpoint has an invalid story ID"
                    ),
                    self.client.receipt(call),
                )
        if decision.kind == DecisionKind.READ_BACK_REQUIRED and not story_id:
            return MutationAttempt.uncertain(
                requested,
                failure(
                    "addComment",
                    FailureKind.TRANSPORT,
                    "Asana comment dispatch checkpoint does not permit another comment",
                ),
                self.client.receipt(call),
            )
        token = await self.client.token(call)
        if token is None:
            authentication_problem = failure(
                "addComment", FailureKind.AUTHENTICATION, "Asana connection credentials are unavailable"
            )
            return (
                MutationAttempt.retry(authentication_problem)
                if story_id
                else MutationAttempt.branch_result("defect", requested, Receipt(), authentication_problem)
            )
        if story_id:
            return await self._read_back(call, token, requested, story_id)
        try:
            exchange = await fence.dispatch(
                lambda _deadline: self.client.exchange(
                    call,
                    token,
                    "POST",
                    f"/tasks/{task_id}/stories",
                    {"opt_fields": CREATED_STORY_FIELDS},
                    {"data": {"text": input.text}},
                )
            )
        except (CheckpointError, InsufficientAttemptTimeError):
            return MutationAttempt.retry(
                failure(
                    "addComment",
                    FailureKind.AVAILABILITY,
                    "Asana comment dispatch checkpoint could not be recorded",
                )
            )
        except (TimeoutError, DispatchNotAllowedError):
            return MutationAttempt.uncertain(
                requested,
                failure("addComment", FailureKind.TRANSPORT, "Asana comment outcome is unknown"),
                self.client.receipt(call),
            )
        route, problem, delay = _write_outcome(self.client, "addComment", "comment", exchange, False)
        receipt = self.client.receipt(call)
        if route == "retry":
            try:
                await fence.record_can_resend()
            except Exception:
                return MutationAttempt.uncertain(
                    requested,
                    failure(
                        "addComment",
                        FailureKind.LOCAL_DEFECT,
                        "Asana comment retry checkpoint is unavailable",
                    ),
                    receipt,
                )
            assert problem is not None
            return MutationAttempt.retry(problem, delay)
        if route == "uncertain":
            assert problem is not None
            return MutationAttempt.uncertain(requested, problem, receipt)
        if route != "success":
            return MutationAttempt.branch_result(route, requested, receipt, problem)
        try:
            resource = data_object(exchange)
            story_id = gid(resource.get("gid", ""), "story gid")
        except (ValueError, TypeError):
            return MutationAttempt.uncertain(
                requested,
                failure(
                    "addComment",
                    FailureKind.PROTOCOL,
                    "Asana accepted the comment but returned an unusable story reference",
                ),
                receipt,
            )
        try:
            await fence.record_provider_id(story_id)
        except Exception:
            pass
        output = self._story_output(requested, resource, story_id)
        return MutationAttempt.branch_result("added", output, self.client.receipt(call, story_id))

    async def _read_back(
        self, call: Call, token: str, requested: AddCommentOutput, story_id: str
    ) -> MutationAttempt[AddCommentOutput]:
        exchange = await self.client.exchange(
            call, token, "GET", f"/stories/{gid(story_id, 'storyId')}", {"opt_fields": CREATED_STORY_FIELDS}
        )
        route, problem, delay = _read_outcome(self.client, "addComment", "comment", exchange)
        if route == "retry":
            assert problem is not None
            return MutationAttempt.retry(problem, delay)
        receipt = self.client.receipt(call, story_id)
        if route != "success":
            return MutationAttempt.uncertain(
                requested,
                failure("addComment", FailureKind.PROTOCOL, "Asana comment could not be confirmed by ID"),
                receipt,
            )
        try:
            resource = data_object(exchange)
            if resource.get("gid") != story_id:
                raise ValueError("story ID differs")
        except ValueError:
            return MutationAttempt.uncertain(
                requested,
                failure(
                    "addComment",
                    FailureKind.PROTOCOL,
                    "Asana comment read-back did not match the checkpointed ID",
                ),
                receipt,
            )
        return MutationAttempt.branch_result(
            "added", self._story_output(requested, resource, story_id), receipt
        )

    def _story_output(
        self, requested: AddCommentOutput, resource: dict[str, Any], story_id: str
    ) -> AddCommentOutput:
        author_resource = resource.get("created_by")
        author = None
        if isinstance(author_resource, dict):
            try:
                author = UserReference(
                    gid(author_resource.get("gid", ""), "author gid"), author_resource.get("name", "")
                )
            except (ValueError, TypeError):
                pass
        created_at = None
        try:
            created_at = optional_time(resource.get("created_at"))
        except ValueError:
            pass
        return replace(requested, comment_id=story_id, author=author, created_at=created_at)
