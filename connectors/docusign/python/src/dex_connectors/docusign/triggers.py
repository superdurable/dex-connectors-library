# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Receive DocuSign Connect envelope outcomes through the ``envelopeEventReceived`` webhook Trigger.

At behavior parity with the Go connector, one shared endpoint per connection:

- accepts only POST bodies up to ``connectMaxBodyBytes``, answering 413 above it before the signature is
  checked;
- accepts any ``X-DocuSign-Signature-<n>`` header equal to the body's base64 HMAC-SHA256 under the
  connection's ``connect_hmac_key`` (quotes removed), compared in constant time; without a key it answers
  503 so Connect retries, and a bad signature answers 400;
- decodes the JSON SIM ``envelope-completed``, ``envelope-declined``, and ``envelope-voided`` events, with
  the envelope ID and event name as the Trigger event ID, and acknowledges every other Connect event
  unrecorded; XML or malformed bodies answer 400;
- records each event in every accepting binding's durable inbox before answering 200.

`define_envelope_event_received_trigger_binding` declares a binding in a Flow's
`get_connector_trigger_bindings`; `new_project_envelope_event_received_endpoint_runner` wires every
binding of a project and is the usual entry point.
"""

import weakref
from collections.abc import Awaitable, Callable, Iterable, MutableMapping, Sequence
from dataclasses import dataclass
from typing import Any, TypeAlias

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
    Configuration,
    LoadedProject,
    ObjectNotFoundError,
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

from ._connect_event import (
    CONNECT_ENVELOPE_EVENTS,
    CONNECT_EVENT_ENVELOPE_COMPLETED,
    CONNECT_EVENT_ENVELOPE_DECLINED,
    CONNECT_EVENT_ENVELOPE_VOIDED,
    ConnectMessageMalformedError,
    ConnectSignatureInvalidError,
    decode_connect_message,
    envelope_event_custom_field_value,
    validate_envelope_event_received_trigger_configuration,
    verify_connect_signature,
)
from ._generated.definitions import CONNECTOR_ID, ENVELOPE_EVENT_RECEIVED_TRIGGER_DEFINITION
from ._generated.triggers import (
    decode_envelope_event_received_trigger_configuration,
    define_envelope_event_received_trigger_binding,
)
from .client import Client, Credentials
from .connection import Connection
from .models import EnvelopeEvent, EnvelopeEventReceivedTriggerConfiguration

_TRIGGER_NAME = ENVELOPE_EVENT_RECEIVED_TRIGGER_DEFINITION.trigger.trigger_name
_ASGIScope: TypeAlias = MutableMapping[str, Any]
_ASGIReceive: TypeAlias = Callable[[], Awaitable[MutableMapping[str, Any]]]
_ASGISend: TypeAlias = Callable[[MutableMapping[str, Any]], Awaitable[None]]
_DurableTargetFactory = Callable[
    [TriggerInboxKey, TriggerTarget[EnvelopeEvent]], TriggerTarget[EnvelopeEvent]
]

# One endpoint per client and connection, as the Go client caches them, so every binding shares a URL.
_ENDPOINTS: (
    "weakref.WeakKeyDictionary[Client, dict[ConnectionRef, WebhookEndpoint[Credentials, EnvelopeEvent]]]"
)
_ENDPOINTS = weakref.WeakKeyDictionary()


def new_envelope_event_received_trigger(
    *,
    connection: Connection,
    binding_name: str,
    configuration: EnvelopeEventReceivedTriggerConfiguration,
    target: TriggerTarget[EnvelopeEvent],
    logger: TriggerLogger | None = None,
) -> TriggerRunner:
    """Create one ``envelopeEventReceived`` binding's TriggerRunner on the connection's shared endpoint.

    The runner delivers each verified event whose name configuration's events accept (empty accepts all
    three) to target; a filtered event is still answered 200. Wrap target in a durable inbox, as
    `new_project_envelope_event_received_endpoint_runner` does, so an event is stored before Connect
    receives 200. Serve `envelope_event_received_webhook_endpoint(connection)` or a
    `WebhookEndpointRunner`. logger receives the runner's records; None uses the standard logging adapter.
    Raises ValueError for a mock connection, a blank binding name, a missing target, or an invalid
    configuration.
    """
    if target is None or not (binding_name or "").strip():
        raise ValueError("DocuSign envelopeEventReceived binding name and target are required")
    validate_envelope_event_received_trigger_configuration(configuration)
    events = configuration.events

    def accepts_event(event: TriggerEvent[EnvelopeEvent]) -> bool:
        return not events or event.payload.event in events

    endpoint = envelope_event_received_webhook_endpoint(connection)
    binding = TriggerBindingRef(
        connection.reference, ENVELOPE_EVENT_RECEIVED_TRIGGER_DEFINITION.trigger, binding_name
    )
    return new_trigger(
        TriggerConfig(
            ENVELOPE_EVENT_RECEIVED_TRIGGER_DEFINITION,
            binding,
            endpoint.new_source(accepts_event),
            target,
            logger,
        )
    )


def envelope_event_received_webhook_endpoint(
    connection: Connection,
) -> WebhookEndpoint[Credentials, EnvelopeEvent]:
    """Return the connection's Connect endpoint, creating it on first use, as an ASGI application.

    Mount it at the URL to Publish of the Connect configuration. Every ``envelopeEventReceived`` Trigger
    built from this connection feeds from it. It answers 503 while none of them runs and without a
    ``connect_hmac_key``, so Connect retries; 400 for a bad signature or a body that is not JSON SIM; 413
    above ``connectMaxBodyBytes``; and 200 for another Connect event, unrecorded. Raises ValueError for a
    mock connection.
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
                verify_request=_verify_connect_request,
                decode_event=_decode_connect_request,
                maximum_body_bytes=client.config.connect_max_body_bytes,
                clock=client.now,
            )
        )
        endpoints[connection.reference] = endpoint
    return endpoint


@dataclass(frozen=True, slots=True)
class ProjectEnvelopeEventReceivedTriggerRoute:
    """Bind one ``envelopeEventReceived`` binding saved in the project configuration to its target."""

    binding_name: str
    """The saved binding whose configuration filters the events."""
    target: TriggerTarget[EnvelopeEvent]
    """Receives the binding's events through its durable project inbox, such as a
    `new_dex_rpc_trigger_target` that resumes the Flow waiting for the envelope."""


class EnvelopeEventReceivedEndpointRunner:
    """Serve one connection's Connect endpoint while running its bindings.

    It is an ASGI 3 application: mount it at the Connect configuration's URL to Publish and serve it with
    lifespan enabled, such as ``uvicorn.run(runner)``, so startup replays each binding's inbox and starts
    receiving on the server's event loop. A host that owns the loop calls `run` beside the server instead.
    Deliveries are answered 503 until every binding receives, so Connect retries. It is safe for
    concurrent requests.
    """

    def __init__(self, connection: Connection, runners: Sequence[TriggerRunner]) -> None:
        """Bind runners to the connection's shared endpoint.

        Applications use `new_project_envelope_event_received_endpoint_runner`.
        """
        self._endpoint = envelope_event_received_webhook_endpoint(connection)
        self._endpoint_runner = WebhookEndpointRunner(self._endpoint, runners)

    async def __call__(self, scope: _ASGIScope, receive: _ASGIReceive, send: _ASGISend) -> None:
        """Serve the ASGI ``http`` and ``lifespan`` scopes; see `WebhookEndpointRunner`."""
        await self._endpoint_runner(scope, receive, send)

    async def handle_request(
        self,
        method: str,
        url: str,
        headers: Iterable[tuple[str, str]],
        body: bytes,
        cancellation: TriggerCancellation | None = None,
    ) -> WebhookResponse:
        """Verify, record, and acknowledge one delivery with its complete body, for non-ASGI hosts."""
        return await self._endpoint_runner.handle_request(method, url, headers, body, cancellation)

    async def run(self, context: TriggerContext | None = None) -> None:
        """Replay each binding's inbox, then deliver new events until context is cancelled or one fails.

        Raises TriggerDeliveryAbortedError after cancellation.
        """
        await self._endpoint_runner.run(context)

    def running_source_count(self) -> int:
        """Return how many bindings receive events; a readiness check waits until it equals the routes."""
        return self._endpoint.running_source_count()


def new_project_envelope_event_received_endpoint_runner(
    project: LoadedProject,
    connection: Connection,
    routes: Sequence[ProjectEnvelopeEventReceivedTriggerRoute],
    *,
    logger: TriggerLogger | None = None,
) -> EnvelopeEventReceivedEndpointRunner:
    """Run each route's saved binding of connection with its target behind a durable project inbox.

    An event is in project storage before Connect receives 200, and `run` replays stored events, also
    ones a Go replica recorded, after a restart. connection is usually `new_project_connection(project,
    name)`; the caller keeps ownership and closes it after the runner stops. logger receives the runners'
    and inboxes' records. Raises ValueError for no routes, a blank or duplicated binding name, a missing
    target, or an invalid saved configuration, and ObjectNotFoundError for a binding the project does not
    store.
    """
    if project is None:
        raise ValueError(
            "DocuSign envelopeEventReceived endpoint runner requires loaded project configuration"
        )

    def durable_target(
        key: TriggerInboxKey, target: TriggerTarget[EnvelopeEvent]
    ) -> TriggerTarget[EnvelopeEvent]:
        return new_durable_trigger_target(
            project.trigger_inbox(key), target, decode_payload=_decode_stored_envelope_event, logger=logger
        )

    return _new_envelope_event_received_endpoint_runner(
        connection, project.configuration, routes, durable_target, logger
    )


def _new_envelope_event_received_endpoint_runner(
    connection: Connection,
    configuration: Configuration,
    routes: Sequence[ProjectEnvelopeEventReceivedTriggerRoute],
    make_durable: _DurableTargetFactory,
    logger: TriggerLogger | None,
) -> EnvelopeEventReceivedEndpointRunner:
    if not routes:
        raise ValueError("DocuSign envelopeEventReceived routes are required")
    _connection_client(connection)
    connection_name = connection.reference.name
    runners: list[TriggerRunner] = []
    binding_names: set[str] = set()
    for route in routes:
        binding_name = (route.binding_name or "").strip()
        if not binding_name or route.target is None:
            raise ValueError("DocuSign envelopeEventReceived binding name and target are required")
        if binding_name in binding_names:
            raise ValueError(f'DocuSign envelopeEventReceived binding name "{binding_name}" is duplicated')
        binding_names.add(binding_name)
        key = TriggerInboxKey(CONNECTOR_ID, connection_name, _TRIGGER_NAME, binding_name)
        try:
            binding_configuration = decode_trigger_configuration(
                configuration, key, decode_envelope_event_received_trigger_configuration
            )
        except ObjectNotFoundError:
            raise
        except ValueError as error:
            raise ValueError(
                f'DocuSign envelopeEventReceived binding "{binding_name}" configuration: {error}'
            ) from error
        try:
            validate_envelope_event_received_trigger_configuration(binding_configuration)
        except ValueError as error:
            raise ValueError(f'binding "{binding_name}": {error}') from error
        runners.append(
            new_envelope_event_received_trigger(
                connection=connection,
                binding_name=binding_name,
                configuration=binding_configuration,
                target=make_durable(key, route.target),
                logger=logger,
            )
        )
    return EnvelopeEventReceivedEndpointRunner(connection, runners)


async def _verify_connect_request(
    request: WebhookRequest, credentials: Credentials, cancellation: TriggerCancellation
) -> None:
    # DocuSign shows the key quoted in some exports; Go strips every quote too.
    secret = credentials.connect_hmac_key
    hmac_key = (secret.reveal() if secret is not None else "").replace('"', "")
    if not hmac_key:
        raise WebhookVerificationUnavailableError()
    verify_connect_signature(request.body, request.headers, hmac_key)


def _decode_connect_request(request: WebhookRequest) -> TriggerEvent[EnvelopeEvent] | None:
    return decode_connect_message(request.body, request.received_at)


def _decode_stored_envelope_event(payload_json: str, payload_bytes: bytes) -> EnvelopeEvent:
    """Decode a replayed event, including one a Go replica recorded."""
    decoded = CodecRegistry().resolve(EnvelopeEvent).decode(Value(WireKind.JSON, payload_json))
    if not isinstance(decoded, EnvelopeEvent) or not decoded.envelope_id or not decoded.event:
        raise ValueError("stored DocuSign envelope event is invalid")
    return decoded


def _connection_client(connection: Connection) -> Client:
    connection.reference.validate()
    # The generated Connection keeps its Client private; Triggers need its credentials and limits.
    client = connection._client
    if client is None:
        raise ValueError(
            "DocuSign Triggers require a connection built with new_connection, not a mock connection"
        )
    return client


__all__ = [
    "CONNECT_ENVELOPE_EVENTS",
    "CONNECT_EVENT_ENVELOPE_COMPLETED",
    "CONNECT_EVENT_ENVELOPE_DECLINED",
    "CONNECT_EVENT_ENVELOPE_VOIDED",
    "ENVELOPE_EVENT_RECEIVED_TRIGGER_DEFINITION",
    "ConnectMessageMalformedError",
    "ConnectSignatureInvalidError",
    "EnvelopeEvent",
    "EnvelopeEventReceivedEndpointRunner",
    "EnvelopeEventReceivedTriggerConfiguration",
    "ProjectEnvelopeEventReceivedTriggerRoute",
    "decode_connect_message",
    "decode_envelope_event_received_trigger_configuration",
    "define_envelope_event_received_trigger_binding",
    "envelope_event_custom_field_value",
    "envelope_event_received_webhook_endpoint",
    "new_envelope_event_received_trigger",
    "new_project_envelope_event_received_endpoint_runner",
    "validate_envelope_event_received_trigger_configuration",
    "verify_connect_signature",
]
