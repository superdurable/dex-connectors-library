# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify signed Stripe Checkout events and deliver them through durable Trigger bindings."""

from __future__ import annotations

import hashlib
import hmac
import json
import re
import weakref
from collections.abc import Awaitable, Callable, Iterable, MutableMapping, Sequence
from dataclasses import dataclass
from datetime import UTC, datetime
from decimal import Decimal, InvalidOperation
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

from ._generated.definitions import (
    CHECKOUT_SESSION_UPDATED_TRIGGER_DEFINITION as CHECKOUT_SESSION_UPDATED_TRIGGER_DEFINITION,
)
from ._generated.definitions import CONNECTOR_ID
from ._generated.models import CheckoutSession as GeneratedCheckoutSession
from ._generated.triggers import (
    decode_checkout_session_updated_trigger_configuration,
)
from ._generated.triggers import (
    define_checkout_session_updated_trigger_binding as define_checkout_session_updated_trigger_binding,
)
from .client import Client, decode_checkout_session
from .connection import Connection
from .models import CheckoutSessionEvent, CheckoutSessionUpdatedTriggerConfiguration, Credentials

CHECKOUT_SESSION_COMPLETED: Final = "checkout.session.completed"
CHECKOUT_SESSION_ASYNC_PAYMENT_SUCCEEDED: Final = "checkout.session.async_payment_succeeded"
CHECKOUT_SESSION_ASYNC_PAYMENT_FAILED: Final = "checkout.session.async_payment_failed"
CHECKOUT_SESSION_EXPIRED: Final = "checkout.session.expired"
_SUPPORTED_EVENT_TYPES: Final = (
    CHECKOUT_SESSION_COMPLETED,
    CHECKOUT_SESSION_ASYNC_PAYMENT_SUCCEEDED,
    CHECKOUT_SESSION_ASYNC_PAYMENT_FAILED,
    CHECKOUT_SESSION_EXPIRED,
)
_TRIGGER_NAME: Final = CHECKOUT_SESSION_UPDATED_TRIGGER_DEFINITION.trigger.trigger_name
_DURATION_PART: Final = re.compile(r"(\d+(?:\.\d+)?)(ns|us|µs|μs|ms|s|m|h)")
_DURATION_MULTIPLIER: Final = {
    "ns": 1,
    "us": 1_000,
    "µs": 1_000,
    "μs": 1_000,
    "ms": 1_000_000,
    "s": 1_000_000_000,
    "m": 60_000_000_000,
    "h": 3_600_000_000_000,
}
_ASGIScope: TypeAlias = MutableMapping[str, Any]
_ASGIReceive: TypeAlias = Callable[[], Awaitable[MutableMapping[str, Any]]]
_ASGISend: TypeAlias = Callable[[MutableMapping[str, Any]], Awaitable[None]]
_ENDPOINTS: weakref.WeakKeyDictionary[
    Client, dict[ConnectionRef, WebhookEndpoint[Credentials, CheckoutSessionEvent]]
] = weakref.WeakKeyDictionary()


def validate_checkout_session_updated_trigger_configuration(
    configuration: CheckoutSessionUpdatedTriggerConfiguration,
) -> None:
    """Require each selected Checkout event type to be supported and listed once."""
    if len(set(configuration.event_types)) != len(configuration.event_types):
        raise ValueError("Stripe Checkout Session event types must be unique")
    for event_type in configuration.event_types:
        if event_type not in _SUPPORTED_EVENT_TYPES:
            raise ValueError(f"Stripe Checkout Session event type {event_type!r} is not supported")


def _duration_seconds(value: str | int) -> float:
    if isinstance(value, int) and not isinstance(value, bool):
        nanoseconds = value
    elif isinstance(value, str):
        remaining = value
        nanoseconds_decimal = Decimal(0)
        while remaining:
            part = _DURATION_PART.match(remaining)
            if part is None:
                raise ValueError("Stripe webhookSignatureTolerance must be a positive Go duration")
            try:
                nanoseconds_decimal += Decimal(part.group(1)) * _DURATION_MULTIPLIER[part.group(2)]
            except InvalidOperation as error:
                raise ValueError("Stripe webhookSignatureTolerance is invalid") from error
            remaining = remaining[part.end() :]
        nanoseconds = int(nanoseconds_decimal)
    else:
        raise ValueError("Stripe webhookSignatureTolerance must be a positive Go duration")
    if nanoseconds <= 0 or nanoseconds > 2**63 - 1:
        raise ValueError("Stripe webhookSignatureTolerance must be a positive Go duration")
    return nanoseconds / 1_000_000_000


def _connection_client(connection: Connection) -> Client:
    client = connection._client
    if not isinstance(client, Client):
        raise ValueError("Stripe checkoutSessionUpdated requires a provider-backed Stripe connection")
    return client


async def _verify_request(
    request: WebhookRequest,
    credentials: Credentials,
    cancellation: TriggerCancellation,
    *,
    tolerance_seconds: float,
) -> None:
    secret = credentials.webhook_secret.reveal() if credentials.webhook_secret is not None else ""
    if not secret:
        raise WebhookVerificationUnavailableError()
    timestamp = 0
    signatures: list[bytes] = []
    for component in (request.header("Stripe-Signature") or "").split(","):
        key, separator, value = component.strip().partition("=")
        if not separator:
            continue
        if key == "t":
            try:
                if re.fullmatch(r"[+-]?[0-9]+", value) is None:
                    raise ValueError
                timestamp = int(value)
            except ValueError as error:
                raise ValueError("Stripe signature timestamp is invalid") from error
            if timestamp < -(2**63) or timestamp > 2**63 - 1:
                raise ValueError("Stripe signature timestamp is invalid")
        elif key == "v1" and re.fullmatch(r"[0-9a-fA-F]+", value) and len(value) % 2 == 0:
            signatures.append(bytes.fromhex(value))
    if timestamp == 0 or not signatures:
        raise ValueError("Stripe signature is incomplete")
    try:
        sent_at = datetime.fromtimestamp(timestamp, tz=UTC)
    except (OverflowError, OSError, ValueError) as error:
        raise ValueError("Stripe signature timestamp is invalid") from error
    if abs((request.received_at - sent_at).total_seconds()) > tolerance_seconds:
        raise ValueError("Stripe signature timestamp is outside the tolerance")
    expected = hmac.new(
        secret.encode(), str(timestamp).encode() + b"." + request.body, hashlib.sha256
    ).digest()
    if not any(hmac.compare_digest(signature, expected) for signature in signatures):
        raise ValueError("Stripe signature does not match")


def _reject_json_constant(value: str) -> NoReturn:
    raise ValueError("Stripe webhook JSON constant is invalid")


def _decode_event(request: WebhookRequest) -> TriggerEvent[CheckoutSessionEvent] | None:
    try:
        envelope = json.loads(request.body, parse_constant=_reject_json_constant)
        if not isinstance(envelope, dict):
            raise ValueError
        event_id = envelope.get("id") or ""
        object_type = envelope.get("object") or ""
        event_type = envelope.get("type") or ""
        created = envelope.get("created") or 0
        if (
            not isinstance(event_id, str)
            or not event_id
            or object_type != "event"
            or not isinstance(event_type, str)
            or not event_type
            or type(created) is not int
            or created <= 0
        ):
            raise ValueError
        occurred_at = datetime.fromtimestamp(created, tz=UTC)
        if event_type not in _SUPPORTED_EVENT_TYPES:
            return None
        data = envelope.get("data")
        if not isinstance(data, dict):
            raise ValueError
        raw_session = data.get("object")
        session = decode_checkout_session(json.dumps(raw_session).encode())
        trigger_session = GeneratedCheckoutSession(
            id=session.id,
            url=session.url,
            client_reference_id=session.client_reference_id,
            payment_status=session.payment_status,
            status=session.status,
            payment_intent_id=session.payment_intent_id,
            currency=session.currency,
            amount_total=session.amount_total,
            expires_at=session.expires_at or datetime.min.replace(tzinfo=UTC),
            metadata=session.metadata,
        )
        return TriggerEvent(event_id, occurred_at, CheckoutSessionEvent(event_type, trigger_session))
    except (TypeError, ValueError, OverflowError, OSError, UnicodeDecodeError) as error:
        raise ValueError("Stripe webhook event is malformed") from error


def _decode_stored_payload(payload_json: str, payload_bytes: bytes) -> CheckoutSessionEvent:
    decoded = CodecRegistry().resolve(CheckoutSessionEvent).decode(Value(WireKind.JSON, payload_json))
    if not isinstance(decoded, CheckoutSessionEvent):
        raise ValueError("pending Stripe checkoutSessionUpdated payload is invalid")
    return decoded


def checkout_session_webhook_endpoint(
    connection: Connection,
) -> WebhookEndpoint[Credentials, CheckoutSessionEvent]:
    """Return one shared ASGI endpoint for a Stripe provider connection."""
    client = _connection_client(connection)
    endpoints = _ENDPOINTS.setdefault(client, {})
    endpoint = endpoints.get(connection.reference)
    if endpoint is None:
        maximum_bytes = client.config.webhook_max_body_bytes or 1_048_576
        if maximum_bytes <= 0:
            raise ValueError("Stripe webhookMaxBodyBytes must be positive")
        tolerance_seconds = _duration_seconds(client.config.webhook_signature_tolerance)
        endpoint = WebhookEndpoint(
            WebhookEndpointConfig(
                connector_id=CONNECTOR_ID,
                trigger_name=_TRIGGER_NAME,
                connection=connection.reference,
                credentials=client._credentials,
                verify_request=partial(_verify_request, tolerance_seconds=tolerance_seconds),
                decode_event=_decode_event,
                maximum_body_bytes=maximum_bytes,
                clock=client._now,
            )
        )
        endpoints[connection.reference] = endpoint
    return endpoint


def new_checkout_session_updated_trigger(
    *,
    connection: Connection,
    binding_name: str,
    target: TriggerTarget[CheckoutSessionEvent],
    configuration: CheckoutSessionUpdatedTriggerConfiguration | None = None,
    logger: TriggerLogger | None = None,
) -> TriggerRunner:
    """Run one filtered Checkout Session binding; use the project runner for durable delivery."""
    if not binding_name.strip() or target is None:
        raise ValueError("Stripe checkoutSessionUpdated binding name and target are required")
    selected = configuration or CheckoutSessionUpdatedTriggerConfiguration()
    validate_checkout_session_updated_trigger_configuration(selected)
    endpoint = checkout_session_webhook_endpoint(connection)
    return new_trigger(
        TriggerConfig(
            definition=CHECKOUT_SESSION_UPDATED_TRIGGER_DEFINITION,
            binding=TriggerBindingRef(
                connection.reference, CHECKOUT_SESSION_UPDATED_TRIGGER_DEFINITION.trigger, binding_name
            ),
            source=endpoint.new_source(
                lambda event: not selected.event_types or event.payload.type in selected.event_types
            ),
            target=target,
            logger=logger,
        )
    )


@dataclass(frozen=True, slots=True)
class ProjectCheckoutSessionUpdatedTriggerRoute:
    """One stored Checkout Session binding and its application target."""

    binding_name: str
    """Application binding name in Dex project configuration."""
    target: TriggerTarget[CheckoutSessionEvent]
    """Target receiving verified Checkout Session events after durable inbox delivery."""


class CheckoutSessionWebhookRuntime:
    """Serve the shared Stripe endpoint and replay its durable binding inboxes."""

    def __init__(self, connection: Connection, runners: Sequence[TriggerRunner]) -> None:
        """Bind one endpoint and at least one Trigger runner."""
        if not runners:
            raise ValueError("Stripe Checkout Session trigger routes are required")
        self._endpoint = checkout_session_webhook_endpoint(connection)
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
        """Verify and persist one complete webhook before acknowledgment."""
        return await self._runner.handle_request(method, url, headers, body, cancellation)

    async def run(self, context: TriggerContext | None = None) -> None:
        """Replay pending events and receive new webhooks until cancelled."""
        await self._runner.run(context)

    def running_source_count(self) -> int:
        """Return the number of active binding sources."""
        return self._endpoint.running_source_count()


def new_project_checkout_session_webhook_runtime(
    project: LoadedProject,
    connection: Connection,
    routes: Sequence[ProjectCheckoutSessionUpdatedTriggerRoute],
    logger: TriggerLogger | None = None,
) -> CheckoutSessionWebhookRuntime:
    """Read binding filters and persist signed events before answering Stripe 200."""
    if project is None or not routes:
        raise ValueError("Stripe Checkout Session project and routes are required")
    _connection_client(connection)
    runners: list[TriggerRunner] = []
    names: set[str] = set()
    for route in routes:
        name = route.binding_name.strip()
        if not name or route.target is None or name in names:
            raise ValueError("Stripe Checkout Session binding names and targets must be unique")
        names.add(name)
        key = TriggerInboxKey(CONNECTOR_ID, connection.reference.name, _TRIGGER_NAME, name)
        try:
            selected = decode_trigger_configuration(
                project.configuration, key, decode_checkout_session_updated_trigger_configuration
            )
            validate_checkout_session_updated_trigger_configuration(selected)
        except ValueError as error:
            raise ValueError(
                f"Stripe checkoutSessionUpdated binding {name!r} configuration: {error}"
            ) from error
        target = new_durable_trigger_target(
            project.trigger_inbox(key), route.target, decode_payload=_decode_stored_payload, logger=logger
        )
        runners.append(
            new_checkout_session_updated_trigger(
                connection=connection, binding_name=name, configuration=selected, target=target, logger=logger
            )
        )
    return CheckoutSessionWebhookRuntime(connection, runners)
