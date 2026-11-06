# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""ClickUp HTTP client and provider outcome classification."""

import json
import re
from collections.abc import Callable, Mapping
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from decimal import Decimal
from typing import TYPE_CHECKING, Any
from urllib.parse import urlencode

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    ReauthorizationRequiredError,
    Receipt,
)
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials

if TYPE_CHECKING:
    from .models import SearchTasksOutput, Task
    from .operations import (
        AddCommentOperation,
        CreateTaskOperation,
        FindMemberByEmailOperation,
        GetTaskOperation,
        SearchTasksOperation,
        UpdateTaskOperation,
        UpdateTaskTagsOperation,
    )

API_BASE_URL = "https://api.clickup.com/api/v2"
_ERROR_CODE = re.compile(r"^[A-Z][A-Z0-9]{0,15}_[0-9]{1,6}$")
_TEAM_NOT_AUTHORIZED = {
    "OAUTH_023",
    "OAUTH_026",
    "OAUTH_027",
    *(f"OAUTH_{number:03d}" for number in range(29, 46)),
}


@dataclass(frozen=True)
class Exchange:
    """One classified provider request."""

    kind: str
    failure: Failure | None = None
    body: bytes = b""
    retry_after: timedelta = timedelta()

    @property
    def is_retryable(self) -> bool:
        """Whether a read or repeat-safe write may try again."""
        return self.kind in ("not_sent", "rate_limited", "unconfirmed")


class Client:
    """Resolve current credentials for every ClickUp API request."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Callable[[], datetime] | None = None,
        api_base_url: str = API_BASE_URL,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("ClickUp configuration and credential source are required")
        if config.max_response_bytes <= 0 or config.webhook_max_body_bytes <= 0:
            raise ValueError("ClickUp response and webhook body limits must be positive")
        self.base_url = validate_base_url(api_base_url)
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            9, address_policy="publicHTTPS" if self.base_url == API_BASE_URL else "privateNetworkHTTP"
        )
        self._owns_http_client = http_client is None
        self.now = now or (lambda: datetime.now(UTC))

    async def aclose(self) -> None:
        """Close an HTTP client this connector created."""
        if self._owns_http_client:
            await self.http_client.aclose()

    def receipt(self, call: Call, object_id: str = "", metadata: Mapping[str, str] | None = None) -> Receipt:
        """Record provider identity without credential material."""
        return Receipt(
            call_id=call.id,
            provider="clickup",
            observed_at=self.now(),
            idempotency_key=call.idempotency_key,
            provider_object_id=object_id,
            metadata=dict(metadata or {}),
        )

    async def resolve_credentials(
        self, call: Call, operation: str
    ) -> tuple[Credentials | None, Failure | None]:
        """Return current personal token or a terminal authentication failure."""
        try:
            credentials = await self.credentials.resolve(call)
            token = credentials.api_token.reveal()
            if not is_header_safe_credential(token) or not token.startswith("pk_") or len(token) <= 3:
                raise ValueError("invalid personal token")
            return credentials, None
        except (AttributeError, TypeError, ValueError, LookupError, OSError, ReauthorizationRequiredError):
            return None, failure(
                operation, FailureKind.AUTHENTICATION, "ClickUp connection credentials are unavailable"
            )

    async def exchange(
        self,
        operation: str,
        credentials: Credentials,
        method: str,
        path: str,
        query: Mapping[str, str | list[str]] | None = None,
        payload: Any = None,
    ) -> Exchange:
        """Send one request and classify HTTP and transport failures."""
        try:
            query_pairs = [
                (key, item)
                for key in sorted(query or {})
                for item in (
                    (query or {})[key] if isinstance((query or {})[key], list) else [(query or {})[key]]
                )
            ]
            query_text = urlencode(query_pairs, doseq=True)
            url = self.base_url + path + (("?" + query_text) if query_text else "")
            headers = {"Authorization": credentials.api_token.reveal(), "Accept": "application/json"}
            body = (
                json.dumps(payload, separators=(",", ":"), ensure_ascii=False, allow_nan=False).encode()
                if payload is not None
                else None
            )
            if body is not None:
                headers["Content-Type"] = "application/json"
            request = self.http_client.build_request(method, url, headers=headers, content=body)
        except (ValueError, TypeError, httpx.HTTPError):
            return outcome(
                "defect", operation, FailureKind.LOCAL_DEFECT, "ClickUp request could not be built"
            )
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError as error:
            if error.provably_not_sent:
                return outcome(
                    "not_sent",
                    operation,
                    FailureKind.TRANSPORT,
                    "ClickUp could not be reached; no request was sent",
                )
            return outcome(
                "unconfirmed",
                operation,
                FailureKind.TRANSPORT,
                "ClickUp request failed before a response arrived",
            )
        # Go classifies a large 429/5xx by status, rather than size.
        if 200 <= response.status_code < 300:
            try:
                response_body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return outcome(
                    "invalid",
                    operation,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "ClickUp response exceeds the configured maxResponseBytes limit",
                )
            except ProviderTransportError:
                return outcome(
                    "unconfirmed", operation, FailureKind.TRANSPORT, "ClickUp response could not be read"
                )
            if credentials.api_token.reveal().encode() in response_body:
                return outcome(
                    "invalid",
                    operation,
                    FailureKind.PROTOCOL,
                    "ClickUp response reflected the connection credential",
                )
            return Exchange("succeeded", body=response_body)
        status = response.status_code
        try:
            error_body = await read_bounded_body(response, self.max_response_bytes)
        except BodyTooLargeError:
            error_body = b""
        except ProviderTransportError:
            return outcome(
                "unconfirmed", operation, FailureKind.TRANSPORT, "ClickUp response could not be read"
            )
        error_code = ""
        try:
            decoded = json.loads(error_body)
            if (
                isinstance(decoded, dict)
                and isinstance(decoded.get("ECODE"), str)
                and _ERROR_CODE.fullmatch(decoded["ECODE"])
            ):
                error_code = decoded["ECODE"]
        except (ValueError, httpx.HTTPError):
            pass
        suffix = f" [{error_code}]" if error_code else ""
        if status == 429:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), self.now())
            if delay <= 0:
                try:
                    reset = int(response.headers.get("X-RateLimit-Reset", ""))
                    delay = (
                        max(1.0, (datetime.fromtimestamp(reset, UTC) - self.now()).total_seconds())
                        if reset > 0
                        else 0.0
                    )
                except (ValueError, OverflowError):
                    delay = 0.0
            return outcome(
                "rate_limited",
                operation,
                FailureKind.RATE_LIMIT,
                f"ClickUp rate limited the token (HTTP 429){suffix}",
                min(delay, 60.0),
            )
        if status == 408 or status >= 500:
            return outcome(
                "unconfirmed",
                operation,
                FailureKind.AVAILABILITY,
                f"ClickUp could not complete the request (HTTP {status}){suffix}",
            )
        if status in (404, 410):
            return outcome(
                "not_found",
                operation,
                FailureKind.NOT_FOUND,
                f"ClickUp found no such resource (HTTP {status}){suffix}",
            )
        if status == 401 and error_code in _TEAM_NOT_AUTHORIZED:
            return outcome(
                "not_found",
                operation,
                FailureKind.AUTHORIZATION,
                "ClickUp reports that the resource's Workspace is not authorized for the token "
                f"(HTTP 401){suffix}",
            )
        if 300 <= status < 400:
            return outcome(
                "rejected", operation, FailureKind.PROTOCOL, f"ClickUp redirected the request (HTTP {status})"
            )
        if status == 401:
            return outcome(
                "rejected",
                operation,
                FailureKind.AUTHENTICATION,
                f"ClickUp rejected the personal API token (HTTP 401){suffix}",
            )
        kind = (
            FailureKind.AUTHORIZATION
            if status == 403
            else FailureKind.CONFLICT
            if status == 409
            else FailureKind.VALIDATION
            if status in (400, 422)
            else FailureKind.PROVIDER_REJECTION
        )
        return outcome("rejected", operation, kind, f"ClickUp rejected the request (HTTP {status}){suffix}")

    def search_tasks(self) -> "SearchTasksOperation":
        """Bind Workspace search."""
        from .operations import SearchTasksOperation

        return SearchTasksOperation(self)

    def get_task(self) -> "GetTaskOperation":
        """Bind task read."""
        from .operations import GetTaskOperation

        return GetTaskOperation(self)

    def create_task(self) -> "CreateTaskOperation":
        """Bind task creation."""
        from .operations import CreateTaskOperation

        return CreateTaskOperation(self)

    def update_task(self) -> "UpdateTaskOperation":
        """Bind task update."""
        from .operations import UpdateTaskOperation

        return UpdateTaskOperation(self)

    def update_task_tags(self) -> "UpdateTaskTagsOperation":
        """Bind tag update."""
        from .operations import UpdateTaskTagsOperation

        return UpdateTaskTagsOperation(self)

    def add_comment(self) -> "AddCommentOperation":
        """Bind comment creation."""
        from .operations import AddCommentOperation

        return AddCommentOperation(self)

    def find_member_by_email(self) -> "FindMemberByEmailOperation":
        """Bind Workspace member lookup."""
        from .operations import FindMemberByEmailOperation

        return FindMemberByEmailOperation(self)


def failure(operation: str, kind: FailureKind, message: str) -> Failure:
    """Create one safe classified provider failure."""
    return Failure(kind, "clickup", operation, message)


def outcome(
    kind: str, operation: str, failure_kind: FailureKind, message: str, delay: float = 0.0
) -> Exchange:
    """Create a failed exchange."""
    return Exchange(kind, failure(operation, failure_kind, message), retry_after=timedelta(seconds=delay))


_TASK_ID = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$")
_NUMERIC_ID = re.compile(r"^[0-9]{1,20}$")


def path_escape(value: str) -> str:
    """Escape path segments exactly as Go url.PathEscape does."""
    from urllib.parse import quote

    return quote(value, safe="-._~$&+=:@")


def numeric_id(value: str) -> bool:
    """Check a ClickUp numeric ID without interpreting leading zeros."""
    return isinstance(value, str) and bool(_NUMERIC_ID.fullmatch(value))


def task_id(value: str) -> bool:
    """Check an ordinary ClickUp task ID."""
    return isinstance(value, str) and bool(_TASK_ID.fullmatch(value))


def flexible_id(value: Any) -> str:
    """Decode ClickUp's string or JSON-number identifier."""
    if value is None:
        return ""
    if isinstance(value, str):
        return value
    if type(value) is int:
        return str(value)
    if isinstance(value, _JSONNumber):
        return value.source
    if isinstance(value, Decimal):
        return str(value)
    raise ValueError("identifier is not a string or number")


def flexible_integer(value: Any) -> int:
    """Decode a ClickUp user ID from an integer or decimal string."""
    if value is None or value == "":
        return 0
    if type(value) is int:
        if -(2**63) <= value < 2**63:
            return value
        raise ValueError("user ID is not an integer")
    if isinstance(value, str) and re.fullmatch(r"[+-]?[0-9]+", value):
        return flexible_integer(int(value))
    raise ValueError("user ID is not an integer")


def unix_milliseconds(value: Any) -> datetime | None:
    """Decode ClickUp Unix milliseconds as a UTC instant."""
    if value is None or value == "":
        return None
    if type(value) not in (int, str) or not re.fullmatch(r"[+-]?[0-9]+", str(value)):
        raise ValueError("time is not Unix milliseconds")
    milliseconds = int(value)
    if milliseconds < 0 or milliseconds >= 2**63:
        raise ValueError("time is not Unix milliseconds")
    if milliseconds == 0:
        return None
    try:
        return datetime(1970, 1, 1, tzinfo=UTC) + timedelta(milliseconds=milliseconds)
    except OverflowError as error:
        raise ValueError("time is outside Python's supported range") from error


def decode_object(body: bytes) -> dict[str, Any]:
    """Decode one JSON object; reject arrays and scalars."""
    value = json.loads(
        body,
        parse_float=_JSONNumber,
        parse_int=_decode_json_integer,
        parse_constant=_reject_non_json_number,
    )
    if not isinstance(value, dict):
        raise ValueError("response is not a JSON object")
    return value


class _JSONNumber(Decimal):
    source: str

    def __new__(cls, source: str) -> "_JSONNumber":
        instance = super().__new__(cls, source)
        instance.source = source
        return instance


def _reject_non_json_number(value: str) -> None:
    raise ValueError(f"invalid JSON number {value}")


def _decode_json_integer(source: str) -> int | _JSONNumber:
    return _JSONNumber(source) if source == "-0" else int(source)


def _exact_custom_field_value(value: Any, field_id: str) -> Any:
    if isinstance(value, Decimal):
        try:
            converted = float(value)
            encoded = json.dumps(converted, allow_nan=False)
        except (OverflowError, ValueError) as error:
            raise ValueError(f"custom field {field_id} value cannot be represented exactly") from error
        source = value.source if isinstance(value, _JSONNumber) else str(value)
        if encoded != source:
            raise ValueError(f"custom field {field_id} value cannot be represented exactly")
        return converted
    if isinstance(value, list):
        return [_exact_custom_field_value(item, field_id) for item in value]
    if isinstance(value, dict):
        return {key: _exact_custom_field_value(item, field_id) for key, item in value.items()}
    return value


def decode_task(value: Any, details: bool = False) -> "Task":
    """Decode one task with Go's valid field types and optional details."""
    from .models import CustomFieldValue, Task, TaskStatus, User

    if not isinstance(value, dict):
        raise ValueError("task is not a JSON object")

    def typed(field: str, expected: type, default: Any) -> Any:
        result = value.get(field, default)
        if result is None:
            return default
        if type(result) is not expected:
            raise ValueError(f"task {field} has invalid type")
        return result

    def nullable_string(source: dict[str, Any], field: str, label: str) -> str:
        result = source.get(field)
        if result is None:
            return ""
        if type(result) is not str:
            raise ValueError(f"{label} has invalid type")
        return result

    def timestamp(field: str) -> datetime | None:
        try:
            return unix_milliseconds(value.get(field))
        except ValueError as error:
            raise ValueError(f"task {field}: {error}") from error

    identifier = typed("id", str, "")
    if not task_id(identifier):
        raise ValueError("task has no valid ID")
    status = typed("status", dict, {})
    status_name = nullable_string(status, "status", "task status")
    status_type = nullable_string(status, "type", "task status")
    raw_priority = value.get("priority")
    priority_name = ""
    if isinstance(raw_priority, dict):
        priority_name = nullable_string(raw_priority, "priority", "task priority")
        raw_priority = flexible_id(raw_priority.get("id"))
        if not re.fullmatch(r"[+-]?[0-9]+", raw_priority):
            raise ValueError("task priority has no valid level")
        raw_priority = int(raw_priority)
    if raw_priority is not None and (type(raw_priority) is not int or raw_priority not in (1, 2, 3, 4)):
        raise ValueError("task priority is invalid")

    def location(name: str) -> dict[str, Any]:
        result = value.get(name)
        if result is None:
            return {}
        if not isinstance(result, dict):
            raise ValueError("task location is invalid")
        return result

    assignees = []
    for user in typed("assignees", list, []):
        if not isinstance(user, dict):
            raise ValueError("task assignee is invalid")
        user_id = flexible_integer(user.get("id"))
        if user_id <= 0:
            raise ValueError("task assignee has no valid user ID")
        username = nullable_string(user, "username", "task assignee username")
        assignees.append(User(user_id, username))
    tags = []
    for tag in typed("tags", list, []):
        if not isinstance(tag, dict):
            raise ValueError("task tag is invalid")
        tags.append(nullable_string(tag, "name", "task tag name"))
    creator = value.get("creator")
    if creator is not None:
        if not isinstance(creator, dict):
            raise ValueError("task creator is invalid")
        creator_id = flexible_integer(creator.get("id"))
        creator_name = nullable_string(creator, "username", "task creator username")
        creator = User(creator_id, creator_name) if creator_id > 0 else None
    custom_fields: list[CustomFieldValue] = []
    for field in typed("custom_fields", list, []):
        if not isinstance(field, dict):
            raise ValueError("task custom field is invalid")
        field_id = nullable_string(field, "id", "task custom field ID")
        field_name = nullable_string(field, "name", f"custom field {field_id} name")
        field_type = nullable_string(field, "type", f"custom field {field_id} type")
        if details and len(custom_fields) < 100:
            if not task_id(field_id):
                raise ValueError("task custom field has no valid ID")
            custom_fields.append(
                CustomFieldValue(
                    field_id, field_name, field_type,
                    _exact_custom_field_value(field.get("value"), field_id)
                )
            )
    description = value.get("markdown_description")
    if description is None:
        description = value.get("description")
    if description is not None and type(description) is not str:
        raise ValueError("task description is invalid")
    encoded_description = (description or "").encode()
    truncated = details and len(encoded_description) > 65536
    if truncated:
        encoded_description = encoded_description[:65536]
        while True:
            try:
                description = encoded_description.decode()
                break
            except UnicodeDecodeError:
                encoded_description = encoded_description[:-1]
    return Task(
        id=identifier,
        custom_id=typed("custom_id", str, "") if value.get("custom_id") is not None else "",
        name=typed("name", str, ""),
        url=typed("url", str, ""),
        status=TaskStatus(status_name, status_type),
        priority=raw_priority,
        priority_name=priority_name,
        due_at=timestamp("due_date"),
        start_at=timestamp("start_date"),
        created_at=timestamp("date_created") or datetime.min.replace(tzinfo=UTC),
        updated_at=timestamp("date_updated") or datetime.min.replace(tzinfo=UTC),
        closed_at=timestamp("date_closed"),
        is_archived=typed("archived", bool, False),
        creator=creator,
        assignees=tuple(assignees),
        tags=tuple(tags),
        parent_task_id=typed("parent", str, "") if value.get("parent") is not None else "",
        workspace_id=flexible_id(value.get("team_id")),
        list_id=flexible_id(location("list").get("id")),
        list_name=nullable_string(location("list"), "name", "task list name"),
        folder_id=flexible_id(location("folder").get("id")),
        space_id=flexible_id(location("space").get("id")),
        markdown_description=description if details and description is not None else "",
        is_description_truncated=truncated,
        custom_fields=tuple(custom_fields),
    )


def decode_search_page(body: bytes, page: int) -> "SearchTasksOutput":
    """Decode one ClickUp task page and its next-page marker."""
    from .models import SearchTasksOutput

    value = decode_object(body)
    tasks = value.get("tasks")
    if not isinstance(tasks, list) or len(tasks) > 100:
        raise ValueError("the response has no tasks array")
    last_page = value.get("last_page")
    if last_page is not None and type(last_page) is not bool:
        raise ValueError("last_page is invalid")
    decoded = tuple(decode_task(task) for task in tasks)
    has_more = (not last_page and bool(tasks)) if last_page is not None else len(tasks) == 100
    return SearchTasksOutput(decoded, page, page + 1 if has_more else None)
