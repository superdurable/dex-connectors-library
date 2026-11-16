# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Receive matching Gmail root messages and replies with the messageReceived and replyReceived Triggers.

Both Triggers poll the newest inbox page every connection ``pollInterval``, read each new message's
metadata, and deliver matching roots or replies oldest first, at behavior parity with the Go connector.
Run every binding of one connection on one `MessageTriggerRunner`, which delivers each poll's roots
before its replies, so a root starts its Flow before a reply to it arrives.
"""

from collections.abc import Sequence
from dataclasses import dataclass, field

from dex import CodecRegistry, Value, WireKind
from dex_connectors.sdk import (
    TriggerBindingRef,
    TriggerConfig,
    TriggerContext,
    TriggerDefinition,
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

from ._generated.definitions import (
    CONNECTOR_ID,
    MESSAGE_RECEIVED_TRIGGER_DEFINITION,
    REPLY_RECEIVED_TRIGGER_DEFINITION,
)
from ._generated.models import (
    MessageEvent,
    MessageMatcher,
    MessageReceivedTriggerConfiguration,
    ReplyReceivedTriggerConfiguration,
)
from ._generated.triggers import (
    decode_message_received_trigger_configuration,
    decode_reply_received_trigger_configuration,
    define_message_received_trigger_binding,
    define_reply_received_trigger_binding,
)
from ._trigger_source import (
    MessagePollingTriggerSource,
    MessageTriggerRouteSource,
    message_matcher_matches,
    poll_message_trigger_routes,
    replay_message_trigger_routes,
    run_message_trigger_polls,
    validate_message_matcher,
)
from .connection import Connection
from .provider import GmailProvider

_MESSAGE_RECEIVED = MESSAGE_RECEIVED_TRIGGER_DEFINITION.trigger.trigger_name
_REPLY_RECEIVED = REPLY_RECEIVED_TRIGGER_DEFINITION.trigger.trigger_name


@dataclass(frozen=True, slots=True)
class MessageReceivedTriggerRoute:
    """Binds one ``messageReceived`` configuration to its application target."""

    binding_name: str
    """The stable binding name, unique among the runner's messageReceived routes."""
    target: TriggerTarget[MessageEvent]
    """Routes accepted root messages into the application."""
    configuration: MessageReceivedTriggerConfiguration = field(
        default_factory=MessageReceivedTriggerConfiguration
    )
    """The Gmail search query and root-message matcher; the default matches every root message."""


@dataclass(frozen=True, slots=True)
class ReplyReceivedTriggerRoute:
    """Binds one ``replyReceived`` configuration to its application target."""

    binding_name: str
    """The stable binding name, unique among the runner's replyReceived routes."""
    target: TriggerTarget[MessageEvent]
    """Routes accepted replies into the application."""
    configuration: ReplyReceivedTriggerConfiguration = field(
        default_factory=ReplyReceivedTriggerConfiguration
    )
    """The Gmail search query and reply matcher; the default matches every reply."""


@dataclass(frozen=True, slots=True)
class ProjectMessageReceivedTriggerRoute:
    """Selects one ``messageReceived`` binding saved in the project configuration."""

    binding_name: str
    """Names the saved binding."""
    target: TriggerTarget[MessageEvent]
    """Routes accepted root messages into the application, usually `new_dex_flow_trigger_target`."""


@dataclass(frozen=True, slots=True)
class ProjectReplyReceivedTriggerRoute:
    """Selects one ``replyReceived`` binding saved in the project configuration."""

    binding_name: str
    """Names the saved binding."""
    target: TriggerTarget[MessageEvent]
    """Routes accepted replies into the application, usually `new_dex_rpc_trigger_target`."""


class MessageTriggerRunner:
    """Polls every configured Gmail route of one connection and delivers root messages before replies.

    Create it with `new_message_trigger_runner` or `new_project_message_trigger_runner`.
    """

    def __init__(
        self,
        root_routes: Sequence[MessageTriggerRouteSource],
        reply_routes: Sequence[MessageTriggerRouteSource],
        provider: GmailProvider,
    ) -> None:
        """Keep the validated routes; applications use the factory functions instead."""
        self._root_routes = tuple(root_routes)
        self._reply_routes = tuple(reply_routes)
        self._provider = provider

    async def run(self, context: TriggerContext | None = None) -> None:
        """Replay each route's durable inbox in order, roots first, then poll until the context ends.

        Each poll lists every reply route before any root route, then delivers every root before any
        reply, and ends at the first failure. Every later poll retries the failed event before that
        route's listed messages, even after Gmail stops listing it. Gmail receives a thread's root before
        any reply to it, so the root of every listed reply is in the later root listing and reaches Dex
        first.

        It logs through the provider's Trigger logger: WARN "gmail poll failed; retrying" with the
        consecutive failed polls as ``attempt`` and the poll interval as ``delay_ms`` when a list or read
        fails, WARN "trigger delivery failed; retrying" with the attempt number when a target fails, INFO
        "trigger delivered after retry" when a later poll delivers that message, DEBUG "trigger event
        ignored" with its ``reason``, and the durable inbox records. It raises
        TriggerDeliveryAbortedError once the context is cancelled, and RuntimeError("replay Gmail
        Trigger deliveries: ...") when an inbox replay fails.
        """
        delivery_context = context if context is not None else TriggerContext()
        await replay_message_trigger_routes(delivery_context, (*self._root_routes, *self._reply_routes))
        await run_message_trigger_polls(
            delivery_context,
            lambda: poll_message_trigger_routes(delivery_context, self._root_routes, self._reply_routes),
            self._provider.poll_interval,
        )


def new_message_trigger_runner(
    connection: Connection,
    *,
    message_received_routes: Sequence[MessageReceivedTriggerRoute] = (),
    reply_received_routes: Sequence[ReplyReceivedTriggerRoute] = (),
) -> MessageTriggerRunner:
    """Create one ordered runner for every configured Gmail message Trigger route of a connection.

    The delivered set covers the last listed page in memory, so a restart rescans the page; deterministic
    Flow starts and the application's RPC absorb the duplicates. Raises ValueError for a connection
    without a Gmail provider, no routes, a blank or repeated binding name, a missing target, or an
    invalid sender address.
    """
    provider = _connection_provider(connection)
    binding_keys: set[str] = set()
    root_routes = [
        _new_route(
            connection,
            provider,
            _MESSAGE_RECEIVED,
            route.binding_name,
            route.target,
            route.configuration.search_query,
            route.configuration.message_matcher,
            False,
            binding_keys,
        )
        for route in message_received_routes
    ]
    reply_routes = [
        _new_route(
            connection,
            provider,
            _REPLY_RECEIVED,
            route.binding_name,
            route.target,
            route.configuration.search_query,
            route.configuration.reply_matcher,
            True,
            binding_keys,
        )
        for route in reply_received_routes
    ]
    if not root_routes and not reply_routes:
        raise ValueError("Gmail message Trigger routes are required")
    return MessageTriggerRunner(root_routes, reply_routes, provider)


def new_project_message_trigger_runner(
    project: LoadedProject,
    connection: Connection,
    *,
    message_received_routes: Sequence[ProjectMessageReceivedTriggerRoute] = (),
    reply_received_routes: Sequence[ProjectReplyReceivedTriggerRoute] = (),
) -> MessageTriggerRunner:
    """Load the selected bindings' saved configuration and create one durable, ordered runner.

    connection is the one `new_project_connection` opened from project; the caller keeps owning it. Each
    route keeps acknowledged events in its binding's project Trigger inbox until its target consumes
    them, so a restarted or replaced replica replays them. Go and Python replicas share the inbox, whose
    payload is the Go connector's MessageEvent JSON. The provider's Trigger logger also receives the
    inbox records. Raises ValueError naming the binding when it is not configured or its saved
    configuration is invalid, and as `new_message_trigger_runner` does.
    """
    if project is None:
        raise ValueError("Gmail message Trigger runner requires the loaded project configuration")
    provider = _connection_provider(connection)

    def durable(
        trigger_name: str, binding_name: str, target: TriggerTarget[MessageEvent]
    ) -> tuple[TriggerInboxKey, TriggerTarget[MessageEvent]]:
        key = TriggerInboxKey(CONNECTOR_ID, connection.reference.name, trigger_name, binding_name)
        return key, new_durable_trigger_target(
            project.trigger_inbox(key),
            target,
            decode_payload=decode_message_event_payload,
            logger=provider.trigger_logger,
        )

    roots: list[MessageReceivedTriggerRoute] = []
    for root in message_received_routes:
        key, target = durable(_MESSAGE_RECEIVED, root.binding_name, root.target)
        try:
            root_configuration = decode_trigger_configuration(
                project.configuration, key, decode_message_received_trigger_configuration
            )
        except (ValueError, LookupError) as error:
            # Like Go, a binding missing from the project is named in the error.
            raise ValueError(
                f'gmail messageReceived binding "{root.binding_name}" configuration: {error}'
            ) from error
        roots.append(MessageReceivedTriggerRoute(root.binding_name, target, root_configuration))
    replies: list[ReplyReceivedTriggerRoute] = []
    for reply in reply_received_routes:
        key, target = durable(_REPLY_RECEIVED, reply.binding_name, reply.target)
        try:
            reply_configuration = decode_trigger_configuration(
                project.configuration, key, decode_reply_received_trigger_configuration
            )
        except (ValueError, LookupError) as error:
            # Like Go, a binding missing from the project is named in the error.
            raise ValueError(
                f'gmail replyReceived binding "{reply.binding_name}" configuration: {error}'
            ) from error
        replies.append(ReplyReceivedTriggerRoute(reply.binding_name, target, reply_configuration))
    return new_message_trigger_runner(
        connection, message_received_routes=roots, reply_received_routes=replies
    )


def new_message_received_trigger(
    *,
    connection: Connection,
    binding_name: str,
    target: TriggerTarget[MessageEvent],
    configuration: MessageReceivedTriggerConfiguration | None = None,
) -> TriggerRunner:
    """Create one ``messageReceived`` Trigger that polls on its own, as Go's generated factory does.

    Prefer one `MessageTriggerRunner` per connection, which delivers every root before any reply. The
    runner consumes undeliverable events and logs them with the binding identity. Raises ValueError as
    `new_message_trigger_runner` does.
    """
    configuration = configuration or MessageReceivedTriggerConfiguration()
    return _new_standalone_trigger(
        connection,
        MESSAGE_RECEIVED_TRIGGER_DEFINITION,
        binding_name,
        target,
        configuration.search_query,
        configuration.message_matcher,
        False,
    )


def new_reply_received_trigger(
    *,
    connection: Connection,
    binding_name: str,
    target: TriggerTarget[MessageEvent],
    configuration: ReplyReceivedTriggerConfiguration | None = None,
) -> TriggerRunner:
    """Create one ``replyReceived`` Trigger that polls on its own, as Go's generated factory does.

    Prefer one `MessageTriggerRunner` per connection, which delivers every root before any reply. Raises
    ValueError as `new_message_trigger_runner` does.
    """
    configuration = configuration or ReplyReceivedTriggerConfiguration()
    return _new_standalone_trigger(
        connection,
        REPLY_RECEIVED_TRIGGER_DEFINITION,
        binding_name,
        target,
        configuration.search_query,
        configuration.reply_matcher,
        True,
    )


def validate_message_received_trigger_configuration(
    configuration: MessageReceivedTriggerConfiguration,
) -> None:
    """Raise ValueError for an invalid or repeated sender address, as Go's Validate does."""
    validate_message_matcher(configuration.message_matcher)


def validate_reply_received_trigger_configuration(configuration: ReplyReceivedTriggerConfiguration) -> None:
    """Raise ValueError for an invalid or repeated sender address, as Go's Validate does."""
    validate_message_matcher(configuration.reply_matcher)


def matches_message_event(matcher: MessageMatcher | None, event: MessageEvent) -> bool:
    """Report whether event passes matcher, as the pollers check it before delivery.

    A non-empty ``message_contains`` must appear, ignoring case, in the subject or snippet; non-empty
    ``sender_emails`` must contain the sender's address, ignoring case. None matches every event.
    """
    return message_matcher_matches(matcher, event.subject, event.snippet, event.from_)


def decode_message_event_payload(payload_json: str, payload_bytes: bytes) -> MessageEvent:
    """Decode a replayed inbox payload, written by a Go or Python replica, as a MessageEvent.

    Raises ValueError for a payload that is not a Gmail message event.
    """
    try:
        decoded = CodecRegistry().resolve(MessageEvent).decode(Value(WireKind.JSON, payload_json))
    except (TypeError, ValueError) as error:
        raise ValueError("stored Gmail message event payload is invalid") from error
    if not isinstance(decoded, MessageEvent):
        raise ValueError("stored Gmail message event payload is invalid")
    return decoded


def _new_standalone_trigger(
    connection: Connection,
    definition: TriggerDefinition,
    binding_name: str,
    target: TriggerTarget[MessageEvent],
    search_query: str,
    matcher: MessageMatcher | None,
    requires_reply: bool,
) -> TriggerRunner:
    provider = _connection_provider(connection)
    route = _new_route(
        connection,
        provider,
        definition.trigger.trigger_name,
        binding_name,
        target,
        search_query,
        matcher,
        requires_reply,
        set(),
        # Like a Go source from a generated per-Trigger factory, the source has no binding name.
        include_binding_name=False,
    )
    binding = TriggerBindingRef(connection.reference, definition.trigger, binding_name)
    return new_trigger(TriggerConfig(definition, binding, route.source, target, provider.trigger_logger))


def _new_route(
    connection: Connection,
    provider: GmailProvider,
    trigger_name: str,
    binding_name: str,
    target: TriggerTarget[MessageEvent],
    search_query: str,
    matcher: MessageMatcher | None,
    requires_reply: bool,
    binding_keys: set[str],
    *,
    include_binding_name: bool = True,
) -> MessageTriggerRouteSource:
    trimmed_binding_name = (binding_name or "").strip()
    if not trimmed_binding_name or target is None:
        raise ValueError("Gmail message Trigger binding name and target are required")
    binding_key = f"{trigger_name}\0{trimmed_binding_name}"
    if binding_key in binding_keys:
        raise ValueError(f'Gmail message Trigger binding name "{trimmed_binding_name}" is duplicated')
    binding_keys.add(binding_key)
    validate_message_matcher(matcher)
    source = MessagePollingTriggerSource(
        provider,
        connection.reference,
        trigger_name,
        binding_name if include_binding_name else "",
        search_query,
        matcher,
        requires_reply,
    )
    return MessageTriggerRouteSource(source, target)


def _connection_provider(connection: Connection) -> GmailProvider:
    provider = getattr(connection, "_client", None) if connection is not None else None
    if not isinstance(provider, GmailProvider):
        raise ValueError("Gmail Triggers require a connection opened with a Gmail provider")
    return provider


__all__ = [
    "MESSAGE_RECEIVED_TRIGGER_DEFINITION",
    "REPLY_RECEIVED_TRIGGER_DEFINITION",
    "MessageReceivedTriggerRoute",
    "MessageTriggerRunner",
    "ProjectMessageReceivedTriggerRoute",
    "ProjectReplyReceivedTriggerRoute",
    "ReplyReceivedTriggerRoute",
    "decode_message_event_payload",
    "decode_message_received_trigger_configuration",
    "decode_reply_received_trigger_configuration",
    "define_message_received_trigger_binding",
    "define_reply_received_trigger_binding",
    "matches_message_event",
    "new_message_received_trigger",
    "new_message_trigger_runner",
    "new_project_message_trigger_runner",
    "new_reply_received_trigger",
    "validate_message_received_trigger_configuration",
    "validate_reply_received_trigger_configuration",
]
