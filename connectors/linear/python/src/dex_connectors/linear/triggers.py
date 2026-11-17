# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Receive signed Linear Issue webhooks and deliver them through a durable inbox."""

from __future__ import annotations

import hashlib
import hmac
import json
import math
import re
import weakref
from collections.abc import Awaitable, Callable, Iterable, MutableMapping, Sequence
from dataclasses import dataclass, fields
from datetime import UTC, datetime
from functools import partial
from typing import Any, Final, NoReturn, TypeAlias

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

from ._generated.definitions import CONNECTOR_ID, ISSUE_EVENT_RECEIVED_TRIGGER_DEFINITION
from ._generated.triggers import (
    decode_issue_event_received_trigger_configuration,
)
from ._generated.triggers import (
    define_issue_event_received_trigger_binding as define_issue_event_received_trigger_binding,
)
from .client import Client, Credentials
from .connection import Connection
from .models import (
    IssueEvent,
    IssueEventActor,
    IssueEventIssue,
    IssueEventReceivedTriggerConfiguration,
    IssueSummary,
)
from .wire import issue_summary, linear_time, uuid_field

ISSUE_EVENT_ACTION_CREATE: Final = "create"
ISSUE_EVENT_ACTION_UPDATE: Final = "update"
ISSUE_EVENT_ACTION_REMOVE: Final = "remove"
_SUPPORTED_ACTIONS: Final = (
    ISSUE_EVENT_ACTION_CREATE,
    ISSUE_EVENT_ACTION_UPDATE,
    ISSUE_EVENT_ACTION_REMOVE,
)
_TOKEN: Final = re.compile(r"[A-Za-z0-9_.:-]{1,64}\Z", re.ASCII)
_SIGNATURE: Final = re.compile(r"[0-9a-fA-F]{64}\Z", re.ASCII)
_TRIGGER_NAME: Final = ISSUE_EVENT_RECEIVED_TRIGGER_DEFINITION.trigger.trigger_name
_ASGIScope: TypeAlias = MutableMapping[str, Any]
_ASGIReceive: TypeAlias = Callable[[], Awaitable[MutableMapping[str, Any]]]
_ASGISend: TypeAlias = Callable[[MutableMapping[str, Any]], Awaitable[None]]
_ENDPOINTS: weakref.WeakKeyDictionary[
    Client, dict[ConnectionRef, WebhookEndpoint[Credentials, IssueEvent]]
] = weakref.WeakKeyDictionary()


def validate_issue_event_received_trigger_configuration(
    configuration: IssueEventReceivedTriggerConfiguration,
) -> None:
    """Require distinct create, update, or remove actions and an optional team UUID."""
    if len(set(configuration.actions)) != len(configuration.actions) or any(
        action not in _SUPPORTED_ACTIONS for action in configuration.actions
    ):
        raise ValueError("issueEventReceived actions must be distinct create, update, or remove values")
    if configuration.team_id:
        uuid_field(configuration.team_id, "issueEventReceived teamId", True)


def _connection_client(connection: Connection) -> Client:
    client = getattr(connection, "_client", None)
    if not isinstance(client, Client):
        raise ValueError("Linear issueEventReceived requires a Linear Client connection")
    return client


async def _verify_request(
    request: WebhookRequest, credentials: Credentials, cancellation: TriggerCancellation, *, tolerance: float
) -> None:
    secret = credentials.webhook_signing_secret.reveal()
    if not secret:
        raise WebhookVerificationUnavailableError()
    signature = (request.header("Linear-Signature") or "").strip()
    if _SIGNATURE.fullmatch(signature) is None:
        raise ValueError("Linear-Signature is missing or invalid")
    expected = hmac.new(secret.encode(), request.body, hashlib.sha256).digest()
    if not hmac.compare_digest(bytes.fromhex(signature), expected):
        raise ValueError("Linear-Signature does not match the body")
    try:
        wire = json.loads(request.body, parse_constant=_reject_json_constant)
        timestamp = wire["webhookTimestamp"]
        if isinstance(timestamp, bool) or not isinstance(timestamp, (int, float)):
            raise ValueError
        if not math.isfinite(timestamp) or timestamp <= 0 or timestamp > (2**63 - 1) / 2:
            raise ValueError
        sent_at = datetime.fromtimestamp(int(timestamp) / 1000, tz=UTC)
    except (ValueError, TypeError, KeyError, OverflowError, AttributeError) as error:
        raise ValueError("Linear webhookTimestamp is invalid") from error
    if abs((request.received_at - sent_at).total_seconds()) > tolerance:
        raise ValueError("Linear webhookTimestamp is stale")


def _reject_json_constant(value: str) -> NoReturn:
    raise ValueError("Linear webhook JSON constant is invalid")


def _required_string(wire: dict[str, Any], key: str) -> str:
    value = wire.get(key, "")
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError("Linear webhook body is malformed")
    return value


def _optional_string(wire: dict[str, Any], key: str) -> str:
    value = wire.get(key)
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError("Linear webhook body is malformed")
    return value


def _decode_issue(raw: object) -> IssueEventIssue:
    if not isinstance(raw, dict):
        raise ValueError("Linear webhook issue is malformed")
    wire = dict(raw)
    for key in ("teamId", "stateId", "assigneeId", "projectId", "cycleId", "parentId"):
        if wire.get(key) is not None and not isinstance(wire[key], str):
            raise ValueError("Linear webhook issue is malformed")
    for reference, nested in (("teamId", "team"), ("stateId", "state")):
        if wire.get(nested) is None and isinstance(wire.get(reference), str):
            try:
                uuid_field(wire[reference], reference, True)
            except ValueError:
                pass
            else:
                wire[nested] = {"id": wire[reference]}
    if wire.get("assignee") is None and isinstance(wire.get("assigneeId"), str):
        wire["assignee"] = {"id": wire["assigneeId"]}
    try:
        summary = issue_summary(wire)
        number = wire.get("number", 0)
        if number is None:
            number = 0
        if type(number) is not int or not 0 <= number <= 2**31 - 1:
            raise ValueError
        trashed = wire.get("trashed")
        if trashed is not None and type(trashed) is not bool:
            raise ValueError
        values = {item.name: getattr(summary, item.name) for item in fields(IssueSummary)}
        return IssueEventIssue(
            **values,
            number=number,
            project_id=_optional_string(wire, "projectId"),
            cycle_id=_optional_string(wire, "cycleId"),
            parent_id=_optional_string(wire, "parentId"),
            trashed=trashed is True,
        )
    except (TypeError, ValueError) as error:
        raise ValueError("Linear webhook issue is malformed") from error


def _decode_request(request: WebhookRequest) -> TriggerEvent[IssueEvent] | None:
    try:
        wire = json.loads(request.body, parse_constant=_reject_json_constant)
    except (ValueError, UnicodeDecodeError) as error:
        raise ValueError("Linear webhook body is malformed") from error
    if not isinstance(wire, dict):
        raise ValueError("Linear webhook body is malformed")
    action = _required_string(wire, "action")
    entity_type = _required_string(wire, "type")
    if not entity_type or _TOKEN.fullmatch(action) is None:
        raise ValueError("Linear webhook body is malformed")
    if entity_type != "Issue":
        return None
    try:
        occurred_at_text = linear_time(_required_string(wire, "createdAt"))
        occurred_at = datetime.fromisoformat(occurred_at_text.replace("Z", "+00:00"))
        issue = _decode_issue(wire.get("data"))
        updated_at = datetime.fromisoformat(issue.updated_at.replace("Z", "+00:00"))
        actor_wire = wire.get("actor")
        actor = None
        if actor_wire is not None:
            if not isinstance(actor_wire, dict):
                raise ValueError
            actor = IssueEventActor(
                id=_required_string(actor_wire, "id"),
                name=_required_string(actor_wire, "name"),
                type=_required_string(actor_wire, "type"),
            )
        changed = wire.get("updatedFrom")
        if changed is not None and not isinstance(changed, dict):
            raise ValueError
        changed = changed or {}
        fields_selected = tuple(sorted(key for key in changed if _TOKEN.fullmatch(key))[:50])
        previous_state = changed.get("stateId")
        if not isinstance(previous_state, str):
            previous_state = ""
        try:
            uuid_field(previous_state, "previousStateId", True)
        except ValueError:
            previous_state = ""
        event = IssueEvent(
            action=action,
            occurred_at=occurred_at_text,
            webhook_id=_required_string(wire, "webhookId"),
            organization_id=_required_string(wire, "organizationId"),
            actor=actor,
            issue=issue,
            updated_fields=fields_selected,
            previous_state_id=previous_state,
        )
        event_id = f"{action}:{issue.id}:{int(updated_at.timestamp() * 1000)}"
        return TriggerEvent(event_id, occurred_at, event)
    except (TypeError, ValueError) as error:
        raise ValueError("Linear webhook body is malformed") from error


def _decode_stored_payload(payload_json: str, payload_bytes: bytes) -> IssueEvent:
    decoded = CodecRegistry().resolve(IssueEvent).decode(Value(WireKind.JSON, payload_json))
    if not isinstance(decoded, IssueEvent):
        raise ValueError("pending Linear issueEventReceived payload is invalid")
    return decoded


def issue_event_received_webhook_endpoint(connection: Connection) -> WebhookEndpoint[Credentials, IssueEvent]:
    """Return one shared signed Issue webhook endpoint for a Linear connection."""
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
                verify_request=partial(
                    _verify_request, tolerance=client.config.webhook_signature_tolerance_seconds
                ),
                decode_event=_decode_request,
                maximum_body_bytes=client.config.webhook_max_body_bytes,
                clock=client.now,
            )
        )
        endpoints[connection.reference] = endpoint
    return endpoint


def new_issue_event_received_trigger(
    *,
    connection: Connection,
    binding_name: str,
    target: TriggerTarget[IssueEvent],
    configuration: IssueEventReceivedTriggerConfiguration | None = None,
    logger: TriggerLogger | None = None,
) -> TriggerRunner:
    """Run one filtered Issue binding; production targets should use a durable inbox."""
    if not binding_name.strip() or target is None:
        raise ValueError("Linear issueEventReceived binding name and target are required")
    selected = configuration or IssueEventReceivedTriggerConfiguration()
    validate_issue_event_received_trigger_configuration(selected)
    endpoint = issue_event_received_webhook_endpoint(connection)
    return new_trigger(
        TriggerConfig(
            definition=ISSUE_EVENT_RECEIVED_TRIGGER_DEFINITION,
            binding=TriggerBindingRef(
                connection.reference, ISSUE_EVENT_RECEIVED_TRIGGER_DEFINITION.trigger, binding_name
            ),
            source=endpoint.new_source(
                lambda event: (
                    (not selected.actions or event.payload.action in selected.actions)
                    and (
                        not selected.team_id
                        or selected.team_id.lower() == event.payload.issue.team.id.lower()
                    )
                )
            ),
            target=target,
            logger=logger,
        )
    )


@dataclass(frozen=True, slots=True)
class ProjectIssueEventReceivedTriggerRoute:
    """One stored binding and its application target."""

    binding_name: str
    """Application binding name stored in Dex project configuration."""
    target: TriggerTarget[IssueEvent]
    """Target receiving verified Issue events after durable inbox delivery."""


class IssueEventReceivedEndpointRunner:
    """Serve Linear's endpoint and replay each binding's durable inbox."""

    def __init__(self, connection: Connection, runners: Sequence[TriggerRunner]) -> None:
        """Bind one shared endpoint and at least one configured Trigger runner."""
        if not runners:
            raise ValueError("Linear issueEventReceived routes are required")
        self._endpoint = issue_event_received_webhook_endpoint(connection)
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
        """Verify and persist one webhook before acknowledgment."""
        return await self._runner.handle_request(method, url, headers, body, cancellation)

    async def run(self, context: TriggerContext | None = None) -> None:
        """Replay pending events and receive new webhooks until cancelled."""
        await self._runner.run(context)

    def running_source_count(self) -> int:
        """Return the number of active binding sources."""
        return self._endpoint.running_source_count()


def new_project_issue_event_received_endpoint_runner(
    project: LoadedProject,
    connection: Connection,
    routes: Sequence[ProjectIssueEventReceivedTriggerRoute],
    logger: TriggerLogger | None = None,
) -> IssueEventReceivedEndpointRunner:
    """Load binding filters and persist each verified event before answering Linear 200."""
    if project is None or not routes:
        raise ValueError("Linear issueEventReceived project and routes are required")
    _connection_client(connection)
    runners: list[TriggerRunner] = []
    names: set[str] = set()
    for route in routes:
        name = route.binding_name.strip()
        if not name or route.target is None or name in names:
            raise ValueError("Linear issueEventReceived binding names and targets must be unique")
        names.add(name)
        key = TriggerInboxKey(CONNECTOR_ID, connection.reference.name, _TRIGGER_NAME, name)
        try:
            selected = decode_trigger_configuration(
                project.configuration, key, decode_issue_event_received_trigger_configuration
            )
            validate_issue_event_received_trigger_configuration(selected)
        except ValueError as error:
            raise ValueError(f"Linear issueEventReceived binding {name!r} configuration: {error}") from error
        target = new_durable_trigger_target(
            project.trigger_inbox(key), route.target, decode_payload=_decode_stored_payload, logger=logger
        )
        runners.append(
            new_issue_event_received_trigger(
                connection=connection, binding_name=name, configuration=selected, target=target, logger=logger
            )
        )
    return IssueEventReceivedEndpointRunner(connection, runners)
