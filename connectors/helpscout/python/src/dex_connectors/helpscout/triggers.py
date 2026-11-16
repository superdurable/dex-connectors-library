# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Receive signed Help Scout conversation webhooks through the ``conversationEvent`` Trigger.

Each connection has one shared webhook endpoint that verifies ``X-HelpScout-Signature``,
the base64 HMAC-SHA1 of the raw body keyed with the connection's ``webhook_secret``, and
decodes the ten conversation events named by ``X-HelpScout-Event``. Every binding of the
Trigger runs one source of that endpoint and keeps the events its configuration selects.
`new_project_conversation_event_endpoint_runner` wires stored bindings to durable
project inboxes, so an event is recorded before Help Scout receives 200.
"""

import base64
import hashlib
import hmac
import json
import re
from collections.abc import Callable, Iterable, Sequence
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any, Final

import dex
import httpx
from dex_connectors.sdk import (
    ConnectionRef,
    CredentialProvider,
    CredentialRefreshDriver,
    CredentialRefreshResult,
    CredentialRefreshState,
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

from . import decode
from ._generated.definitions import CONNECTOR_ID, CONVERSATION_EVENT_TRIGGER_DEFINITION
from ._generated.triggers import (
    decode_conversation_event_trigger_configuration,
    define_conversation_event_trigger_binding,
)
from .client import Client, Credentials
from .connection import Connection, new_project_connection
from .json_values import strict_json_loads
from .models import ConversationEvent, ConversationEventTriggerConfiguration

WEBHOOK_EVENT_CONVERSATION_CREATED: Final = "convo.created"
"""Sent when a conversation is created."""
WEBHOOK_EVENT_CONVERSATION_ASSIGNED: Final = "convo.assigned"
"""Sent when a conversation's assignee changes."""
WEBHOOK_EVENT_CONVERSATION_STATUS_UPDATED: Final = "convo.status"
"""Sent when a conversation's status changes."""
WEBHOOK_EVENT_CONVERSATION_TAGS_UPDATED: Final = "convo.tags"
"""Sent when a conversation's tags change."""
WEBHOOK_EVENT_CONVERSATION_CUSTOM_FIELDS_UPDATED: Final = "convo.custom-fields"
"""Sent when a conversation's custom fields change."""
WEBHOOK_EVENT_CONVERSATION_MOVED: Final = "convo.moved"
"""Sent when a conversation moves to another inbox."""
WEBHOOK_EVENT_CONVERSATION_MERGED: Final = "convo.merged"
"""Sent when a conversation is merged into another."""
WEBHOOK_EVENT_CUSTOMER_REPLY_CREATED: Final = "convo.customer.reply.created"
"""Sent when a customer replies."""
WEBHOOK_EVENT_AGENT_REPLY_CREATED: Final = "convo.agent.reply.created"
"""Sent when a user replies; Help Scout omits auto replies."""
WEBHOOK_EVENT_NOTE_CREATED: Final = "convo.note.created"
"""Sent when a user adds an internal note."""

_CONVERSATION_WEBHOOK_EVENTS: Final = (
    WEBHOOK_EVENT_CONVERSATION_CREATED,
    WEBHOOK_EVENT_CONVERSATION_ASSIGNED,
    WEBHOOK_EVENT_CONVERSATION_STATUS_UPDATED,
    WEBHOOK_EVENT_CONVERSATION_TAGS_UPDATED,
    WEBHOOK_EVENT_CONVERSATION_CUSTOM_FIELDS_UPDATED,
    WEBHOOK_EVENT_CONVERSATION_MOVED,
    WEBHOOK_EVENT_CONVERSATION_MERGED,
    WEBHOOK_EVENT_CUSTOMER_REPLY_CREATED,
    WEBHOOK_EVENT_AGENT_REPLY_CREATED,
    WEBHOOK_EVENT_NOTE_CREATED,
)
_TRIGGER_NAME: Final = CONVERSATION_EVENT_TRIGGER_DEFINITION.trigger.trigger_name
_SIGNATURE_HEADER: Final = "X-HelpScout-Signature"
_EVENT_HEADER: Final = "X-HelpScout-Event"
_WEBHOOK_EVENT_NAME: Final = re.compile(r"[a-z][a-z0-9.-]{0,63}", re.ASCII)
# Padded standard base64, as Go's base64.StdEncoding accepts it once line breaks are removed.
_STANDARD_BASE64: Final = re.compile(
    r"(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?", re.ASCII
)
_SHA1_BYTES: Final = 20
# Go's unicode.IsSpace set; str.strip also trims U+001C-U+001F.
_GO_SPACE: Final = "\t\n\v\f\r \x85\xa0                　"
_SIGNATURE_INVALID: Final = "X-HelpScout-Signature is missing, malformed, or does not match"


def conversation_webhook_events() -> tuple[str, ...]:
    """Return the Help Scout webhook events whose body is a conversation, which the Trigger decodes.

    Help Scout documents each with a v2 Conversation object body.
    """
    return _CONVERSATION_WEBHOOK_EVENTS


def validate_conversation_event_trigger_configuration(
    configuration: ConversationEventTriggerConfiguration,
) -> None:
    """Check that ``events`` holds distinct conversation webhook events and ``mailbox_id`` is not negative.

    Empty ``events`` accepts every conversation event, and a zero ``mailbox_id`` every inbox.
    Raises ValueError naming the first problem.
    """
    seen: set[str] = set()
    for event in configuration.events:
        if event not in _CONVERSATION_WEBHOOK_EVENTS:
            raise ValueError(
                f"conversationEvent event {json.dumps(event)} is not a Help Scout conversation webhook event"
            )
        if event in seen:
            raise ValueError(f"conversationEvent events lists {json.dumps(event)} twice")
        seen.add(event)
    if configuration.mailbox_id < 0:
        raise ValueError(
            "conversationEvent mailboxId must be a positive Help Scout inbox ID, or zero for every inbox"
        )


def _accepts_conversation_event(
    configuration: ConversationEventTriggerConfiguration, event: TriggerEvent[ConversationEvent]
) -> bool:
    if configuration.events and event.payload.event not in configuration.events:
        return False
    return configuration.mailbox_id in (0, event.payload.conversation.mailbox_id)


def _trim_go_space(value: str) -> str:
    return value.strip(_GO_SPACE)


def _verify_help_scout_signature(body: bytes, header: str, secret: str) -> None:
    """Compare base64(HMAC-SHA1(body, secret)) in constant time; Help Scout signs no timestamp."""
    encoded = _trim_go_space(header).replace("\r", "").replace("\n", "")
    if _STANDARD_BASE64.fullmatch(encoded) is None:
        raise ValueError(_SIGNATURE_INVALID)
    signature = base64.b64decode(encoded)
    expected = hmac.new(secret.encode(), body, hashlib.sha1).digest()
    if len(signature) != _SHA1_BYTES or not hmac.compare_digest(signature, expected):
        raise ValueError(_SIGNATURE_INVALID)


async def _verify_conversation_webhook_request(
    request: WebhookRequest, credentials: Credentials, cancellation: TriggerCancellation
) -> None:
    """Answer 503 without a webhook secret, so Help Scout keeps retrying."""
    secret = credentials.webhook_secret.reveal() if credentials.webhook_secret is not None else ""
    if not secret:
        raise WebhookVerificationUnavailableError()
    _verify_help_scout_signature(request.body, request.header(_SIGNATURE_HEADER) or "", secret)


def _decode_conversation_webhook_request(request: WebhookRequest) -> TriggerEvent[ConversationEvent] | None:
    """Acknowledge, without a record, valid events outside the conversation events."""
    event = _trim_go_space(request.header(_EVENT_HEADER) or "")
    if _WEBHOOK_EVENT_NAME.fullmatch(event) is None:
        raise ValueError("X-HelpScout-Event is missing or malformed")
    if event not in _CONVERSATION_WEBHOOK_EVENTS:
        return None
    if not request.body:
        raise ValueError("Help Scout returned a malformed response")
    try:
        # Go's encoding/json replaces invalid UTF-8 inside strings with U+FFFD.
        wire = strict_json_loads(request.body.decode("utf-8", "replace"))
    except ValueError as error:
        raise ValueError("Help Scout returned a malformed response") from error
    conversation = decode.conversation(wire)
    body_digest = hashlib.sha256(request.body).hexdigest()[:32]
    return TriggerEvent(
        f"{event}:{conversation.id}:{body_digest}",
        request.received_at.astimezone(UTC),
        ConversationEvent(event, conversation=conversation),
    )


def _decode_conversation_event_payload(payload_json: str, payload_bytes: bytes) -> ConversationEvent:
    """Decode a replayed inbox payload that the Go, TypeScript, or Python connector wrote."""
    decoded = (
        dex.CodecRegistry().resolve(ConversationEvent).decode(dex.Value(dex.WireKind.JSON, payload_json))
    )
    if not isinstance(decoded, ConversationEvent):
        raise ValueError("pending Help Scout conversation event is invalid")
    return decoded


class _ExpiredRecordRefreshDriver:
    """Renews only a record project storage refuses as expired; verification needs only the secret."""

    def __init__(self, token_driver: CredentialRefreshDriver[Credentials]) -> None:
        self._token_driver = token_driver

    def refresh_required(self, state: CredentialRefreshState[Credentials]) -> bool:
        return state.expires_at is not None and state.now >= state.expires_at

    async def refresh(
        self, state: CredentialRefreshState[Credentials]
    ) -> CredentialRefreshResult[Credentials]:
        return await self._token_driver.refresh(state)


def _webhook_credentials(
    client: Client,
) -> tuple[CredentialProvider[Credentials], CredentialRefreshDriver[Credentials] | None]:
    """Return the credential source and refresh driver for webhook verification.

    A project connection's source pairs a refreshing provider with the manifest token driver, which
    renews five minutes early; webhook verification renews only an expired record, as Go does.
    """
    provider = getattr(client.credentials, "provider", None)
    token_driver = getattr(client.credentials, "driver", None)
    if provider is None or token_driver is None:
        return client.credentials, None
    return provider, _ExpiredRecordRefreshDriver(token_driver)


def _new_conversation_event_webhook_endpoint(
    client: Client,
    connection: ConnectionRef,
    now: Callable[[], datetime],
    logger: TriggerLogger | None,
) -> WebhookEndpoint[Credentials, ConversationEvent]:
    connection.validate()
    credentials, refresh_driver = _webhook_credentials(client)
    try:
        return WebhookEndpoint(
            WebhookEndpointConfig(
                connector_id=CONNECTOR_ID,
                trigger_name=_TRIGGER_NAME,
                connection=connection,
                credentials=credentials,
                verify_request=_verify_conversation_webhook_request,
                decode_event=_decode_conversation_webhook_request,
                maximum_body_bytes=client.config.webhook_max_body_bytes,
                credential_refresh=refresh_driver,
                clock=now,
                logger=logger,
            )
        )
    except ValueError as error:
        raise ValueError(f"Help Scout webhook endpoint: {error}") from error


def _help_scout_client_of(connection: Connection) -> Client:
    client = getattr(connection, "_client", None)
    if not isinstance(client, Client):
        raise ValueError("Help Scout Triggers require a connection bound to a Help Scout Client")
    return client


def conversation_event_webhook_endpoint(
    connection: Connection,
) -> WebhookEndpoint[Credentials, ConversationEvent]:
    """Return the connection's shared Help Scout webhook endpoint, creating it on first use.

    The endpoint is an ASGI application to mount at the public HTTPS URL of the Help Scout
    webhook; `WebhookEndpoint.handle_request` serves hosts that are not ASGI servers. Every
    ``conversationEvent`` Trigger built from this connection feeds from it, and it answers 503
    while none of them runs, so Help Scout retries. A body over ``webhookMaxBodyBytes`` answers
    413. Raises ValueError for a connection without a Help Scout Client, such as a mock.
    """
    return _help_scout_client_of(connection).conversation_event_webhook_endpoint(connection.reference)


def new_conversation_event_trigger(
    *,
    connection: Connection,
    binding_name: str,
    target: TriggerTarget[ConversationEvent],
    configuration: ConversationEventTriggerConfiguration | None = None,
    logger: TriggerLogger | None = None,
) -> TriggerRunner:
    """Create one ``conversationEvent`` binding's runner on the connection's shared endpoint.

    The runner receives events only while `run` is active, so serve
    `conversation_event_webhook_endpoint` of the same connection. Events the configuration
    does not select are acknowledged and dropped; None records every conversation event of
    every inbox. Wrap target with `new_durable_trigger_target` so an event is recorded before
    Help Scout is acknowledged, as `new_project_conversation_event_endpoint_runner` does.
    Raises ValueError for an invalid configuration, a blank binding name, or a connection
    without a Help Scout Client.
    """
    selected = configuration if configuration is not None else ConversationEventTriggerConfiguration()
    validate_conversation_event_trigger_configuration(selected)
    endpoint = conversation_event_webhook_endpoint(connection)

    def accepts_event(event: TriggerEvent[ConversationEvent]) -> bool:
        return _accepts_conversation_event(selected, event)

    return new_trigger(
        TriggerConfig(
            definition=CONVERSATION_EVENT_TRIGGER_DEFINITION,
            binding=TriggerBindingRef(
                connection.reference, CONVERSATION_EVENT_TRIGGER_DEFINITION.trigger, binding_name
            ),
            source=endpoint.new_source(accepts_event),
            target=target,
            logger=logger,
        )
    )


@dataclass(frozen=True, slots=True)
class ProjectConversationEventTriggerRoute:
    """Bind one stored ``conversationEvent`` binding to its application target."""

    binding_name: str
    """The stored Trigger binding name whose configuration filters the events."""
    target: TriggerTarget[ConversationEvent]
    """Receives the binding's events through its durable project inbox."""


_DurableConversationEventTargetFactory = Callable[
    [TriggerInboxKey, TriggerTarget[ConversationEvent]], TriggerTarget[ConversationEvent]
]


class ConversationEventEndpointRunner:
    """Serve one connection's Help Scout webhook endpoint while running its bindings.

    It is an ASGI 3 application: mount it at the URL of the Help Scout webhook and serve it with
    an ASGI server such as uvicorn, whose ``lifespan`` startup replays each binding's durable
    inbox and then receives new events, and whose shutdown stops them. Until a binding runs,
    signed deliveries answer 503 so Help Scout retries. It is safe for concurrent requests.
    Close it with `aclose` after the server stops; it closes a connection it opened.
    """

    def __init__(
        self,
        endpoint_runner: WebhookEndpointRunner,
        endpoint: WebhookEndpoint[Credentials, ConversationEvent],
        connection: Connection | None = None,
    ) -> None:
        """Wrap an endpoint runner; applications use `new_project_conversation_event_endpoint_runner`."""
        if endpoint_runner is None or endpoint is None:
            raise ValueError("Help Scout conversationEvent endpoint runner and endpoint are required")
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

        Use it instead of the ASGI ``lifespan`` when the host owns the event loop. Raises
        TriggerDeliveryAbortedError after cancellation, otherwise the first binding's failure.
        """
        await self._endpoint_runner.run(context)

    def running_source_count(self) -> int:
        """Return how many bindings are receiving events; until it is positive, deliveries answer 503."""
        return self._endpoint.running_source_count()

    async def aclose(self) -> None:
        """Close the connection the runner opened, after the bindings stop."""
        if self._owned_connection is not None:
            await self._owned_connection.aclose()


async def new_project_conversation_event_endpoint_runner(
    project: LoadedProject,
    connection_name: str,
    routes: Sequence[ProjectConversationEventTriggerRoute],
    http_client: httpx.AsyncClient | None = None,
    logger: TriggerLogger | None = None,
) -> ConversationEventEndpointRunner:
    """Open the project connection and its stored bindings behind one shared webhook endpoint.

    Each route's target receives events through its binding's durable project inbox, which
    records an event before Help Scout receives 200 and replays it after a restart. logger
    receives the endpoint's, bindings', and inboxes' records; None uses standard logging. Raises ValueError
    for empty routes, a blank or repeated binding name, or a missing or invalid stored binding
    configuration. Close the runner with `aclose`.
    """
    if project is None:
        raise ValueError("Help Scout conversationEvent runner requires the loaded project configuration")
    connection = new_project_connection(project, connection_name, http_client)
    # The endpoint's delivery records go to the same logger as the inboxes', as Go's WithLogger does.
    _help_scout_client_of(connection)._trigger_logger = logger

    def make_durable(
        key: TriggerInboxKey, target: TriggerTarget[ConversationEvent]
    ) -> TriggerTarget[ConversationEvent]:
        return new_durable_trigger_target(
            project.trigger_inbox(key),
            target,
            decode_payload=_decode_conversation_event_payload,
            logger=logger,
        )

    try:
        return _new_conversation_event_endpoint_runner(
            connection, project.configuration, routes, make_durable, logger, owns_connection=True
        )
    except BaseException:
        await connection.aclose()
        raise


def _new_conversation_event_endpoint_runner(
    connection: Connection,
    configuration: Configuration,
    routes: Sequence[ProjectConversationEventTriggerRoute],
    make_durable: _DurableConversationEventTargetFactory,
    logger: TriggerLogger | None = None,
    *,
    owns_connection: bool = False,
) -> ConversationEventEndpointRunner:
    if not routes:
        raise ValueError("Help Scout conversationEvent routes are required")
    endpoint = conversation_event_webhook_endpoint(connection)
    connection_name = connection.reference.name
    runners: list[TriggerRunner] = []
    binding_names: set[str] = set()
    for route in routes:
        binding_name = _trim_go_space(route.binding_name or "")
        if not binding_name or route.target is None:
            raise ValueError("Help Scout conversationEvent binding name and target are required")
        if binding_name in binding_names:
            raise ValueError(
                f"Help Scout conversationEvent binding name {json.dumps(binding_name)} is duplicated"
            )
        binding_names.add(binding_name)
        key = TriggerInboxKey(CONNECTOR_ID, connection_name, _TRIGGER_NAME, binding_name)
        try:
            binding_configuration = decode_trigger_configuration(
                configuration, key, decode_conversation_event_trigger_configuration
            )
        except (ValueError, LookupError) as error:
            raise ValueError(
                f"Help Scout conversationEvent binding {json.dumps(binding_name)} configuration: {error}"
            ) from error
        try:
            validate_conversation_event_trigger_configuration(binding_configuration)
        except ValueError as error:
            raise ValueError(f"binding {json.dumps(binding_name)}: {error}") from error
        runners.append(
            new_conversation_event_trigger(
                connection=connection,
                binding_name=binding_name,
                configuration=binding_configuration,
                target=make_durable(key, route.target),
                logger=logger,
            )
        )
    return ConversationEventEndpointRunner(
        WebhookEndpointRunner(endpoint, runners), endpoint, connection if owns_connection else None
    )


__all__ = [
    "CONVERSATION_EVENT_TRIGGER_DEFINITION",
    "WEBHOOK_EVENT_AGENT_REPLY_CREATED",
    "WEBHOOK_EVENT_CONVERSATION_ASSIGNED",
    "WEBHOOK_EVENT_CONVERSATION_CREATED",
    "WEBHOOK_EVENT_CONVERSATION_CUSTOM_FIELDS_UPDATED",
    "WEBHOOK_EVENT_CONVERSATION_MERGED",
    "WEBHOOK_EVENT_CONVERSATION_MOVED",
    "WEBHOOK_EVENT_CONVERSATION_STATUS_UPDATED",
    "WEBHOOK_EVENT_CONVERSATION_TAGS_UPDATED",
    "WEBHOOK_EVENT_CUSTOMER_REPLY_CREATED",
    "WEBHOOK_EVENT_NOTE_CREATED",
    "ConversationEvent",
    "ConversationEventEndpointRunner",
    "ConversationEventTriggerConfiguration",
    "ProjectConversationEventTriggerRoute",
    "conversation_event_webhook_endpoint",
    "conversation_webhook_events",
    "decode_conversation_event_trigger_configuration",
    "define_conversation_event_trigger_binding",
    "new_conversation_event_trigger",
    "new_project_conversation_event_endpoint_runner",
    "validate_conversation_event_trigger_configuration",
]
