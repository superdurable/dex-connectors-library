# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Slack channelThreadCreated, threadReplyCreated, and buttonClicked Triggers.

Slack sends each Socket Mode event to only one of an app's open connections, so one `MessageTriggerRunner`
receives every Trigger route of a connection. It uses Socket Mode when the connection's credential has an
``app_token``. A runner from `new_project_message_trigger_runner` whose shared connection has none receives
the Events API callbacks its Dex host relays instead, at `relayed_trigger_path`.
"""

from __future__ import annotations

import base64
import json
from collections.abc import Callable, Iterable, Sequence
from dataclasses import dataclass
from typing import Any, Final, TypeVar

import dex
import httpx
from dex_connectors.sdk import (
    TriggerBindingRef,
    TriggerCancellation,
    TriggerConfig,
    TriggerContext,
    TriggerEvent,
    TriggerRunner,
    TriggerTarget,
    has_durable_trigger_inbox,
    new_trigger,
)
from dex_connectors.sdk.projectconfig import (
    LoadedProject,
    TriggerInboxKey,
    decode_trigger_configuration,
    new_durable_trigger_target,
)
from dex_connectors.sdk.webhooktrigger import WebhookResponse

from ._generated.definitions import (
    BUTTON_CLICKED_TRIGGER_DEFINITION,
    CHANNEL_THREAD_CREATED_TRIGGER_DEFINITION,
    CONNECTOR_ID,
    THREAD_REPLY_CREATED_TRIGGER_DEFINITION,
)
from ._generated.triggers import (
    decode_button_clicked_trigger_configuration,
    decode_channel_thread_created_trigger_configuration,
    decode_thread_reply_created_trigger_configuration,
    define_button_clicked_trigger_binding,
    define_channel_thread_created_trigger_binding,
    define_thread_reply_created_trigger_binding,
)
from ._relayed_receiver import RELAYED_TRIGGER_PATH_PREFIX, RelayedMessageReceiver
from ._socket_mode_runner import SocketModeRoutes, replay_socket_mode_routes, run_socket_mode_routes
from ._trigger_sources import (
    BindingIdentity,
    ButtonRoute,
    ButtonSource,
    MessageRoute,
    MessageSource,
    SlackTriggerOptions,
    SlackTriggerTimings,
    validate_channel_thread_created_trigger_configuration,
    validate_thread_reply_created_trigger_configuration,
)
from .button_trigger import validate_button_clicked_trigger_configuration
from .client import Client
from .connection import Connection, new_project_connection
from .models import (
    ButtonClickedTriggerConfiguration,
    ButtonClickEvent,
    ChannelThreadCreatedTriggerConfiguration,
    MessageEvent,
    MessageMatcher,
    ThreadReplyCreatedTriggerConfiguration,
)

PayloadT = TypeVar("PayloadT")

UI_UNIT_CHANNEL_PICKER: Final = "channelPicker"
"""Slack channel picker UI unit, which stores a stable channel ID."""
UI_CHANNEL_PICKER_PORT_CHANNEL_ID: Final = "channelId"
"""Channel picker output port."""
UI_UNIT_MEMBER_PICKER: Final = "memberPicker"
"""Slack member picker UI unit, which stores member IDs."""
UI_MEMBER_PICKER_PORT_MEMBER_IDS: Final = "memberIds"
"""Member picker output port."""
UI_UNIT_TEXT_INPUT: Final = "textInput"
"""Plain text input UI unit."""
UI_TEXT_INPUT_PORT_TEXT: Final = "text"
"""Text input output port."""

_BUTTON_CLICKS_REQUIRE_DURABLE_INBOXES: Final = (
    "Slack button click Triggers require durable inboxes for every route on their Socket Mode connection: "
    "use new_project_message_trigger_runner, or give new_message_trigger_runner targets from "
    "new_durable_trigger_target"
)


def relayed_trigger_path(connection_name: str) -> str:
    """Return a connection's relayed event path: "/triggers/slack/" and its unpadded base64url name."""
    encoded = base64.urlsafe_b64encode(connection_name.encode()).decode().rstrip("=")
    return RELAYED_TRIGGER_PATH_PREFIX + encoded


def decode_message_event_payload(payload_json: str, payload_bytes: bytes) -> MessageEvent:
    """Decode a replayed inbox payload that Go, TypeScript, or this package recorded."""
    return _decode_dataclass_payload(MessageEvent, payload_json)


def decode_button_click_event_payload(payload_json: str, payload_bytes: bytes) -> ButtonClickEvent:
    """Decode a replayed inbox click that Go, TypeScript, or this package recorded."""
    return _decode_dataclass_payload(ButtonClickEvent, payload_json)


def _decode_dataclass_payload(payload_type: type[PayloadT], payload_json: str) -> PayloadT:
    decoded = dex.CodecRegistry().resolve(payload_type).decode(dex.Value(dex.WireKind.JSON, payload_json))
    if not isinstance(decoded, payload_type):
        raise ValueError(f"pending Slack {payload_type.__name__} is invalid")
    return decoded


@dataclass(frozen=True, slots=True)
class ChannelThreadCreatedTriggerRoute:
    """One root-message binding and its application target."""

    binding_name: str
    """Configured binding name."""
    configuration: ChannelThreadCreatedTriggerConfiguration
    """Channel and root-message matcher."""
    target: TriggerTarget[MessageEvent]
    """Routes accepted events into the application."""


@dataclass(frozen=True, slots=True)
class ThreadReplyCreatedTriggerRoute:
    """One reply-message binding and its application target."""

    binding_name: str
    """Configured binding name."""
    configuration: ThreadReplyCreatedTriggerConfiguration
    """Channel and reply matcher, which requires at least one poster."""
    target: TriggerTarget[MessageEvent]
    """Routes accepted events into the application."""


@dataclass(frozen=True, slots=True)
class ButtonClickedTriggerRoute:
    """One button click binding and its application target."""

    binding_name: str
    """Configured binding name."""
    configuration: ButtonClickedTriggerConfiguration
    """Channel, action ID, and clicker filters."""
    target: TriggerTarget[ButtonClickEvent]
    """Routes accepted clicks into the application."""


@dataclass(frozen=True, slots=True)
class ProjectChannelThreadCreatedTriggerRoute:
    """One stored root-message binding and its application target."""

    binding_name: str
    """Stored binding name."""
    target: TriggerTarget[MessageEvent]
    """Receives events through the binding's durable project inbox."""


@dataclass(frozen=True, slots=True)
class ProjectThreadReplyCreatedTriggerRoute:
    """One stored reply-message binding and its application target."""

    binding_name: str
    """Stored binding name."""
    target: TriggerTarget[MessageEvent]
    """Receives events through the binding's durable project inbox."""


@dataclass(frozen=True, slots=True)
class ProjectButtonClickedTriggerRoute:
    """One stored button click binding and its application target."""

    binding_name: str
    """Stored binding name."""
    target: TriggerTarget[ButtonClickEvent]
    """Receives clicks through the binding's durable project inbox."""


class MessageTriggerRunner:
    """Receives Slack messages and button clicks for one connection and dispatches matching routes.

    Create it with `new_message_trigger_runner` or `new_project_message_trigger_runner`. It is an ASGI 3
    application for the relayed route; `run` must run on the same event loop. Close it with `aclose`,
    which closes a connection the runner opened.
    """

    def __init__(
        self,
        routes: SocketModeRoutes,
        relay: RelayedMessageReceiver | None,
        owned_connection: Connection | None = None,
    ) -> None:
        """Wrap prepared routes; applications use the factory functions instead."""
        self._routes = routes
        self._relay = relay
        self._owned_connection = owned_connection

    async def run(self, context: TriggerContext | None = None) -> None:
        """Receive and dispatch Slack events until context is cancelled or the runner fails.

        It first replays each route's durable inbox in order: roots, then replies, then clicks; before each
        reconnect it replays them again, so an event persisted on a failed connection is delivered before any
        envelope read on the next one. When every target is a durable inbox, it acknowledges each envelope
        once the matching inboxes recorded it and delivers in arrival order separately, so a retrying target
        does not delay later acknowledgements; otherwise it delivers each event before reading the next.

        A project runner first reads the credential, retrying with the reconnect backoff while it is
        unavailable. With an ``app_token`` it uses Socket Mode. Without one, on a shared connection, it
        replays the inboxes, serves relayed events, and registers the connection with the host, retrying
        while the host is unavailable. It raises ValueError when the host refuses the registration, when the
        connection is not shared, and for button routes, because a host relays only Events API messages.
        Raises TriggerDeliveryAbortedError after cancellation.
        """
        context = context if context is not None else TriggerContext()
        if self._relay is not None and await self._relay.select_relayed_mode(context):
            if self._routes.buttons:
                raise ValueError(
                    f'Slack connection "{self._relay.connection_name}" has no app_token for Socket Mode, '
                    "and button clicks reach only Socket Mode; add an app_token or remove the buttonClicked "
                    "routes"
                )
            await self._relay.run(context)
            return
        await replay_socket_mode_routes(context, self._routes)
        await run_socket_mode_routes(context, self._routes)

    async def handle_request(
        self,
        method: str,
        url: str,
        headers: Iterable[tuple[str, str]],
        body: bytes,
        cancellation: TriggerCancellation | None = None,
    ) -> WebhookResponse:
        """Answer one relayed request with its complete raw body, for hosts that are not ASGI servers.

        It answers 200 once every matching binding's inbox recorded the event, 400 without a valid host
        signature or for an invalid body, 413 for a body over 1 MiB, 405 for another method, and 503 while
        the runner does not receive relayed events, such as before `run` or when it uses Socket Mode. A
        runner without a project configuration answers 404.
        """
        if self._relay is None:
            return WebhookResponse(404)
        return await self._relay.endpoint.handle_request(method, url, headers, body, cancellation)

    async def __call__(self, scope: Any, receive: Any, send: Any) -> None:
        """Serve the relayed route as an ASGI 3 ``http`` application, as `handle_request` answers."""
        if scope.get("type") != "http":
            raise ValueError("unsupported ASGI scope type")
        if self._relay is None:
            await send({"type": "http.response.start", "status": 404, "headers": []})
            await send({"type": "http.response.body", "body": b""})
            return
        await self._relay.endpoint(scope, receive, send)

    async def aclose(self) -> None:
        """Close the connection the runner opened, after `run` returned."""
        if self._owned_connection is not None:
            await self._owned_connection.aclose()


def new_message_trigger_runner(
    connection: Connection,
    *,
    channel_thread_created_routes: Sequence[ChannelThreadCreatedTriggerRoute] = (),
    thread_reply_created_routes: Sequence[ThreadReplyCreatedTriggerRoute] = (),
    button_clicked_routes: Sequence[ButtonClickedTriggerRoute] = (),
    options: SlackTriggerOptions | None = None,
) -> MessageTriggerRunner:
    """Create one Socket Mode runner for every message and button route of a connection.

    Button clicks reach only Socket Mode, so the credential needs an ``app_token`` and the Slack app needs
    Interactivity. With button routes every target, message routes included, must be a durable inbox from
    `new_durable_trigger_target`, so a click is acknowledged within three seconds without risking its loss.
    Raises ValueError for no routes, a blank or repeated binding name, an invalid configuration, or button
    routes without durable inboxes.
    """
    return MessageTriggerRunner(
        _socket_mode_routes(
            connection,
            channel_thread_created_routes,
            thread_reply_created_routes,
            button_clicked_routes,
            options or SlackTriggerOptions(),
        ),
        None,
    )


async def new_project_message_trigger_runner(
    project: LoadedProject,
    connection_name: str,
    *,
    channel_thread_created_routes: Sequence[ProjectChannelThreadCreatedTriggerRoute] = (),
    thread_reply_created_routes: Sequence[ProjectThreadReplyCreatedTriggerRoute] = (),
    button_clicked_routes: Sequence[ProjectButtonClickedTriggerRoute] = (),
    http_client: httpx.AsyncClient | None = None,
    options: SlackTriggerOptions | None = None,
) -> MessageTriggerRunner:
    """Open connection_name and its stored bindings and create one durable runner.

    The runner uses Socket Mode or the host's relay as `MessageTriggerRunner.run` describes; serve it at
    `relayed_trigger_path(connection_name)`. Each route's target receives events through the binding's
    durable project inbox, which every replica shares. Raises ObjectNotFoundError for a binding the project
    does not store and ValueError for an invalid stored configuration. Close the runner with `aclose`.
    """
    if project is None:
        raise ValueError("Slack Trigger runner requires the loaded project configuration")
    resolved_options = options or SlackTriggerOptions()
    connection = new_project_connection(project, connection_name, http_client)
    try:
        routes = _project_socket_mode_routes(
            project,
            connection,
            channel_thread_created_routes,
            thread_reply_created_routes,
            button_clicked_routes,
            resolved_options,
        )
    except BaseException:
        await connection.aclose()
        raise
    return MessageTriggerRunner(
        routes, RelayedMessageReceiver(project, routes.identity, routes.messages), connection
    )


def _slack_client_of(connection: Connection) -> Client:
    client = getattr(connection, "_client", None)
    if not isinstance(client, Client):
        raise ValueError("Slack Triggers require a connection bound to a Slack Client")
    return client


def _socket_mode_routes(
    connection: Connection,
    channel_thread_created_routes: Sequence[ChannelThreadCreatedTriggerRoute],
    thread_reply_created_routes: Sequence[ThreadReplyCreatedTriggerRoute],
    button_clicked_routes: Sequence[ButtonClickedTriggerRoute],
    options: SlackTriggerOptions,
) -> SocketModeRoutes:
    options.validate()
    client = _slack_client_of(connection)
    reserved: set[tuple[str, str]] = set()

    def identity(trigger_name: str, binding_name: str, target: object) -> BindingIdentity:
        trimmed = (binding_name or "").strip()
        if not trimmed or target is None:
            raise ValueError("Slack message Trigger binding name and target are required")
        if (trigger_name, trimmed) in reserved:
            raise ValueError(f"Slack message Trigger binding name {json.dumps(trimmed)} is duplicated")
        reserved.add((trigger_name, trimmed))
        return BindingIdentity(client, connection.reference, trigger_name, binding_name, options)

    routes = SocketModeRoutes()
    for root in channel_thread_created_routes:
        validate_channel_thread_created_trigger_configuration(root.configuration)
        source = MessageSource(
            identity("channelThreadCreated", root.binding_name, root.target),
            root.configuration.channel_id,
            root.configuration.thread_trigger_matcher or MessageMatcher(),
            False,
        )
        routes.messages.append(MessageRoute(source, root.target))
    for reply in thread_reply_created_routes:
        validate_thread_reply_created_trigger_configuration(reply.configuration)
        source = MessageSource(
            identity("threadReplyCreated", reply.binding_name, reply.target),
            reply.configuration.channel_id,
            reply.configuration.thread_reply_matcher or MessageMatcher(),
            True,
        )
        routes.messages.append(MessageRoute(source, reply.target))
    for button in button_clicked_routes:
        validate_button_clicked_trigger_configuration(button.configuration)
        button_source = ButtonSource(
            identity("buttonClicked", button.binding_name, button.target), button.configuration
        )
        routes.buttons.append(ButtonRoute(button_source, button.target))
    if not routes.messages and not routes.buttons:
        raise ValueError("Slack message Trigger routes are required")
    if routes.buttons and not routes.has_durable_inboxes():
        raise ValueError(_BUTTON_CLICKS_REQUIRE_DURABLE_INBOXES)
    return routes


def _project_socket_mode_routes(
    project: LoadedProject,
    connection: Connection,
    channel_thread_created_routes: Sequence[ProjectChannelThreadCreatedTriggerRoute],
    thread_reply_created_routes: Sequence[ProjectThreadReplyCreatedTriggerRoute],
    button_clicked_routes: Sequence[ProjectButtonClickedTriggerRoute],
    options: SlackTriggerOptions,
) -> SocketModeRoutes:
    connection_name = connection.reference.name

    def stored(trigger_name: str, binding_name: str, decode: Callable[[dict[str, Any]], Any]) -> Any:
        key = TriggerInboxKey(CONNECTOR_ID, connection_name, trigger_name, binding_name)
        try:
            return decode_trigger_configuration(project.configuration, key, decode)
        except ValueError as error:
            raise ValueError(
                f"slack {trigger_name} binding {json.dumps(binding_name)} configuration: {error}"
            ) from error

    def durable(
        trigger_name: str,
        binding_name: str,
        target: TriggerTarget[PayloadT],
        decode_payload: Callable[[str, bytes], PayloadT],
    ) -> TriggerTarget[PayloadT]:
        key = TriggerInboxKey(CONNECTOR_ID, connection_name, trigger_name, binding_name)
        return new_durable_trigger_target(
            project.trigger_inbox(key), target, decode_payload=decode_payload, logger=options.logger
        )

    return _socket_mode_routes(
        connection,
        [
            ChannelThreadCreatedTriggerRoute(
                route.binding_name,
                stored(
                    "channelThreadCreated",
                    route.binding_name,
                    decode_channel_thread_created_trigger_configuration,
                ),
                durable(
                    "channelThreadCreated", route.binding_name, route.target, decode_message_event_payload
                ),
            )
            for route in channel_thread_created_routes
        ],
        [
            ThreadReplyCreatedTriggerRoute(
                route.binding_name,
                stored(
                    "threadReplyCreated",
                    route.binding_name,
                    decode_thread_reply_created_trigger_configuration,
                ),
                durable("threadReplyCreated", route.binding_name, route.target, decode_message_event_payload),
            )
            for route in thread_reply_created_routes
        ],
        [
            ButtonClickedTriggerRoute(
                route.binding_name,
                stored("buttonClicked", route.binding_name, decode_button_clicked_trigger_configuration),
                durable("buttonClicked", route.binding_name, route.target, decode_button_click_event_payload),
            )
            for route in button_clicked_routes
        ],
        options,
    )


class _SocketModeTriggerSource:
    """Runs one standalone binding's own Socket Mode connection."""

    def __init__(self, route: MessageRoute | ButtonRoute) -> None:
        self._route = route

    async def run(self, context: TriggerContext, target: TriggerTarget[Any]) -> None:
        if isinstance(self._route, ButtonRoute):
            if not has_durable_trigger_inbox(target):
                raise ValueError(_BUTTON_CLICKS_REQUIRE_DURABLE_INBOXES)
            routes = SocketModeRoutes(buttons=[ButtonRoute(self._route.source, target)])
        else:
            routes = SocketModeRoutes(messages=[MessageRoute(self._route.source, target)])
        await run_socket_mode_routes(context, routes)


def _new_standalone_trigger(
    connection: Connection,
    connection_name: str,
    route: MessageRoute | ButtonRoute,
    target: TriggerTarget[Any],
    options: SlackTriggerOptions,
) -> TriggerRunner:
    if connection_name != connection.reference.name:
        raise ValueError(
            f"slack connector trigger connection_name {json.dumps(connection_name)} must equal "
            f"its connection's name {json.dumps(connection.reference.name)}"
        )
    identity = route.source.identity
    definition = {
        "channelThreadCreated": CHANNEL_THREAD_CREATED_TRIGGER_DEFINITION,
        "threadReplyCreated": THREAD_REPLY_CREATED_TRIGGER_DEFINITION,
        "buttonClicked": BUTTON_CLICKED_TRIGGER_DEFINITION,
    }[identity.trigger_name]
    return new_trigger(
        TriggerConfig(
            definition=definition,
            binding=TriggerBindingRef(connection.reference, definition.trigger, identity.binding_name),
            source=_SocketModeTriggerSource(route),
            target=target,
            logger=options.logger,
        )
    )


def new_channel_thread_created_trigger(
    *,
    connection: Connection,
    connection_name: str,
    binding_name: str,
    configuration: ChannelThreadCreatedTriggerConfiguration,
    target: TriggerTarget[MessageEvent],
    options: SlackTriggerOptions | None = None,
) -> TriggerRunner:
    """Create one channelThreadCreated Trigger with its own Socket Mode connection.

    Prefer one `MessageTriggerRunner` per connection, because Slack sends each event to only one of an app's
    connections. Raises ValueError for a connection name that differs from the connection's or an invalid
    configuration.
    """
    resolved_options = options or SlackTriggerOptions()
    routes = _socket_mode_routes(
        connection,
        [ChannelThreadCreatedTriggerRoute(binding_name, configuration, target)],
        (),
        (),
        resolved_options,
    )
    return _new_standalone_trigger(connection, connection_name, routes.messages[0], target, resolved_options)


def new_thread_reply_created_trigger(
    *,
    connection: Connection,
    connection_name: str,
    binding_name: str,
    configuration: ThreadReplyCreatedTriggerConfiguration,
    target: TriggerTarget[MessageEvent],
    options: SlackTriggerOptions | None = None,
) -> TriggerRunner:
    """Create one threadReplyCreated Trigger with its own Socket Mode connection.

    Prefer one `MessageTriggerRunner` per connection. Raises ValueError for a connection name that differs
    from the connection's or an invalid configuration.
    """
    resolved_options = options or SlackTriggerOptions()
    routes = _socket_mode_routes(
        connection,
        (),
        [ThreadReplyCreatedTriggerRoute(binding_name, configuration, target)],
        (),
        resolved_options,
    )
    return _new_standalone_trigger(connection, connection_name, routes.messages[0], target, resolved_options)


class _UnpreparedButtonTarget:
    """Lets route validation accept a target that `new_trigger` wraps before the source sees it."""

    def __init__(self, target: TriggerTarget[ButtonClickEvent]) -> None:
        self._target = target

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[ButtonClickEvent]) -> None:
        await self._target.handle_trigger(context, event)

    async def prepare_trigger(self, context: TriggerContext, event: TriggerEvent[ButtonClickEvent]) -> None:
        return None

    async def replay_trigger_deliveries(self, context: TriggerContext) -> None:
        return None


def new_button_clicked_trigger(
    *,
    connection: Connection,
    connection_name: str,
    binding_name: str,
    configuration: ButtonClickedTriggerConfiguration,
    target: TriggerTarget[ButtonClickEvent],
    options: SlackTriggerOptions | None = None,
) -> TriggerRunner:
    """Create one buttonClicked Trigger, at parity with Go's generated NewButtonClickedTrigger.

    A click is acknowledged before delivery, so it needs a durable inbox, which `new_trigger` cannot pass to
    its source: `run` always raises ValueError. Use `new_project_message_trigger_runner` with
    ``button_clicked_routes`` instead.
    """
    resolved_options = options or SlackTriggerOptions()
    routes = _socket_mode_routes(
        connection,
        (),
        (),
        [ButtonClickedTriggerRoute(binding_name, configuration, _UnpreparedButtonTarget(target))],
        resolved_options,
    )
    return _new_standalone_trigger(connection, connection_name, routes.buttons[0], target, resolved_options)


__all__ = [
    "BUTTON_CLICKED_TRIGGER_DEFINITION",
    "CHANNEL_THREAD_CREATED_TRIGGER_DEFINITION",
    "THREAD_REPLY_CREATED_TRIGGER_DEFINITION",
    "UI_CHANNEL_PICKER_PORT_CHANNEL_ID",
    "UI_MEMBER_PICKER_PORT_MEMBER_IDS",
    "UI_TEXT_INPUT_PORT_TEXT",
    "UI_UNIT_CHANNEL_PICKER",
    "UI_UNIT_MEMBER_PICKER",
    "UI_UNIT_TEXT_INPUT",
    "ButtonClickedTriggerRoute",
    "ChannelThreadCreatedTriggerRoute",
    "MessageTriggerRunner",
    "ProjectButtonClickedTriggerRoute",
    "ProjectChannelThreadCreatedTriggerRoute",
    "ProjectThreadReplyCreatedTriggerRoute",
    "SlackTriggerOptions",
    "SlackTriggerTimings",
    "ThreadReplyCreatedTriggerRoute",
    "decode_button_click_event_payload",
    "decode_button_clicked_trigger_configuration",
    "decode_channel_thread_created_trigger_configuration",
    "decode_message_event_payload",
    "decode_thread_reply_created_trigger_configuration",
    "define_button_clicked_trigger_binding",
    "define_channel_thread_created_trigger_binding",
    "define_thread_reply_created_trigger_binding",
    "new_button_clicked_trigger",
    "new_channel_thread_created_trigger",
    "new_message_trigger_runner",
    "new_project_message_trigger_runner",
    "new_thread_reply_created_trigger",
    "relayed_trigger_path",
    "validate_button_clicked_trigger_configuration",
    "validate_channel_thread_created_trigger_configuration",
    "validate_thread_reply_created_trigger_configuration",
]
