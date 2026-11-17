# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Receive signed ClickUp task webhooks with durable Trigger delivery."""

from __future__ import annotations

import hashlib
import hmac
import json
import re
import weakref
from collections.abc import Awaitable, Callable, Iterable, MutableMapping, Sequence
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any, Final, TypeAlias

from dex import CodecRegistry, Value, WireKind
from dex_connectors.sdk import (
    ConnectionRef,
    TriggerBindingRef,
    TriggerCancellation,
    TriggerConfig,
    TriggerContext,
    TriggerEvent,
    TriggerLogger,
    TriggerRunner,
    TriggerTarget,
    new_trigger,
)
from dex_connectors.sdk.projectconfig import (
    LoadedProject,
    TriggerInboxKey,
    decode_trigger_configuration,
    new_durable_trigger_target,
)
from dex_connectors.sdk.webhooktrigger import (
    WebhookEndpoint,
    WebhookEndpointConfig,
    WebhookEndpointRunner,
    WebhookRequest,
    WebhookResponse,
    WebhookVerificationUnavailableError,
)

from ._generated.definitions import CONNECTOR_ID, TASK_EVENT_TRIGGER_DEFINITION
from ._generated.triggers import decode_task_event_trigger_configuration, define_task_event_trigger_binding
from .client import Client, Credentials, flexible_id, flexible_integer, unix_milliseconds
from .connection import Connection
from .models import TaskEvent, TaskEventTriggerConfiguration, TaskHistoryItem

TASK_EVENT_CREATED: Final = "taskCreated"
TASK_EVENT_UPDATED: Final = "taskUpdated"
TASK_EVENT_DELETED: Final = "taskDeleted"
TASK_EVENT_PRIORITY_UPDATED: Final = "taskPriorityUpdated"
TASK_EVENT_STATUS_UPDATED: Final = "taskStatusUpdated"
TASK_EVENT_ASSIGNEE_UPDATED: Final = "taskAssigneeUpdated"
TASK_EVENT_DUE_DATE_UPDATED: Final = "taskDueDateUpdated"
TASK_EVENT_TAG_UPDATED: Final = "taskTagUpdated"
TASK_EVENT_MOVED: Final = "taskMoved"
TASK_EVENT_COMMENT_POSTED: Final = "taskCommentPosted"
TASK_EVENT_COMMENT_UPDATED: Final = "taskCommentUpdated"
TASK_EVENT_TIME_ESTIMATE_UPDATED: Final = "taskTimeEstimateUpdated"
TASK_EVENT_TIME_TRACKED_UPDATED: Final = "taskTimeTrackedUpdated"
_SUPPORTED_EVENTS: Final = (
    TASK_EVENT_CREATED,
    TASK_EVENT_UPDATED,
    TASK_EVENT_DELETED,
    TASK_EVENT_PRIORITY_UPDATED,
    TASK_EVENT_STATUS_UPDATED,
    TASK_EVENT_ASSIGNEE_UPDATED,
    TASK_EVENT_DUE_DATE_UPDATED,
    TASK_EVENT_TAG_UPDATED,
    TASK_EVENT_MOVED,
    TASK_EVENT_COMMENT_POSTED,
    TASK_EVENT_COMMENT_UPDATED,
    TASK_EVENT_TIME_ESTIMATE_UPDATED,
    TASK_EVENT_TIME_TRACKED_UPDATED,
)
_TASK_ID: Final = re.compile(r"[A-Za-z0-9][A-Za-z0-9_-]{0,63}", re.ASCII)
_SIGNATURE: Final = re.compile(r"[0-9a-fA-F]{64}", re.ASCII)
_TRIGGER_NAME: Final = TASK_EVENT_TRIGGER_DEFINITION.trigger.trigger_name
_ASGIScope: TypeAlias = MutableMapping[str, Any]
_ASGIReceive: TypeAlias = Callable[[], Awaitable[MutableMapping[str, Any]]]
_ASGISend: TypeAlias = Callable[[MutableMapping[str, Any]], Awaitable[None]]
_ENDPOINTS: weakref.WeakKeyDictionary[
    Client, dict[ConnectionRef, WebhookEndpoint[Credentials, TaskEvent]]
] = weakref.WeakKeyDictionary()


def task_events() -> tuple[str, ...]:
    """Return supported task webhook names in the Go connector's stable order."""
    return _SUPPORTED_EVENTS


def validate_task_event_trigger_configuration(configuration: TaskEventTriggerConfiguration) -> None:
    """Require each selected task event to be supported and listed once."""
    seen: set[str] = set()
    for event in configuration.events:
        if event not in _SUPPORTED_EVENTS:
            raise ValueError(f"ClickUp webhook event {event!r} is not a supported task event")
        if event in seen:
            raise ValueError("ClickUp webhook events list one event twice")
        seen.add(event)


def _connection_client(connection: Connection) -> Client:
    client = getattr(connection, "_client", None)
    if not isinstance(client, Client):
        raise ValueError("ClickUp taskEvent requires a connection bound to a ClickUp Client")
    return client


async def _verify_request(
    request: WebhookRequest, credentials: Credentials, cancellation: TriggerCancellation
) -> None:
    secret = credentials.webhook_secret.reveal() if credentials.webhook_secret is not None else ""
    if not secret:
        raise WebhookVerificationUnavailableError()
    signature = (request.header("X-Signature") or "").strip()
    if _SIGNATURE.fullmatch(signature) is None:
        raise ValueError("X-Signature is missing or is not a hex SHA-256 signature")
    expected = hmac.new(secret.encode(), request.body, hashlib.sha256).digest()
    if not hmac.compare_digest(bytes.fromhex(signature), expected):
        raise ValueError("X-Signature does not match the body")


def _identifier(value: str) -> bool:
    return (
        bool(value)
        and len(value.encode("utf-8")) <= 64
        and all("!" <= character <= "~" and character != ":" for character in value)
    )


def _history_item(value: object) -> TaskHistoryItem:
    if not isinstance(value, dict):
        raise ValueError("ClickUp history item is invalid")
    identifier = flexible_id(value.get("id"))
    if not _identifier(identifier):
        raise ValueError("ClickUp history item has no valid id")
    field = value.get("field", "")
    if not isinstance(field, str):
        raise ValueError("ClickUp history item field is invalid")
    parent_id = flexible_id(value.get("parent_id"))
    occurred_at = unix_milliseconds(value.get("date"))
    user = value.get("user")
    user_id = flexible_integer(user.get("id", 0)) if isinstance(user, dict) else 0
    before = value.get("before")
    after = value.get("after")
    status_before = before.get("status") if field == "status" and isinstance(before, dict) else None
    status_after = after.get("status") if field == "status" and isinstance(after, dict) else None
    return TaskHistoryItem(
        id=identifier,
        field=field,
        occurred_at=occurred_at or datetime.min.replace(tzinfo=UTC),
        user_id=user_id if user_id > 0 else 0,
        parent_id=parent_id,
        status_before=status_before if isinstance(status_before, str) else "",
        status_after=status_after if isinstance(status_after, str) else "",
    )


def _decode_request(request: WebhookRequest) -> TriggerEvent[TaskEvent] | None:
    try:
        wire = json.loads(
            request.body.decode("utf-8", "replace"),
            parse_constant=lambda _: (_ for _ in ()).throw(ValueError("invalid constant")),
        )
    except ValueError as error:
        raise ValueError("ClickUp webhook body is not a JSON object") from error
    if not isinstance(wire, dict):
        raise ValueError("ClickUp webhook body is not a JSON object")
    event_name = wire.get("event")
    webhook_id = wire.get("webhook_id")
    if (
        not isinstance(event_name, str)
        or not event_name
        or not isinstance(webhook_id, str)
        or not _identifier(webhook_id)
    ):
        raise ValueError("ClickUp webhook event has no event name or webhook_id")
    if event_name not in _SUPPORTED_EVENTS:
        return None
    task_id = flexible_id(wire.get("task_id"))
    if _TASK_ID.fullmatch(task_id) is None:
        raise ValueError("ClickUp task event has no valid task_id")
    raw_history = wire.get("history_items")
    if raw_history is not None and not isinstance(raw_history, list):
        raise ValueError("ClickUp task event history_items is invalid")
    history = tuple(_history_item(item) for item in (raw_history or [])[:20])
    occurred_at = (
        history[0].occurred_at
        if history and history[0].occurred_at != datetime.min.replace(tzinfo=UTC)
        else request.received_at.astimezone(UTC)
    )
    event_id = f"{webhook_id}:{event_name}:{history[0].id if history else task_id}"
    payload = TaskEvent(event_id, webhook_id, event_name, task_id, history, occurred_at)
    return TriggerEvent(event_id, occurred_at, payload)


def _decode_stored_payload(payload_json: str, payload_bytes: bytes) -> TaskEvent:
    decoded = CodecRegistry().resolve(TaskEvent).decode(Value(WireKind.JSON, payload_json))
    if not isinstance(decoded, TaskEvent):
        raise ValueError("pending ClickUp taskEvent payload is invalid")
    return decoded


def task_event_webhook_endpoint(connection: Connection) -> WebhookEndpoint[Credentials, TaskEvent]:
    """Return one shared ASGI webhook endpoint for a ClickUp connection.

    Until its binding runs, it answers 503; bad signatures answer 400, oversized bodies 413.
    """
    client = _connection_client(connection)
    endpoints = _ENDPOINTS.setdefault(client, {})
    endpoint = endpoints.get(connection.reference)
    if endpoint is None:
        endpoint = WebhookEndpoint(
            WebhookEndpointConfig(
                connector_id=CONNECTOR_ID,
                trigger_name=_TRIGGER_NAME,
                connection=connection.reference,
                credentials=client.credentials,
                verify_request=_verify_request,
                decode_event=_decode_request,
                maximum_body_bytes=client.webhook_max_body_bytes,
                clock=client.now,
            )
        )
        endpoints[connection.reference] = endpoint
    return endpoint


def new_task_event_trigger(
    *,
    connection: Connection,
    binding_name: str,
    target: TriggerTarget[TaskEvent],
    configuration: TaskEventTriggerConfiguration | None = None,
    logger: TriggerLogger | None = None,
) -> TriggerRunner:
    """Run one taskEvent binding; wrap target in a durable inbox for production."""
    if not binding_name.strip() or target is None:
        raise ValueError("ClickUp taskEvent binding name and target are required")
    selected = configuration or TaskEventTriggerConfiguration()
    validate_task_event_trigger_configuration(selected)
    endpoint = task_event_webhook_endpoint(connection)
    return new_trigger(
        TriggerConfig(
            definition=TASK_EVENT_TRIGGER_DEFINITION,
            binding=TriggerBindingRef(
                connection.reference, TASK_EVENT_TRIGGER_DEFINITION.trigger, binding_name
            ),
            source=endpoint.new_source(
                lambda event: not selected.events or event.payload.event in selected.events
            ),
            target=target,
            logger=logger,
        )
    )


@dataclass(frozen=True, slots=True)
class ProjectTaskEventTriggerRoute:
    """One stored binding and its application target."""

    binding_name: str
    """Application binding name stored in Dex project configuration."""
    target: TriggerTarget[TaskEvent]
    """Target receiving verified task events after durable inbox delivery."""


class TaskEventEndpointRunner:
    """Serve ClickUp's endpoint and replay each binding's durable inbox."""

    def __init__(self, connection: Connection, runners: Sequence[TriggerRunner]) -> None:
        if not runners:
            raise ValueError("ClickUp taskEvent routes are required")
        self._endpoint = task_event_webhook_endpoint(connection)
        self._runner = WebhookEndpointRunner(self._endpoint, runners)

    async def __call__(self, scope: _ASGIScope, receive: _ASGIReceive, send: _ASGISend) -> None:
        """Serve ASGI HTTP and lifespan scopes."""
        await self._runner(scope, receive, send)

    async def handle_request(
        self,
        method: str,
        url: str,
        headers: Iterable[tuple[str, str]],
        body: bytes,
        cancellation: TriggerCancellation | None = None,
    ) -> WebhookResponse:
        """Verify and persist one complete webhook request before acknowledgment."""
        return await self._runner.handle_request(method, url, headers, body, cancellation)

    async def run(self, context: TriggerContext | None = None) -> None:
        """Replay pending inbox events and then receive new webhooks until cancelled."""
        await self._runner.run(context)

    def running_source_count(self) -> int:
        """Return the number of active binding sources."""
        return self._endpoint.running_source_count()


def new_project_task_event_endpoint_runner(
    project: LoadedProject,
    connection: Connection,
    routes: Sequence[ProjectTaskEventTriggerRoute],
    logger: TriggerLogger | None = None,
) -> TaskEventEndpointRunner:
    """Load binding filters and persist each event before answering ClickUp 200."""
    if project is None:
        raise ValueError("ClickUp taskEvent runner requires loaded project configuration")
    if not routes:
        raise ValueError("ClickUp taskEvent routes are required")
    _connection_client(connection)
    runners: list[TriggerRunner] = []
    names: set[str] = set()
    for route in routes:
        name = route.binding_name.strip()
        if not name or route.target is None or name in names:
            raise ValueError("ClickUp taskEvent binding name and target are required and unique")
        names.add(name)
        key = TriggerInboxKey(CONNECTOR_ID, connection.reference.name, _TRIGGER_NAME, name)
        try:
            selected = decode_trigger_configuration(
                project.configuration, key, decode_task_event_trigger_configuration
            )
            validate_task_event_trigger_configuration(selected)
        except ValueError as error:
            raise ValueError(f"ClickUp taskEvent binding {name!r} configuration: {error}") from error
        target = new_durable_trigger_target(
            project.trigger_inbox(key), route.target, decode_payload=_decode_stored_payload, logger=logger
        )
        runners.append(
            new_task_event_trigger(
                connection=connection, binding_name=name, configuration=selected, target=target, logger=logger
            )
        )
    return TaskEventEndpointRunner(connection, runners)


__all__ = [
    "TASK_EVENT_TRIGGER_DEFINITION",
    "TaskEvent",
    "TaskHistoryItem",
    "TaskEventTriggerConfiguration",
    "ProjectTaskEventTriggerRoute",
    "TaskEventEndpointRunner",
    "task_events",
    "validate_task_event_trigger_configuration",
    "define_task_event_trigger_binding",
    "decode_task_event_trigger_configuration",
    "task_event_webhook_endpoint",
    "new_task_event_trigger",
    "new_project_task_event_endpoint_runner",
]
