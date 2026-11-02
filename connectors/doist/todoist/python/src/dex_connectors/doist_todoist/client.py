# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Todoist API v1 HTTP client and Go-compatible operation attempts."""

import asyncio
import hashlib
import json
import re
import time
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any
from urllib.parse import urlencode

import httpx
from dex_connectors.sdk import (
    Call,
    CallID,
    CredentialProvider,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    QueryAttempt,
    QueryDefinition,
    ReauthorizationRequiredError,
    Receipt,
)
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    RequestTrace,
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import (
    CREATE_TASK_DEFINITION,
    DELETE_TASK_DEFINITION,
    GET_TASK_DEFINITION,
    LIST_TASKS_DEFINITION,
    UPDATE_TASK_CONTENT_DEFINITION,
)
from .models import (
    CreateTaskInput,
    DeleteTaskInput,
    DeleteTaskOutcome,
    GetTaskInput,
    ListTasksInput,
    Task,
    TaskPage,
    UpdateTaskInput,
)

_TASK_ID = re.compile(r"^[A-Za-z0-9_-]{1,128}$")
_CURSOR = re.compile(r"^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$")
_REQUEST_ID = re.compile(r"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")
_GO_WHITESPACE = (
    " \t\n\v\f\r\u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007"
    "\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000"
)


class _RequestDispatchTrace(RequestTrace):
    def __init__(self) -> None:
        super().__init__()
        self.connection_started = False

    async def observe(self, event: str, info: dict[str, Any]) -> None:
        if event.endswith("connect_tcp.started") or event.endswith("connect_unix_socket.started"):
            self.connection_started = True
        await super().observe(event, info)

    @property
    def connect_timed_out_before_send(self) -> bool:
        return self.connection_started and not self.request_headers_started


@dataclass(frozen=True, slots=True)
class _Exchange:
    status: int = 0
    body: bytes = b""
    failure: Failure | None = None
    retry_after: float = 0
    transient: bool = False
    undispatched: bool = False
    local_defect: bool = False


@dataclass(frozen=True, slots=True)
class _AttemptBudget:
    write_deadline: float
    attempt_deadline: float
    minimum_request_seconds: float


def _attempt_budget(call: Call) -> _AttemptBudget:
    available = 30.0
    if call.context is not None and call.context.deadline is not None:
        available = min(available, (call.context.deadline - datetime.now(UTC)).total_seconds())
    available = max(0.0, available)
    started = time.monotonic()
    return _AttemptBudget(started + available * 7 / 12, started + available * 5 / 6, available / 10)


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "todoist", operation, message)


def _valid_task_id(value: object) -> bool:
    return isinstance(value, str) and _TASK_ID.fullmatch(value) is not None


def _valid_cursor(value: object) -> bool:
    return isinstance(value, str) and (
        not value or len(value) <= 4096 and _CURSOR.fullmatch(value) is not None
    )


def _valid_text(value: str) -> bool:
    try:
        value.encode("utf-8")
        return True
    except (UnicodeEncodeError, AttributeError):
        return False


def _decode_task(body: bytes) -> Task | None:
    try:
        data = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return None
    if not isinstance(data, dict):
        return None
    if not _valid_task_id(data.get("id")) or not _valid_task_id(data.get("project_id")):
        return None
    if not isinstance(data.get("content"), str) or not isinstance(data.get("description"), str):
        return None
    if not isinstance(data.get("checked"), bool):
        return None
    return Task(data["id"], data["project_id"], data["content"], data["description"], data["checked"])


def _decode_page(body: bytes, input: ListTasksInput) -> TaskPage | None:
    try:
        data = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return None
    if not isinstance(data, dict) or not isinstance(data.get("results"), list):
        return None
    if "next_cursor" not in data:
        return None
    cursor = data["next_cursor"]
    if cursor is None:
        cursor = ""
    if not _valid_cursor(cursor) or cursor and cursor == input.cursor:
        return None
    assert isinstance(cursor, str)
    raw_tasks = data["results"]
    if len(raw_tasks) > (input.limit or 50):
        return None
    tasks = []
    seen = set()
    for raw_task in raw_tasks:
        task = _decode_task(json.dumps(raw_task).encode())
        if task is None or task.id in seen or input.project_id and task.project_id != input.project_id:
            return None
        seen.add(task.id)
        tasks.append(task)
    return TaskPage(tuple(tasks), cursor)


class Client:
    """Resolve credentials per call and send redirect-safe provider requests."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Todoist configuration and credential source are required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("Todoist timeout and response bound must be positive")
        self.base_url = validate_base_url(config.base_url or "https://api.todoist.com/api/v1")
        self.max_response_bytes = config.max_response_bytes or 1_048_576
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds or 15)

    def get_task(self) -> "GetTaskOperation":
        """Return the active-task Query."""
        return GetTaskOperation(self)

    def update_task_content(self) -> "UpdateTaskOperation":
        """Return the absolute-field replacement Mutation."""
        return UpdateTaskOperation(self)

    def list_tasks(self) -> "ListTasksOperation":
        """Return the one-page task Query."""
        return ListTasksOperation(self)

    def create_task(self) -> "CreateTaskOperation":
        """Return the task-creation Mutation."""
        return CreateTaskOperation(self)

    def delete_task(self) -> "DeleteTaskOperation":
        """Return the task-deletion Mutation."""
        return DeleteTaskOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider transport after Worker shutdown."""
        await self.http_client.aclose()

    async def _authorize(self, call: Call, operation: str) -> tuple[str | None, _Exchange | None]:
        try:
            credentials = await self.credentials.resolve(call)
        except (ValueError, LookupError, ReauthorizationRequiredError):
            return None, _Exchange(
                failure=_failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "connection credentials are missing or need reauthorization",
                ),
                local_defect=True,
            )
        except OSError:
            return None, _Exchange(
                failure=_failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "connection credentials could not be read, so the request was not sent",
                ),
                transient=True,
                undispatched=True,
            )
        token = credentials.api_token.reveal()
        if not is_header_safe_credential(token):
            return None, _Exchange(
                failure=_failure(operation, FailureKind.AUTHENTICATION, "connection credentials are invalid"),
                local_defect=True,
            )
        return token, None

    async def _send(
        self,
        operation: str,
        token: str,
        method: str,
        path: str,
        payload: dict[str, Any] | None = None,
        request_id: str = "",
        budget: _AttemptBudget | None = None,
        is_write: bool = False,
    ) -> _Exchange:
        if budget is None:
            return await self._send_raw(operation, token, method, path, payload, request_id)
        deadline = budget.write_deadline if is_write else budget.attempt_deadline
        remaining = deadline - time.monotonic()
        if budget.minimum_request_seconds <= 0 or remaining < budget.minimum_request_seconds:
            return _Exchange(
                failure=_failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "too little attempt time remained, so the request was not sent",
                ),
                transient=True,
                undispatched=True,
            )
        try:
            trace = _RequestDispatchTrace()
            return await asyncio.wait_for(
                self._send_raw(operation, token, method, path, payload, request_id, trace), remaining
            )
        except TimeoutError:
            return _Exchange(
                failure=_failure(
                    operation,
                    FailureKind.TRANSPORT,
                    (
                        "Todoist could not be reached, so the request was not sent"
                        if trace.connect_timed_out_before_send
                        else "Todoist request did not return a complete response"
                    ),
                ),
                transient=True,
                undispatched=trace.connect_timed_out_before_send,
            )

    async def _send_raw(
        self,
        operation: str,
        token: str,
        method: str,
        path: str,
        payload: dict[str, Any] | None = None,
        request_id: str = "",
        trace: _RequestDispatchTrace | None = None,
    ) -> _Exchange:
        headers = {"Authorization": "Bearer " + token, "Accept": "application/json"}
        if method == "POST":
            headers["Content-Type"] = "application/json"
        if request_id:
            headers["X-Request-ID"] = request_id
        content = (
            json.dumps(payload, separators=(",", ":"), ensure_ascii=False).encode()
            if payload is not None
            else None
        )
        try:
            request = self.http_client.build_request(
                method, self.base_url + path, content=content, headers=headers
            )
        except (ValueError, httpx.HTTPError):
            return _Exchange(
                failure=_failure(operation, FailureKind.LOCAL_DEFECT, "Todoist request could not be built"),
                local_defect=True,
            )
        try:
            request_trace = trace if trace is not None else _RequestDispatchTrace()
            request.extensions["trace"] = request_trace.observe
            response = await self.http_client.send(request, follow_redirects=False, stream=True)
        except httpx.TransportError:
            was_undispatched = request_trace.provably_not_sent
            message = (
                "Todoist could not be reached, so the request was not sent"
                if was_undispatched
                else "Todoist request did not return a complete response"
            )
            return _Exchange(
                failure=_failure(operation, FailureKind.TRANSPORT, message),
                transient=True,
                undispatched=was_undispatched,
            )
        status = response.status_code
        if method == "DELETE" and 200 <= status < 300:
            await response.aclose()
            return _Exchange(status=status)
        if not 200 <= status < 300:
            await response.aclose()
            kind = FailureKind.PROVIDER_REJECTION
            if status == 401:
                kind = FailureKind.AUTHENTICATION
            elif status == 403:
                kind = FailureKind.AUTHORIZATION
            elif status == 404:
                kind = FailureKind.NOT_FOUND
            elif status == 409:
                kind = FailureKind.CONFLICT
            elif status in (400, 422):
                kind = FailureKind.VALIDATION
            elif status == 429:
                kind = FailureKind.RATE_LIMIT
            elif status == 408 or status >= 500:
                kind = FailureKind.AVAILABILITY
            retry_after = (
                parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
                if status == 429 or status == 408 or status >= 500
                else 0
            )
            return _Exchange(
                status=status,
                failure=_failure(operation, kind, f"Todoist returned HTTP {status}"),
                retry_after=retry_after,
                transient=status == 408 or status >= 500,
            )
        try:
            body = await read_bounded_body(response, self.max_response_bytes)
        except BodyTooLargeError:
            return _Exchange(
                status=status,
                failure=_failure(
                    operation,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Todoist response was interrupted or exceeded the response limit",
                ),
            )
        except ProviderTransportError:
            return _Exchange(
                status=status,
                failure=_failure(
                    operation,
                    FailureKind.PROTOCOL,
                    "Todoist response was interrupted or exceeded the response limit",
                ),
                transient=True,
            )
        if token.encode() in body:
            return _Exchange(
                status=status,
                failure=_failure(
                    operation, FailureKind.PROTOCOL, "Todoist returned credential material in the response"
                ),
            )
        return _Exchange(status=status, body=body)

    async def _task_request(
        self,
        operation: str,
        token: str,
        method: str,
        task_id: str,
        payload: dict[str, Any] | None = None,
        budget: _AttemptBudget | None = None,
        is_write: bool = False,
    ) -> tuple[_Exchange, Task | None]:
        answer = await self._send(
            operation,
            token,
            method,
            "/tasks/" + task_id,
            payload,
            budget=budget,
            is_write=is_write,
        )
        if answer.failure is not None:
            return answer, None
        task = _decode_task(answer.body)
        if task is None or task.id != task_id:
            return _Exchange(
                status=answer.status,
                failure=_failure(
                    operation, FailureKind.PROTOCOL, "Todoist returned an invalid task response"
                ),
            ), None
        return answer, task


class GetTaskOperation:
    """Read one active task and classify provider outcomes."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query definition."""
        return GET_TASK_DEFINITION

    async def invoke(self, call: Call, input: GetTaskInput) -> QueryAttempt[Task]:
        """Read one active task with retries for transient failures."""
        operation = "getTask"
        if not _valid_task_id(input.task_id):
            return QueryAttempt.branch_result(
                "defect",
                Task(),
                Receipt(),
                _failure(operation, FailureKind.VALIDATION, "taskId must be a non-empty opaque task ID"),
            )
        budget = _attempt_budget(call)
        token, rejected = await self.client._authorize(call, operation)
        if rejected is not None:
            answer, task = rejected, None
        else:
            assert token is not None
            answer, task = await self.client._task_request(
                operation, token, "GET", input.task_id, budget=budget
            )
        if answer.failure is None:
            return QueryAttempt.branch_result("found", task or Task(), Receipt())
        if answer.local_defect:
            branch = "defect"
        elif answer.transient or answer.status == 429:
            return QueryAttempt.retry(answer.failure, answer.retry_after)
        elif answer.status == 404:
            branch = "notFound"
        elif answer.failure.kind in (FailureKind.PROTOCOL, FailureKind.RESPONSE_TOO_LARGE):
            branch = "invalidResponse"
        else:
            branch = "providerRejected"
        return QueryAttempt.branch_result(branch, Task(), Receipt(), answer.failure)


class UpdateTaskOperation:
    """Replace selected fields and confirm an unusable answer by read-back."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated Mutation definition."""
        return UPDATE_TASK_CONTENT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpdateTaskInput) -> IdempotencyKey:
        """Preserve the Dex Call ID for absolute field replacement."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: UpdateTaskInput) -> MutationAttempt[Task]:
        """Patch fields and read back an unconfirmed success once."""
        operation = "updateTaskContent"
        if (
            not _valid_task_id(input.task_id)
            or input.content is None
            and input.description is None
            or input.content is not None
            and (not _valid_text(input.content) or not input.content.strip(_GO_WHITESPACE))
            or input.description is not None
            and not _valid_text(input.description)
        ):
            return MutationAttempt.branch_result(
                "defect",
                Task(),
                Receipt(),
                _failure(
                    operation,
                    FailureKind.VALIDATION,
                    "taskId and at least one valid field are required; "
                    "specified content must be non-blank UTF-8",
                ),
            )
        payload = {}
        if input.content is not None:
            payload["content"] = input.content
        if input.description is not None:
            payload["description"] = input.description
        if len(json.dumps(payload, separators=(",", ":"), ensure_ascii=False).encode()) > 1 << 20:
            return MutationAttempt.branch_result(
                "defect",
                Task(),
                Receipt(),
                _failure(
                    operation, FailureKind.VALIDATION, "encoded fields exceed Todoist's 1 MiB request limit"
                ),
            )
        budget = _attempt_budget(call)
        token, rejected = await self.client._authorize(call, operation)
        if rejected is not None:
            answer, task = rejected, None
        else:
            assert token is not None
            answer, task = await self.client._task_request(
                operation, token, "POST", input.task_id, payload, budget=budget, is_write=True
            )
        matches = (
            task is not None
            and (input.content is None or task.content == input.content)
            and (input.description is None or task.description == input.description)
        )
        if 200 <= answer.status < 300 and (answer.failure is not None or not matches):
            assert token is not None
            read, read_task = await self.client._task_request(
                operation, token, "GET", input.task_id, budget=budget
            )
            read_matches = (
                read_task is not None
                and (input.content is None or read_task.content == input.content)
                and (input.description is None or read_task.description == input.description)
            )
            if read.failure is None and read_matches:
                return MutationAttempt.branch_result("updated", read_task or Task(), Receipt())
            if read.local_defect:
                assert read.failure is not None
                return MutationAttempt.branch_result("defect", Task(), Receipt(), read.failure)
            if read.status == 404:
                return MutationAttempt.branch_result(
                    "notFound",
                    Task(),
                    Receipt(),
                    _failure(
                        operation,
                        FailureKind.NOT_FOUND,
                        "Todoist answered the update, but the task is no longer active; "
                        "it may be completed or deleted, so the specified fields are unconfirmed",
                    ),
                )
            if read.status >= 300 and not read.transient and read.status != 429:
                assert read.failure is not None
                return MutationAttempt.branch_result(
                    "providerRejected",
                    Task(),
                    Receipt(),
                    _failure(
                        operation,
                        read.failure.kind,
                        f"Todoist rejected the read-back with HTTP {read.status}, "
                        "so the update could not be confirmed; repeating it is safe "
                        "because it sets absolute field values",
                    ),
                )
            return MutationAttempt.retry(
                read.failure
                or answer.failure
                or _failure(operation, FailureKind.PROTOCOL, "Todoist did not confirm the specified fields"),
                read.retry_after,
            )
        if answer.failure is None:
            return MutationAttempt.branch_result("updated", task or Task(), Receipt())
        if answer.local_defect:
            branch = "defect"
        elif answer.transient or answer.status == 429:
            return MutationAttempt.retry(answer.failure, answer.retry_after)
        elif answer.status == 404:
            branch = "notFound"
        else:
            branch = "providerRejected"
        return MutationAttempt.branch_result(branch, Task(), Receipt(), answer.failure)


class ListTasksOperation:
    """Read and validate one bounded task page."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query definition."""
        return LIST_TASKS_DEFINITION

    async def invoke(self, call: Call, input: ListTasksInput) -> QueryAttempt[TaskPage]:
        """Read one cursor page, preserving the caller's filters."""
        operation = "listTasks"
        if (
            input.project_id
            and not _valid_task_id(input.project_id)
            or type(input.limit) is not int
            or not 0 <= input.limit <= 200
            or not _valid_cursor(input.cursor)
        ):
            return QueryAttempt.branch_result(
                "defect",
                TaskPage(),
                Receipt(),
                _failure(operation, FailureKind.VALIDATION, "invalid projectId, cursor, or limit"),
            )
        query = {}
        if input.project_id:
            query["project_id"] = input.project_id
        if input.cursor:
            query["cursor"] = input.cursor
        if input.limit:
            query["limit"] = str(input.limit)
        path = "/tasks" + ("?" + urlencode(sorted(query.items())) if query else "")
        budget = _attempt_budget(call)
        token, rejected = await self.client._authorize(call, operation)
        if rejected is not None:
            answer = rejected
        else:
            assert token is not None
            answer = await self.client._send(operation, token, "GET", path, budget=budget)
        if answer.failure is not None:
            if answer.local_defect:
                branch = "defect"
            elif answer.transient or answer.status == 429:
                return QueryAttempt.retry(answer.failure, answer.retry_after)
            elif answer.failure.kind in (FailureKind.PROTOCOL, FailureKind.RESPONSE_TOO_LARGE):
                branch = "invalidResponse"
            else:
                branch = "providerRejected"
            return QueryAttempt.branch_result(branch, TaskPage(), Receipt(), answer.failure)
        page = _decode_page(answer.body, input)
        if page is None:
            return QueryAttempt.branch_result(
                "invalidResponse",
                TaskPage(),
                Receipt(),
                _failure(operation, FailureKind.PROTOCOL, "Todoist returned an invalid task page"),
            )
        return QueryAttempt.branch_result("listed", page, Receipt())


class CreateTaskOperation:
    """Create one task with stable request identity and narrow uncertainty."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated Mutation definition."""
        return CREATE_TASK_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreateTaskInput) -> IdempotencyKey:
        """Derive Todoist's UUID-shaped X-Request-ID from the Dex Call ID."""
        digest = bytearray(hashlib.sha256(("todoist/createTask/" + str(call_id)).encode()).digest())
        digest[6] = digest[6] & 0x0F | 0x50
        digest[8] = digest[8] & 0x3F | 0x80
        encoded = digest[:16].hex()
        return IdempotencyKey(
            f"{encoded[:8]}-{encoded[8:12]}-{encoded[12:16]}-{encoded[16:20]}-{encoded[20:]}"
        )

    async def invoke(self, call: Call, input: CreateTaskInput) -> MutationAttempt[Task]:
        """Create once; read a known ID back without replaying the POST."""
        operation = "createTask"
        if (
            not _valid_text(input.content)
            or not input.content.strip(_GO_WHITESPACE)
            or not _valid_text(input.description)
            or input.project_id
            and not _valid_task_id(input.project_id)
            or _REQUEST_ID.fullmatch(str(call.idempotency_key)) is None
        ):
            return MutationAttempt.branch_result(
                "defect",
                Task(),
                Receipt(),
                _failure(
                    operation,
                    FailureKind.VALIDATION,
                    "valid content, description, projectId, and stable request identity are required",
                ),
            )
        payload = {"content": input.content}
        if input.description:
            payload["description"] = input.description
        if input.project_id:
            payload["project_id"] = input.project_id
        if len(json.dumps(payload, separators=(",", ":"), ensure_ascii=False).encode()) > 1 << 20:
            return MutationAttempt.branch_result(
                "defect",
                Task(),
                Receipt(),
                _failure(
                    operation, FailureKind.VALIDATION, "encoded task exceeds Todoist's 1 MiB request limit"
                ),
            )
        budget = _attempt_budget(call)
        token, rejected = await self.client._authorize(call, operation)
        if rejected is not None:
            answer = rejected
        else:
            assert token is not None
            answer = await self.client._send(
                operation,
                token,
                "POST",
                "/tasks",
                payload,
                str(call.idempotency_key),
                budget=budget,
                is_write=True,
            )
        if answer.failure is not None:
            if answer.local_defect:
                return MutationAttempt.branch_result("defect", Task(), Receipt(), answer.failure)
            if answer.undispatched or answer.status in (429, 503):
                return MutationAttempt.retry(answer.failure, answer.retry_after)
            if (
                answer.status == 409
                or answer.transient
                or answer.failure.kind in (FailureKind.PROTOCOL, FailureKind.RESPONSE_TOO_LARGE)
            ):
                return MutationAttempt.uncertain(Task(), answer.failure, Receipt())
            return MutationAttempt.branch_result("providerRejected", Task(), Receipt(), answer.failure)
        task = _decode_task(answer.body)
        if task is not None:
            return MutationAttempt.branch_result("created", task, Receipt())
        try:
            task_id = json.loads(answer.body).get("id")
        except (ValueError, AttributeError):
            task_id = None
        if not _valid_task_id(task_id):
            return MutationAttempt.uncertain(
                Task(),
                _failure(
                    operation, FailureKind.PROTOCOL, "Todoist answered the creation without a usable task"
                ),
                Receipt(),
            )
        assert token is not None
        read, read_task = await self.client._task_request(operation, token, "GET", task_id, budget=budget)
        if read.failure is None:
            return MutationAttempt.branch_result("created", read_task or Task(), Receipt())
        return MutationAttempt.uncertain(
            Task(),
            _failure(
                operation,
                FailureKind.PROTOCOL,
                f"Todoist accepted the creation of task {task_id} but it could not "
                "be read back in this attempt, so the task is unconfirmed",
            ),
            Receipt(),
        )


class DeleteTaskOperation:
    """Delete an immutable task ID with safe retries."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated Mutation definition."""
        return DELETE_TASK_DEFINITION

    def idempotency_key(self, call_id: CallID, input: DeleteTaskInput) -> IdempotencyKey:
        """Preserve the Dex Call ID for repeated deletion."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: DeleteTaskInput) -> MutationAttempt[DeleteTaskOutcome]:
        """Return deleted, already absent, rejection, defect, or retry."""
        operation = "deleteTask"
        if not _valid_task_id(input.task_id):
            return MutationAttempt.branch_result(
                "defect",
                DeleteTaskOutcome(),
                Receipt(),
                _failure(operation, FailureKind.VALIDATION, "taskId must be a non-empty opaque task ID"),
            )
        budget = _attempt_budget(call)
        token, rejected = await self.client._authorize(call, operation)
        if rejected is not None:
            answer = rejected
        else:
            assert token is not None
            answer = await self.client._send(
                operation, token, "DELETE", "/tasks/" + input.task_id, budget=budget
            )
        value = DeleteTaskOutcome(input.task_id)
        if answer.failure is None:
            return MutationAttempt.branch_result("deleted", value, Receipt())
        if answer.local_defect:
            return MutationAttempt.branch_result("defect", DeleteTaskOutcome(), Receipt(), answer.failure)
        if answer.transient or answer.status == 429:
            return MutationAttempt.retry(answer.failure, answer.retry_after)
        if answer.status == 404:
            return MutationAttempt.branch_result("alreadyAbsent", value, Receipt(), answer.failure)
        return MutationAttempt.branch_result(
            "providerRejected", DeleteTaskOutcome(), Receipt(), answer.failure
        )
