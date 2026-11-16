# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Receive signed Calendly invitee webhooks through the ``inviteeEventReceived`` Trigger.

Each connection has one shared webhook endpoint that verifies the
``Calendly-Webhook-Signature`` header with the connection's ``webhook_signing_key``
and decodes ``invitee.created`` and ``invitee.canceled`` events. Every binding of the
Trigger runs one source of that endpoint.
`new_project_invitee_event_received_endpoint_runner` wires stored bindings to durable
project inboxes, so an event is recorded before Calendly receives 200.
"""

import hashlib
import hmac
import json
import re
from collections.abc import Callable, Iterable, Sequence
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Any, Final

import dex
import httpx
from dex_connectors.sdk import (
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
    Configuration,
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

from ._generated.definitions import CONNECTOR_ID, INVITEE_EVENT_RECEIVED_TRIGGER_DEFINITION
from ._generated.triggers import (
    decode_invitee_event_received_trigger_configuration,
    define_invitee_event_received_trigger_binding,
)
from .client import API_BASE, Client, Credentials
from .connection import Connection, new_project_connection
from .models import InviteeEvent, InviteeEventReceivedTriggerConfiguration
from .operations import _event, _invitee, _object, _resource_id, _string, _timestamp

WEBHOOK_EVENT_INVITEE_CREATED: Final = "invitee.created"
"""The Calendly webhook event sent when an invitee books an event."""
WEBHOOK_EVENT_INVITEE_CANCELED: Final = "invitee.canceled"
"""The Calendly webhook event sent when an invitee or host cancels."""
UI_UNIT_EVENT_TYPE_PICKER: Final = "eventTypePicker"
"""Calendly event type picker UI unit, which stores a stable event type URI."""
UI_EVENT_TYPE_PICKER_PORT_EVENT_TYPE_URI: Final = "eventTypeUri"
"""Event type picker output port."""

# Carries t=<unix seconds>,v1=<hex HMAC-SHA256 of "t.body">.
_SIGNATURE_HEADER: Final = "Calendly-Webhook-Signature"
_SUPPORTED_EVENTS: Final = (WEBHOOK_EVENT_INVITEE_CREATED, WEBHOOK_EVENT_INVITEE_CANCELED)
_TRIGGER_NAME: Final = INVITEE_EVENT_RECEIVED_TRIGGER_DEFINITION.trigger.trigger_name
_INVITEE_URI: Final = re.compile(
    re.escape(API_BASE) + r"/scheduled_events/([A-Za-z0-9_-]{1,64})/invitees/([A-Za-z0-9_-]{1,64})", re.ASCII
)
_GO_INTEGER: Final = re.compile(r"[+-]?[0-9]+", re.ASCII)
_SHA256_HEX: Final = re.compile(r"[0-9a-fA-F]{64}", re.ASCII)
# Go's unicode.IsSpace set; str.strip also trims U+001C-U+001F.
_GO_SPACE: Final = "\t\n\v\f\r \x85\xa0                　"
_INT64_MINIMUM: Final = -(2**63)
_INT64_MAXIMUM: Final = 2**63 - 1
_UNIX_EPOCH: Final = datetime(1970, 1, 1, tzinfo=UTC)


class _InvalidSignatureError(ValueError):
    def __init__(self) -> None:
        super().__init__("Calendly-Webhook-Signature is missing, malformed, stale, or does not match")


def validate_invitee_event_received_trigger_configuration(
    configuration: InviteeEventReceivedTriggerConfiguration,
) -> None:
    """Check that events holds distinct supported webhook events and event_type_uri is an event type URI.

    Empty events accept both ``invitee.created`` and ``invitee.canceled``, and a blank
    event_type_uri accepts every event type. Raises ValueError otherwise.
    """
    events = configuration.events
    if any(event not in _SUPPORTED_EVENTS for event in events) or len(set(events)) != len(events):
        raise ValueError(
            "inviteeEventReceived events must be distinct values from invitee.created and invitee.canceled"
        )
    if configuration.event_type_uri:
        _resource_id(configuration.event_type_uri, "event_types", "event type")


def _accepts_event(
    configuration: InviteeEventReceivedTriggerConfiguration, event: TriggerEvent[InviteeEvent]
) -> bool:
    if configuration.events and event.payload.event not in configuration.events:
        return False
    return (
        not configuration.event_type_uri
        or configuration.event_type_uri == event.payload.scheduled_event.event_type_uri
    )


def _verify_signature(
    body: bytes, header: str, signing_key: str, now: datetime, tolerance: timedelta
) -> None:
    """Require a fresh t= and a v1= equal to hex HMAC-SHA256("t.body"); never say which check failed."""
    timestamp_text = ""
    signatures: list[bytes] = []
    for component in header.split(","):
        key, separator, value = component.strip(_GO_SPACE).partition("=")
        if not separator:
            continue
        if key == "t":
            timestamp_text = value
        elif key == "v1" and _SHA256_HEX.fullmatch(value) is not None:
            signatures.append(bytes.fromhex(value))
    if _GO_INTEGER.fullmatch(timestamp_text) is None or not signatures:
        raise _InvalidSignatureError()
    timestamp = int(timestamp_text)
    if timestamp <= 0 or timestamp > _INT64_MAXIMUM:
        raise _InvalidSignatureError()
    age_microseconds = (now.astimezone(UTC) - _UNIX_EPOCH) // timedelta(
        microseconds=1
    ) - timestamp * 1_000_000
    if abs(age_microseconds) > tolerance // timedelta(microseconds=1):
        raise _InvalidSignatureError()
    expected = hmac.new(signing_key.encode(), timestamp_text.encode() + b"." + body, hashlib.sha256).digest()
    if not any(hmac.compare_digest(signature, expected) for signature in signatures):
        raise _InvalidSignatureError()


async def _verify_request(
    tolerance: timedelta, request: WebhookRequest, credentials: Credentials, cancellation: TriggerCancellation
) -> None:
    # A blank signing key answers 503, so Calendly keeps retrying the delivery.
    signing_key = (
        credentials.webhook_signing_key.reveal() if credentials.webhook_signing_key is not None else ""
    )
    if not signing_key:
        raise WebhookVerificationUnavailableError()
    _verify_signature(
        request.body, request.header(_SIGNATURE_HEADER) or "", signing_key, request.received_at, tolerance
    )


def _envelope_string(envelope: dict[str, object], name: str) -> str:
    value = envelope.get(name)
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError("invalid Calendly webhook")
    return value


def _decode_invitee_webhook(body: bytes) -> TriggerEvent[InviteeEvent] | None:
    """Decode a verified delivery; another event, such as routing_form_submission.created, has no record."""
    try:
        # Like Go's encoding/json, invalid UTF-8 inside strings becomes U+FFFD.
        envelope = json.loads(body.decode("utf-8", errors="replace"))
    except ValueError as error:
        raise ValueError("invalid Calendly webhook") from error
    if not isinstance(envelope, dict):
        raise ValueError("invalid Calendly webhook")
    event = _envelope_string(envelope, "event")
    created_at_text = _envelope_string(envelope, "created_at")
    # Go reads the payload as json.RawMessage: present, even as null, but not yet decoded.
    if not event or "payload" not in envelope:
        raise ValueError("invalid Calendly webhook")
    if event not in _SUPPORTED_EVENTS:
        return None
    created_at = _timestamp(created_at_text)
    payload = _object(envelope["payload"])
    if payload.get("scheduled_event") is None:
        raise ValueError("invalid Calendly webhook")
    invitee = _invitee(payload, _string(payload, "event"))
    scheduled_event = _event(payload["scheduled_event"])
    identifiers = _INVITEE_URI.fullmatch(invitee.uri)
    if identifiers is None or scheduled_event.uri != invitee.scheduled_event_uri:
        raise ValueError("invalid Calendly webhook")
    return TriggerEvent(
        f"{event}:{identifiers.group(1)}:{identifiers.group(2)}",
        created_at,
        InviteeEvent(event=event, created_at=created_at, invitee=invitee, scheduled_event=scheduled_event),
    )


def _decode_webhook_request(request: WebhookRequest) -> TriggerEvent[InviteeEvent] | None:
    return _decode_invitee_webhook(request.body)


def _decode_pending_invitee_event(payload_json: str, payload_bytes: bytes) -> InviteeEvent:
    """Decode a replayed inbox payload that the Go connector or this package wrote."""
    decoded = dex.CodecRegistry().resolve(InviteeEvent).decode(dex.Value(dex.WireKind.JSON, payload_json))
    if not isinstance(decoded, InviteeEvent):
        raise ValueError("pending Calendly invitee event is invalid")
    return decoded


def _connection_client(connection: Connection) -> Client:
    client = getattr(connection, "_client", None) if connection is not None else None
    if not isinstance(client, Client):
        raise ValueError(
            "Calendly Triggers require a connection from new_project_connection, "
            "or new_connection with a Client"
        )
    return client


def _webhook_endpoint(
    connection: Connection, logger: TriggerLogger | None = None
) -> WebhookEndpoint[Credentials, InviteeEvent]:
    """Return the connection's shared endpoint, creating it on its client's first use.

    logger receives the endpoint's delivery records when this call creates it, like Go's WithLogger.
    """
    client = _connection_client(connection)
    reference = connection.reference
    endpoint = client.invitee_event_endpoints.get(reference)
    if endpoint is not None:
        return endpoint
    tolerance = timedelta(seconds=client.config.webhook_signature_tolerance_seconds)

    async def verify_request(
        request: WebhookRequest, credentials: Credentials, cancellation: TriggerCancellation
    ) -> None:
        await _verify_request(tolerance, request, credentials, cancellation)

    try:
        endpoint = WebhookEndpoint(
            WebhookEndpointConfig(
                connector_id=CONNECTOR_ID,
                trigger_name=_TRIGGER_NAME,
                connection=reference,
                credentials=client.credentials,
                verify_request=verify_request,
                decode_event=_decode_webhook_request,
                maximum_body_bytes=client.config.webhook_max_body_bytes,
                credential_refresh=client.refresh_driver,
                clock=client.now,
                logger=logger,
            )
        )
    except ValueError as error:
        raise ValueError(f"Calendly webhook endpoint: {error}") from error
    client.invitee_event_endpoints[reference] = endpoint
    return endpoint


def invitee_event_received_webhook_handler(
    connection: Connection,
) -> WebhookEndpoint[Credentials, InviteeEvent]:
    """Return the connection's webhook endpoint, an ASGI 3 application, for the subscription's callback URL.

    Every ``inviteeEventReceived`` Trigger built from this connection feeds from it,
    and it answers 503 while none of them runs, so Calendly retries. Raises
    ValueError for a mock connection, which has no signing key.
    """
    return _webhook_endpoint(connection)


def new_invitee_event_received_trigger(
    *,
    connection: Connection,
    binding_name: str,
    configuration: InviteeEventReceivedTriggerConfiguration,
    target: TriggerTarget[InviteeEvent],
    logger: TriggerLogger | None = None,
) -> TriggerRunner:
    """Create one ``inviteeEventReceived`` binding's runner on the connection's shared endpoint.

    The runner receives events only while `run` is active, so serve
    `invitee_event_received_webhook_handler` of the same connection. Events the
    configuration filters out are acknowledged and dropped. Raises ValueError for an
    invalid configuration or a mock connection. Wrap target with
    `new_durable_trigger_target` so an event is recorded before Calendly is
    acknowledged, as `new_project_invitee_event_received_endpoint_runner` does.
    """
    validate_invitee_event_received_trigger_configuration(configuration)
    endpoint = _webhook_endpoint(connection, logger)

    def accepts_event(event: TriggerEvent[InviteeEvent]) -> bool:
        return _accepts_event(configuration, event)

    return new_trigger(
        TriggerConfig(
            definition=INVITEE_EVENT_RECEIVED_TRIGGER_DEFINITION,
            binding=TriggerBindingRef(
                connection.reference, INVITEE_EVENT_RECEIVED_TRIGGER_DEFINITION.trigger, binding_name
            ),
            source=endpoint.new_source(accepts_event),
            target=target,
            logger=logger,
        )
    )


@dataclass(frozen=True, slots=True)
class ProjectInviteeEventReceivedTriggerRoute:
    """Bind one stored ``inviteeEventReceived`` binding to its application target."""

    binding_name: str
    """The stored Trigger binding name whose configuration filters the events."""
    target: TriggerTarget[InviteeEvent]
    """Receives the binding's events through its durable project inbox, such as a Dex Flow target."""


_DurableInviteeTargetFactory = Callable[
    [TriggerInboxKey, TriggerTarget[InviteeEvent]], TriggerTarget[InviteeEvent]
]


class InviteeEventReceivedEndpointRunner:
    """Serve one connection's Calendly webhook endpoint while running its bindings.

    It is an ASGI 3 application: mount it at the subscription's callback URL and
    serve it with an ASGI server such as uvicorn, whose ``lifespan`` startup runs the
    bindings on the server's event loop and whose shutdown stops them. Until every
    binding runs, verified deliveries answer 503 so Calendly retries them. Close it
    with `aclose` after the server stops; it closes the connection it opened.
    """

    def __init__(
        self,
        endpoint_runner: WebhookEndpointRunner,
        endpoint: WebhookEndpoint[Credentials, InviteeEvent],
        connection: Connection | None = None,
    ) -> None:
        """Wrap an endpoint runner; applications use `new_project_invitee_event_received_endpoint_runner`."""
        if endpoint_runner is None or endpoint is None:
            raise ValueError("Calendly inviteeEventReceived endpoint runner is required")
        self._endpoint_runner = endpoint_runner
        self._endpoint = endpoint
        self._owned_connection = connection

    async def __call__(self, scope: Any, receive: Any, send: Any) -> None:
        """Serve the ASGI ``http`` and ``lifespan`` scopes."""
        await self._endpoint_runner(scope, receive, send)

    async def handle_request(
        self,
        method: str,
        url: str,
        headers: Iterable[tuple[str, str]],
        body: bytes,
        cancellation: TriggerCancellation | None = None,
    ) -> WebhookResponse:
        """Verify, record, and acknowledge one delivery with its complete raw body, for non-ASGI hosts."""
        return await self._endpoint_runner.handle_request(method, url, headers, body, cancellation)

    async def run(self, context: TriggerContext | None = None) -> None:
        """Replay each binding's durable inbox, then deliver new events until context is cancelled.

        Use it instead of the ASGI ``lifespan`` when the host owns the event loop.
        Raises TriggerDeliveryAbortedError after cancellation, or the first binding's failure.
        """
        await self._endpoint_runner.run(context)

    def running_source_count(self) -> int:
        """Return how many bindings receive events; while it is zero every delivery answers 503."""
        return self._endpoint.running_source_count()

    async def aclose(self) -> None:
        """Close the connection this runner opened, after the bindings stop."""
        if self._owned_connection is not None:
            await self._owned_connection.aclose()


async def new_project_invitee_event_received_endpoint_runner(
    project: LoadedProject,
    connection_name: str,
    routes: Sequence[ProjectInviteeEventReceivedTriggerRoute],
    http_client: httpx.AsyncClient | None = None,
    logger: TriggerLogger | None = None,
) -> InviteeEventReceivedEndpointRunner:
    """Open connection_name and every route's stored binding, and serve them from one endpoint.

    Each route's target receives events through its binding's durable project inbox,
    which records an event before Calendly receives 200 and replays it after a
    restart. Raises ValueError for empty routes, a blank or repeated binding name, or
    an invalid stored configuration, and ObjectNotFoundError for a binding the
    project does not store. logger receives the endpoint's, bindings', and inboxes'
    records.
    Close the runner with `aclose`.
    """
    if project is None:
        raise ValueError("Calendly inviteeEventReceived endpoint requires the loaded project configuration")
    connection = new_project_connection(project, connection_name, http_client)

    def make_durable(
        key: TriggerInboxKey, target: TriggerTarget[InviteeEvent]
    ) -> TriggerTarget[InviteeEvent]:
        return new_durable_trigger_target(
            project.trigger_inbox(key), target, decode_payload=_decode_pending_invitee_event, logger=logger
        )

    try:
        endpoint_runner = _new_endpoint_runner(
            connection, project.configuration, routes, make_durable, logger
        )
    except BaseException:
        await connection.aclose()
        raise
    return InviteeEventReceivedEndpointRunner(endpoint_runner, _webhook_endpoint(connection), connection)


def _new_endpoint_runner(
    connection: Connection,
    configuration: Configuration,
    routes: Sequence[ProjectInviteeEventReceivedTriggerRoute],
    make_durable: _DurableInviteeTargetFactory,
    logger: TriggerLogger | None,
) -> WebhookEndpointRunner:
    if not routes:
        raise ValueError("Calendly inviteeEventReceived routes are required")
    endpoint = _webhook_endpoint(connection, logger)
    connection_name = connection.reference.name
    runners: list[TriggerRunner] = []
    binding_names: set[str] = set()
    for route in routes:
        binding_name = (route.binding_name or "").strip(_GO_SPACE)
        if not binding_name or route.target is None:
            raise ValueError("Calendly inviteeEventReceived binding name and target are required")
        if binding_name in binding_names:
            raise ValueError(
                f"Calendly inviteeEventReceived binding name {json.dumps(binding_name)} is duplicated"
            )
        binding_names.add(binding_name)
        key = TriggerInboxKey(CONNECTOR_ID, connection_name, _TRIGGER_NAME, binding_name)
        try:
            binding_configuration = decode_trigger_configuration(
                configuration, key, decode_invitee_event_received_trigger_configuration
            )
        except ValueError as error:
            raise ValueError(
                f"Calendly inviteeEventReceived binding {json.dumps(binding_name)} configuration: {error}"
            ) from error
        try:
            validate_invitee_event_received_trigger_configuration(binding_configuration)
        except ValueError as error:
            raise ValueError(f"binding {json.dumps(binding_name)}: {error}") from error
        runners.append(
            new_invitee_event_received_trigger(
                connection=connection,
                binding_name=binding_name,
                configuration=binding_configuration,
                target=make_durable(key, route.target),
                logger=logger,
            )
        )
    return WebhookEndpointRunner(endpoint, runners)


__all__ = [
    "INVITEE_EVENT_RECEIVED_TRIGGER_DEFINITION",
    "UI_EVENT_TYPE_PICKER_PORT_EVENT_TYPE_URI",
    "UI_UNIT_EVENT_TYPE_PICKER",
    "WEBHOOK_EVENT_INVITEE_CANCELED",
    "WEBHOOK_EVENT_INVITEE_CREATED",
    "InviteeEvent",
    "InviteeEventReceivedEndpointRunner",
    "InviteeEventReceivedTriggerConfiguration",
    "ProjectInviteeEventReceivedTriggerRoute",
    "decode_invitee_event_received_trigger_configuration",
    "define_invitee_event_received_trigger_binding",
    "invitee_event_received_webhook_handler",
    "new_invitee_event_received_trigger",
    "new_project_invitee_event_received_endpoint_runner",
    "validate_invitee_event_received_trigger_configuration",
]
