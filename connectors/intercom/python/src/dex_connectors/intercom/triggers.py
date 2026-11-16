# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Receive signed Intercom conversation notifications through the conversationEvent Trigger.

Each connection has one shared webhook endpoint. It answers the HEAD request Intercom sends to validate
a webhook URL, verifies ``X-Hub-Signature`` as the hex HMAC-SHA1 of the body under the connection's
``client_secret``, and decodes the conversation topics `conversation_event_topics` lists. Every binding
of the Trigger runs one source of that endpoint and filters by its stored topics.
`new_project_conversation_event_endpoint_runner` wraps each binding's target in its durable project
inbox, so a notification is recorded before Intercom receives 200 and replays after a restart.
"""

import hashlib
import hmac
import json
import re
from collections.abc import Callable, Iterable, Sequence
from dataclasses import dataclass
from datetime import UTC
from typing import Any, Final

import dex
import httpx
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

from ._generated.definitions import CONNECTOR_ID, CONVERSATION_EVENT_TRIGGER_DEFINITION
from ._generated.triggers import (
    decode_conversation_event_trigger_configuration,
    define_conversation_event_trigger_binding,
)
from .client import Client, Credentials
from .connection import Connection, new_project_connection
from .models import ConversationEvent, ConversationEventTriggerConfiguration
from .operations import _ZERO_TIME, _conversation, _object, _time

TOPIC_CONVERSATION_USER_CREATED: Final = "conversation.user.created"
"""A conversation a user, lead, or visitor started."""
TOPIC_CONVERSATION_USER_REPLIED: Final = "conversation.user.replied"
"""A reply from the customer."""
TOPIC_CONVERSATION_ADMIN_REPLIED: Final = "conversation.admin.replied"
"""A reply from a teammate."""
TOPIC_CONVERSATION_ADMIN_SINGLE_CREATED: Final = "conversation.admin.single.created"
"""A one-to-one conversation a teammate started."""
TOPIC_CONVERSATION_ADMIN_ASSIGNED: Final = "conversation.admin.assigned"
"""An assignment by a teammate."""
TOPIC_CONVERSATION_ADMIN_OPEN_ASSIGNED: Final = "conversation.admin.open.assigned"
"""An assignment of an open conversation."""
TOPIC_CONVERSATION_ADMIN_NOTED: Final = "conversation.admin.noted"
"""An internal note from a teammate."""
TOPIC_CONVERSATION_ADMIN_CLOSED: Final = "conversation.admin.closed"
"""A conversation a teammate closed."""
TOPIC_CONVERSATION_ADMIN_OPENED: Final = "conversation.admin.opened"
"""A conversation a teammate reopened."""
TOPIC_CONVERSATION_ADMIN_SNOOZED: Final = "conversation.admin.snoozed"
"""A conversation a teammate snoozed."""
TOPIC_CONVERSATION_ADMIN_UNSNOOZED: Final = "conversation.admin.unsnoozed"
"""A snoozed conversation that reopened."""
TOPIC_CONVERSATION_OPERATOR_REPLIED: Final = "conversation.operator.replied"
"""A reply from Fin or another bot."""
TOPIC_CONVERSATION_PRIORITY_UPDATED: Final = "conversation.priority.updated"
"""A priority change."""
TOPIC_CONVERSATION_RATING_ADDED: Final = "conversation.rating.added"
"""A customer's conversation rating."""

_SUPPORTED_TOPICS: Final = (
    TOPIC_CONVERSATION_USER_CREATED,
    TOPIC_CONVERSATION_USER_REPLIED,
    TOPIC_CONVERSATION_ADMIN_REPLIED,
    TOPIC_CONVERSATION_ADMIN_SINGLE_CREATED,
    TOPIC_CONVERSATION_ADMIN_ASSIGNED,
    TOPIC_CONVERSATION_ADMIN_OPEN_ASSIGNED,
    TOPIC_CONVERSATION_ADMIN_NOTED,
    TOPIC_CONVERSATION_ADMIN_CLOSED,
    TOPIC_CONVERSATION_ADMIN_OPENED,
    TOPIC_CONVERSATION_ADMIN_SNOOZED,
    TOPIC_CONVERSATION_ADMIN_UNSNOOZED,
    TOPIC_CONVERSATION_OPERATOR_REPLIED,
    TOPIC_CONVERSATION_PRIORITY_UPDATED,
    TOPIC_CONVERSATION_RATING_ADDED,
)

UI_UNIT_ADMIN_PICKER: Final = "adminPicker"
"""Studio unit that lists the workspace's admins and stores the chosen admin's numeric ID."""
UI_ADMIN_PICKER_PORT_ADMIN_ID: Final = "adminId"
"""Admin picker output port."""
UI_UNIT_CONVERSATION_TOPIC_PICKER: Final = "conversationTopicPicker"
"""Studio unit that selects a conversationEvent binding's webhook topics."""
UI_CONVERSATION_TOPIC_PICKER_PORT_TOPICS: Final = "topics"
"""Topic picker output port, a list of topic names."""

_TRIGGER_NAME: Final = CONVERSATION_EVENT_TRIGGER_DEFINITION.trigger.trigger_name
_HUB_SIGNATURE_HEADER: Final = "X-Hub-Signature"
_HUB_SIGNATURE_PREFIX: Final = "sha1="
_SHA1_HEX: Final = re.compile(r"[0-9A-Fa-f]{40}\Z", re.ASCII)
# Bounds notification IDs, which applications embed in Flow IDs.
_MAXIMUM_NOTIFICATION_ID_BYTES: Final = 256
# Go's unicode.IsSpace set, which strings.TrimSpace removes.
_GO_SPACE: Final = "\t\n\v\f\r \x85\xa0                　"


def conversation_event_topics() -> list[str]:
    """Return every topic the conversationEvent Trigger decodes, in the Go connector's stable order.

    Each call returns a new list. The Trigger answers 200 to every other topic, including Intercom's
    periodic ping, without recording it.
    """
    return list(_SUPPORTED_TOPICS)


def validate_conversation_event_trigger_configuration(
    configuration: ConversationEventTriggerConfiguration,
) -> None:
    """Check that every configured topic is supported and listed once; raises ValueError otherwise.

    An empty ``topics`` list accepts every topic `conversation_event_topics` returns.
    """
    seen: set[str] = set()
    for topic in configuration.topics:
        if topic not in _SUPPORTED_TOPICS:
            raise ValueError(
                f"Intercom webhook topic {_go_quote(topic)} is not a supported conversation topic"
            )
        if topic in seen:
            raise ValueError(f"Intercom webhook topic {_go_quote(topic)} is listed twice")
        seen.add(topic)


def _accepts_topic(configuration: ConversationEventTriggerConfiguration, topic: str) -> bool:
    if not configuration.topics:
        return topic in _SUPPORTED_TOPICS
    return topic in configuration.topics


def _go_quote(value: str) -> str:
    return json.dumps(value, ensure_ascii=False)


async def _verify_conversation_event_request(
    request: WebhookRequest, credentials: Credentials, cancellation: TriggerCancellation
) -> None:
    """Check X-Hub-Signature: sha1= and the hex HMAC-SHA1 of the body under the client secret."""
    secret = credentials.client_secret.reveal() if credentials.client_secret is not None else ""
    if not secret:
        raise WebhookVerificationUnavailableError(ValueError("Intercom client_secret is not configured"))
    # Go's Header.Get reads only the first value.
    header = _trim_go_space(request.header(_HUB_SIGNATURE_HEADER) or "")
    if not header.startswith(_HUB_SIGNATURE_PREFIX):
        raise ValueError("X-Hub-Signature is missing or does not start with sha1=")
    encoded = header[len(_HUB_SIGNATURE_PREFIX) :]
    if _SHA1_HEX.match(encoded) is None:
        raise ValueError("X-Hub-Signature is not a hex SHA-1 signature")
    expected = hmac.new(secret.encode(), request.body, hashlib.sha1).digest()
    if not hmac.compare_digest(bytes.fromhex(encoded), expected):
        raise ValueError("X-Hub-Signature does not match the body")


def _trim_go_space(value: str) -> str:
    # ASGI decodes header bytes as Latin-1; Go trims the UTF-8 runes those bytes spell.
    as_utf8 = value.encode("latin-1", errors="replace").decode("utf-8", errors="surrogateescape")
    return as_utf8.strip(_GO_SPACE).encode("utf-8", errors="surrogateescape").decode("latin-1")


def _decode_conversation_event_request(request: WebhookRequest) -> TriggerEvent[ConversationEvent] | None:
    """Decode a verified notification; a topic other than a supported conversation topic is unhandled."""
    notification = _decode_notification_object(request.body)
    notification_type = _envelope_string(notification, "type")
    notification_id = _envelope_string(notification, "id")
    topic = _envelope_string(notification, "topic")
    workspace_id = _envelope_string(notification, "app_id")
    try:
        notified_at = _time(_go_field(notification, "created_at"), "created_at")
    except ValueError as error:
        raise ValueError("Intercom notification is not JSON") from error
    # Go decodes data as an object for every topic, so a malformed one fails even for an unhandled topic.
    data = _go_field(notification, "data")
    if data is not None and not isinstance(data, dict):
        raise ValueError("Intercom notification is not JSON")
    if (
        notification_type != "notification_event"
        or not topic
        or not _is_printable_identifier(notification_id, _MAXIMUM_NOTIFICATION_ID_BYTES)
    ):
        raise ValueError("Intercom notification envelope is invalid")
    if topic not in _SUPPORTED_TOPICS:
        return None
    item = _go_field(data or {}, "item", _MISSING)
    if item is _MISSING or (item is not None and not isinstance(item, dict)):
        raise ValueError("Intercom notification item is not a conversation")
    try:
        conversation = _conversation(_object(_go_folded_keys(item)))
    except ValueError as error:
        raise ValueError(f"Intercom notification item: {error}") from error
    if notified_at == _ZERO_TIME:
        notified_at = request.received_at.astimezone(UTC)
    return TriggerEvent(
        notification_id,
        notified_at,
        ConversationEvent(
            notification_id=notification_id,
            topic=topic,
            workspace_id=workspace_id,
            conversation=conversation,
            notified_at=notified_at,
        ),
    )


_MISSING: Final = object()


def _reject_json_constant(name: str) -> Any:
    raise ValueError(f"JSON does not allow {name}")


def _decode_notification_object(body: bytes) -> dict[str, Any]:
    # Go replaces invalid UTF-8 and rejects NaN and Infinity; a JSON null decodes to the zero envelope.
    try:
        decoded = json.loads(body.decode("utf-8", errors="replace"), parse_constant=_reject_json_constant)
    except ValueError as error:
        raise ValueError("Intercom notification is not JSON") from error
    if decoded is None:
        return {}
    if not isinstance(decoded, dict):
        raise ValueError("Intercom notification is not JSON")
    return decoded


def _go_field(document: dict[str, Any], name: str, missing: Any = None) -> Any:
    """Return the value encoding/json assigns to a field: the last key equal to name ignoring ASCII case."""
    value = missing
    for key, candidate in document.items():
        if key == name or (key.isascii() and key.lower() == name):
            value = candidate
    return value


# The only non-ASCII runes whose case folding reaches an ASCII letter: long s and the Kelvin sign.
_GO_ASCII_FOLDS: Final = str.maketrans({"\u017f": "s", "\u212a": "k"})


def _go_folded_keys(value: Any) -> Any:
    """Rename object keys to the lowercase field names encoding/json would match, recursively.

    Every conversation wire field is lowercase ASCII and is matched ignoring case, with the last
    matching key winning, so ``{"ID": "7"}`` decodes as ``{"id": "7"}``. Keys that cannot match a
    field keep their spelling.
    """
    if isinstance(value, list):
        return [_go_folded_keys(item) for item in value]
    if not isinstance(value, dict):
        return value
    folded: dict[str, Any] = {}
    for key, item in value.items():
        candidate = key.translate(_GO_ASCII_FOLDS)
        name = candidate.lower() if candidate.isascii() else key
        folded.pop(name, None)
        folded[name] = _go_folded_keys(item)
    return folded


def _envelope_string(notification: dict[str, Any], name: str) -> str:
    value = _go_field(notification, name)
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError("Intercom notification is not JSON")
    return value


def _is_printable_identifier(value: str, maximum_bytes: int) -> bool:
    encoded = value.encode("utf-8", errors="surrogatepass")
    return 0 < len(encoded) <= maximum_bytes and all(0x20 < byte <= 0x7E for byte in encoded)


def _decode_conversation_event_payload(payload_json: str, payload_bytes: bytes) -> ConversationEvent:
    """Decode a replayed inbox payload that Go or this package wrote."""
    codec = dex.CodecRegistry().resolve(ConversationEvent)
    decoded = codec.decode(dex.Value(dex.WireKind.JSON, payload_json))
    if not isinstance(decoded, ConversationEvent):
        raise ValueError("pending Intercom conversationEvent payload is invalid")
    return decoded


def _new_conversation_event_webhook_endpoint(
    client: Client, connection: ConnectionRef, logger: TriggerLogger | None
) -> WebhookEndpoint[Credentials, ConversationEvent]:
    connection.validate()
    try:
        return WebhookEndpoint(
            WebhookEndpointConfig(
                connector_id=CONNECTOR_ID,
                trigger_name=_TRIGGER_NAME,
                connection=connection,
                credentials=client.credentials,
                verify_request=_verify_conversation_event_request,
                decode_event=_decode_conversation_event_request,
                maximum_body_bytes=client.webhook_max_body_bytes,
                clock=client.now,
                logger=logger,
            )
        )
    except ValueError as error:
        raise ValueError(f"Intercom webhook endpoint: {error}") from error


def _intercom_client_of(connection: Connection) -> Client:
    client = getattr(connection, "_client", None)
    if not isinstance(client, Client):
        raise ValueError("Intercom Triggers require a connection bound to an Intercom Client")
    return client


def new_conversation_event_trigger(
    *,
    connection: Connection,
    connection_name: str,
    binding_name: str,
    configuration: ConversationEventTriggerConfiguration,
    target: TriggerTarget[ConversationEvent],
) -> TriggerRunner:
    """Create one conversationEvent binding's runner on the connection's shared webhook endpoint.

    Serve `new_conversation_event_webhook_handler` of the same connection at the webhook URL; the
    runner receives notifications only while `run` is active. A notification whose topic the
    configuration does not select is still answered 200 and dropped. Wrap target with
    `new_durable_trigger_target` so a notification is recorded before Intercom is acknowledged, as
    `new_project_conversation_event_endpoint_runner` does. Records go to the client's trigger_logger.
    Raises ValueError for an unsupported or repeated topic, a connection_name that differs from the
    connection's, a connection without an Intercom Client, or a blank binding name.
    """
    client = _intercom_client_of(connection)
    if connection_name != connection.reference.name:
        raise ValueError(
            f"Intercom Trigger connection_name {_go_quote(connection_name)} must equal its connection's "
            f"name {_go_quote(connection.reference.name)}"
        )
    validate_conversation_event_trigger_configuration(configuration)
    endpoint = client.conversation_event_webhook_endpoint(connection.reference)

    def accepts_event(event: TriggerEvent[ConversationEvent]) -> bool:
        return _accepts_topic(configuration, event.payload.topic)

    return new_trigger(
        TriggerConfig(
            definition=CONVERSATION_EVENT_TRIGGER_DEFINITION,
            binding=TriggerBindingRef(
                connection.reference, CONVERSATION_EVENT_TRIGGER_DEFINITION.trigger, binding_name
            ),
            source=endpoint.new_source(accepts_event),
            target=target,
            logger=client.trigger_logger,
        )
    )


_HEAD_VALIDATION_RESPONSE: Final = WebhookResponse(200)


class _HeadValidatingWebhookApplication:
    """Answer Intercom's HEAD URL validation with 200 and pass every other request to the endpoint."""

    def __init__(self, endpoint: WebhookEndpoint[Credentials, ConversationEvent]) -> None:
        self._endpoint = endpoint

    async def __call__(self, scope: Any, receive: Any, send: Any) -> None:
        if scope.get("type") == "http" and str(scope.get("method", "")).upper() == "HEAD":
            await _send_head_validation(send)
            return
        await self._endpoint(scope, receive, send)


def new_conversation_event_webhook_handler(connection: Connection) -> Callable[[Any, Any, Any], Any]:
    """Return the connection's ASGI 3 webhook application for the URL in Configure > Webhooks.

    It answers Intercom's HEAD validation request with 200. Every conversationEvent Trigger built from
    this connection feeds from it, and it answers 503 to a notification while none of them runs, so
    Intercom retries. Raises ValueError for a connection without an Intercom Client.
    """
    client = _intercom_client_of(connection)
    return _HeadValidatingWebhookApplication(client.conversation_event_webhook_endpoint(connection.reference))


async def _send_head_validation(send: Any) -> None:
    await send({"type": "http.response.start", "status": 200, "headers": []})
    await send({"type": "http.response.body", "body": b""})


@dataclass(frozen=True, slots=True)
class ProjectConversationEventTriggerRoute:
    """Bind one stored conversationEvent binding to its application target."""

    binding_name: str
    """Names the stored Trigger binding whose configuration selects the topics."""
    target: TriggerTarget[ConversationEvent]
    """Receives the binding's events through its durable project inbox, such as a
    `new_dex_flow_trigger_target` that starts one Flow per conversation."""


class ConversationEventEndpointRunner:
    """Serve one connection's Intercom webhook endpoint while running its bindings.

    It is an ASGI 3 application: mount it at the URL entered in the Intercom app's Configure > Webhooks
    page and serve it with an ASGI server such as uvicorn, whose ``lifespan`` startup runs the bindings
    on the server's event loop and whose shutdown stops them; a host that owns the loop calls `run`
    instead. It answers HEAD with 200, and 503 to a notification until every binding has replayed its
    pending events and started receiving, so Intercom retries. Requests may be served concurrently.
    Close it with `aclose` after the server stops; it closes the connection it opened.
    """

    def __init__(
        self,
        endpoint: WebhookEndpoint[Credentials, ConversationEvent],
        runners: Sequence[TriggerRunner],
        connection: Connection,
    ) -> None:
        """Bind the endpoint and runners; use `new_project_conversation_event_endpoint_runner`."""
        self._endpoint = endpoint
        self._endpoint_runner = WebhookEndpointRunner(endpoint, runners)
        self._connection = connection

    async def __call__(self, scope: Any, receive: Any, send: Any) -> None:
        """Serve the ASGI ``http`` and ``lifespan`` scopes."""
        if scope.get("type") == "http" and str(scope.get("method", "")).upper() == "HEAD":
            await _send_head_validation(send)
            return
        await self._endpoint_runner(scope, receive, send)

    async def handle_request(
        self,
        method: str,
        url: str,
        headers: Iterable[tuple[str, str]],
        body: bytes,
        cancellation: TriggerCancellation | None = None,
    ) -> WebhookResponse:
        """Answer one request with its complete raw body, for hosts that are not ASGI servers.

        It answers HEAD with 200; otherwise 405 to a method other than POST, 413 to a body over
        webhookMaxBodyBytes, 503 while the client secret is blank or no binding runs, 400 to a bad
        signature or envelope, and 200 once every accepting binding recorded the notification.
        """
        if method.upper() == "HEAD":
            return _HEAD_VALIDATION_RESPONSE
        return await self._endpoint_runner.handle_request(method, url, headers, body, cancellation)

    async def run(self, context: TriggerContext | None = None) -> None:
        """Replay each binding's durable inbox, then deliver new notifications until context ends.

        Use it instead of the ASGI ``lifespan`` when the host owns the event loop. Raises
        TriggerDeliveryAbortedError after cancellation, or the first binding's failure.
        """
        await self._endpoint_runner.run(context)

    def running_source_count(self) -> int:
        """Report how many bindings are receiving notifications, for a readiness check.

        Until it equals the number of routes, every notification is answered 503.
        """
        return self._endpoint.running_source_count()

    async def aclose(self) -> None:
        """Close the connection the runner opened, after the bindings stop."""
        await self._connection.aclose()


_DurableConversationEventTarget = Callable[
    [TriggerInboxKey, TriggerTarget[ConversationEvent]], TriggerTarget[ConversationEvent]
]


async def new_project_conversation_event_endpoint_runner(
    project: LoadedProject,
    connection_name: str,
    routes: Sequence[ProjectConversationEventTriggerRoute],
    http_client: httpx.AsyncClient | None = None,
    logger: TriggerLogger | None = None,
) -> ConversationEventEndpointRunner:
    """Open connection_name and every route's stored binding and serve them on one webhook endpoint.

    Each route's target is wrapped in its binding's durable project inbox, so a notification is in
    project storage before Intercom receives 200 and replays after a restart. logger receives the
    endpoint's, runners', and inboxes' records; None uses the standard logging adapter.
    Raises ValueError when there are no routes, a binding name is blank or repeated, a target is
    missing, or a stored configuration is invalid, and ObjectNotFoundError for a binding the project
    does not store. Close the runner with `aclose`.
    """
    if project is None:
        raise ValueError("Intercom conversationEvent endpoint runner requires the loaded project")
    connection = new_project_connection(project, connection_name, http_client)
    client = _intercom_client_of(connection)
    try:
        client.conversation_event_webhook_endpoint(connection.reference, logger)

        def make_durable(
            key: TriggerInboxKey, target: TriggerTarget[ConversationEvent]
        ) -> TriggerTarget[ConversationEvent]:
            return new_durable_trigger_target(
                project.trigger_inbox(key),
                target,
                decode_payload=_decode_conversation_event_payload,
                logger=logger or client.trigger_logger,
            )

        return _new_conversation_event_endpoint_runner(
            connection, project.configuration, routes, make_durable
        )
    except BaseException:
        await connection.aclose()
        raise


def _new_conversation_event_endpoint_runner(
    connection: Connection,
    configuration: Configuration,
    routes: Sequence[ProjectConversationEventTriggerRoute],
    make_durable: _DurableConversationEventTarget,
) -> ConversationEventEndpointRunner:
    if not routes:
        raise ValueError("Intercom conversationEvent routes are required")
    client = _intercom_client_of(connection)
    endpoint = client.conversation_event_webhook_endpoint(connection.reference)
    connection_name = connection.reference.name
    runners: list[TriggerRunner] = []
    binding_names: set[str] = set()
    for route in routes:
        binding_name = _trim_go_space(route.binding_name or "") if route is not None else ""
        if not binding_name or route.target is None:
            raise ValueError("Intercom conversationEvent binding name and target are required")
        if binding_name in binding_names:
            raise ValueError(
                f"Intercom conversationEvent binding name {_go_quote(binding_name)} is duplicated"
            )
        binding_names.add(binding_name)
        key = TriggerInboxKey(CONNECTOR_ID, connection_name, _TRIGGER_NAME, binding_name)
        try:
            binding_configuration = decode_trigger_configuration(
                configuration, key, decode_conversation_event_trigger_configuration
            )
        except ValueError as error:
            raise ValueError(
                f"Intercom conversationEvent binding {_go_quote(binding_name)} configuration: {error}"
            ) from error
        try:
            validate_conversation_event_trigger_configuration(binding_configuration)
        except ValueError as error:
            raise ValueError(f"binding {_go_quote(binding_name)}: {error}") from error
        runners.append(
            new_conversation_event_trigger(
                connection=connection,
                connection_name=connection_name,
                binding_name=binding_name,
                configuration=binding_configuration,
                target=make_durable(key, route.target),
            )
        )
    return ConversationEventEndpointRunner(endpoint, runners, connection)


__all__ = [
    "CONVERSATION_EVENT_TRIGGER_DEFINITION",
    "TOPIC_CONVERSATION_ADMIN_ASSIGNED",
    "TOPIC_CONVERSATION_ADMIN_CLOSED",
    "TOPIC_CONVERSATION_ADMIN_NOTED",
    "TOPIC_CONVERSATION_ADMIN_OPENED",
    "TOPIC_CONVERSATION_ADMIN_OPEN_ASSIGNED",
    "TOPIC_CONVERSATION_ADMIN_REPLIED",
    "TOPIC_CONVERSATION_ADMIN_SINGLE_CREATED",
    "TOPIC_CONVERSATION_ADMIN_SNOOZED",
    "TOPIC_CONVERSATION_ADMIN_UNSNOOZED",
    "TOPIC_CONVERSATION_OPERATOR_REPLIED",
    "TOPIC_CONVERSATION_PRIORITY_UPDATED",
    "TOPIC_CONVERSATION_RATING_ADDED",
    "TOPIC_CONVERSATION_USER_CREATED",
    "TOPIC_CONVERSATION_USER_REPLIED",
    "UI_ADMIN_PICKER_PORT_ADMIN_ID",
    "UI_CONVERSATION_TOPIC_PICKER_PORT_TOPICS",
    "UI_UNIT_ADMIN_PICKER",
    "UI_UNIT_CONVERSATION_TOPIC_PICKER",
    "ConversationEvent",
    "ConversationEventEndpointRunner",
    "ConversationEventTriggerConfiguration",
    "ProjectConversationEventTriggerRoute",
    "conversation_event_topics",
    "decode_conversation_event_trigger_configuration",
    "define_conversation_event_trigger_binding",
    "new_conversation_event_trigger",
    "new_conversation_event_webhook_handler",
    "new_project_conversation_event_endpoint_runner",
    "validate_conversation_event_trigger_configuration",
]
