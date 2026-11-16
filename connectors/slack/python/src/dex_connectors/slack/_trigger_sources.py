# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Binding sources, routes, and Events API and block_actions decoding shared by the Slack Triggers."""

from __future__ import annotations

from collections.abc import Mapping
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta
from typing import Any, Final

from dex_connectors.sdk import (
    CallID,
    ConnectionRef,
    OperationRef,
    TriggerContext,
    TriggerEvent,
    TriggerLogger,
    TriggerLogLevel,
    TriggerLogValue,
    TriggerTarget,
    bind_trigger_log_attributes,
    deliver_trigger,
    log_trigger_record,
    new_trigger_call,
)
from dex_connectors.sdk.types import Call

from ._generated.definitions import CONNECTOR_ID
from .button_trigger import decode_block_actions_payload, match_button_click_payload
from .client import Client
from .models import (
    ButtonClickedTriggerConfiguration,
    ButtonClickEvent,
    ChannelThreadCreatedTriggerConfiguration,
    MessageEvent,
    MessageMatcher,
    ThreadReplyCreatedTriggerConfiguration,
)
from .socket_mode import SlackSocketDialer, dial_slack_socket

_UNIX_EPOCH: Final = datetime(1970, 1, 1, tzinfo=UTC)
_INT64_LIMIT: Final = 2**63


@dataclass(frozen=True, slots=True)
class SlackTriggerTimings:
    """Socket Mode and relay timings; tests shorten them."""

    reconnect_delay: timedelta = timedelta(seconds=1)
    """Wait after the first failed connection and after Slack asks for a reconnect; doubles per failure."""
    reconnect_maximum_delay: timedelta = timedelta(seconds=30)
    """Cap of the doubled reconnect wait."""
    read_timeout: timedelta = timedelta(seconds=45)
    """A connection that receives neither an envelope nor a ping for this long is reconnected."""

    def validate(self) -> None:
        """Raise ValueError unless every timing is positive."""
        if min(self.reconnect_delay, self.reconnect_maximum_delay, self.read_timeout) <= timedelta(0):
            raise ValueError("Slack Trigger timings must be positive")


@dataclass(frozen=True, slots=True)
class SlackTriggerOptions:
    """Logging, Socket Mode, and timing settings shared by one connection's Slack Triggers."""

    logger: TriggerLogger | None = None
    """Receives Socket Mode connection records, ignored events at DEBUG, and delivery retries; None uses
    the SDK's standard logging adapter."""
    socket_dialer: SlackSocketDialer | None = None
    """Opens Socket Mode WebSockets; None uses the websockets package with the connection's response limit."""
    timings: SlackTriggerTimings = field(default_factory=SlackTriggerTimings)
    """Socket Mode reconnect and read timings, which match the Go connector by default."""
    delivery_queue_capacity: int = 256
    """Acknowledged Socket Mode envelopes that may await delivery on one connection, as in Go. A full queue
    stops reading the socket, so Slack holds the rest until delivery catches up."""

    def validate(self) -> None:
        """Raise ValueError for invalid timings or a capacity below one."""
        self.timings.validate()
        if isinstance(self.delivery_queue_capacity, bool) or self.delivery_queue_capacity < 1:
            raise ValueError("Slack Socket Mode delivery queue capacity must be a positive integer")


@dataclass(frozen=True, slots=True)
class BindingIdentity:
    """Names a Trigger source's connection and binding in credential reads and log records."""

    client: Client
    connection: ConnectionRef
    trigger_name: str
    binding_name: str
    options: SlackTriggerOptions

    @property
    def binding_logger(self) -> TriggerLogger:
        return bind_trigger_log_attributes(
            self.options.logger,
            {
                "connector": CONNECTOR_ID,
                "connection": self.connection.name,
                "trigger": self.trigger_name,
                "binding": self.binding_name,
            },
        )

    def is_debug_enabled(self) -> bool:
        logger = bind_trigger_log_attributes(self.options.logger, {})
        try:
            return logger.is_enabled_for(TriggerLogLevel.DEBUG)
        except Exception:  # noqa: BLE001
            return False  # A logger failure cannot change Trigger delivery state.

    def log_connection(
        self,
        context: TriggerContext,
        level: TriggerLogLevel,
        message: str,
        attributes: Mapping[str, TriggerLogValue] | None = None,
    ) -> None:
        log_trigger_record(
            self.options.logger,
            context,
            level,
            message,
            {"connector": CONNECTOR_ID, "connection": self.connection.name, **(attributes or {})},
        )

    def credential_call(self, call_id: CallID) -> Call:
        """Identify a credential read; a project provider refuses one without the connector ID."""
        return new_trigger_call(self.connection, OperationRef(CONNECTOR_ID, self.trigger_name), call_id)

    def socket_dialer(self) -> SlackSocketDialer:
        if self.options.socket_dialer is not None:
            return self.options.socket_dialer
        maximum_message_bytes = self.client.config.max_response_bytes

        async def dial(url: str) -> Any:
            return await dial_slack_socket(url, maximum_message_bytes)

        return dial

    def reconnect_backoff(self, failures: int) -> timedelta:
        """Return the wait after consecutive failure number failures, from 1, doubled up to the cap."""
        timings = self.options.timings
        delay = timings.reconnect_delay
        for _ in range(1, failures):
            if delay >= timings.reconnect_maximum_delay:
                break
            delay *= 2
        return min(delay, timings.reconnect_maximum_delay)


@dataclass(frozen=True, slots=True)
class MessageSource:
    """One message binding's identity, channel, matcher, and whether it matches replies or roots."""

    identity: BindingIdentity
    channel_id: str
    matcher: MessageMatcher
    requires_thread: bool


@dataclass(frozen=True, slots=True)
class ButtonSource:
    """One button binding's identity and filters."""

    identity: BindingIdentity
    configuration: ButtonClickedTriggerConfiguration


@dataclass(frozen=True, slots=True)
class MessageRoute:
    source: MessageSource
    target: TriggerTarget[MessageEvent]


@dataclass(frozen=True, slots=True)
class ButtonRoute:
    source: ButtonSource
    target: TriggerTarget[ButtonClickEvent]


@dataclass(frozen=True, slots=True)
class PendingMessageDelivery:
    source: MessageSource
    target: TriggerTarget[MessageEvent]
    event: TriggerEvent[MessageEvent]


@dataclass(frozen=True, slots=True)
class PendingButtonDelivery:
    source: ButtonSource
    target: TriggerTarget[ButtonClickEvent]
    event: TriggerEvent[ButtonClickEvent]


@dataclass(frozen=True, slots=True)
class SocketEnvelope:
    envelope_id: str
    type: str
    payload: Any
    has_payload: bool
    reason: str


def decode_socket_envelope(message: Any) -> SocketEnvelope:
    """Decode a Socket Mode envelope as Go's json.Unmarshal does; a wrong shape ends the connection."""
    if message is None:
        message = {}
    if not isinstance(message, dict):
        raise ValueError("read Slack Socket Mode envelope: invalid envelope")
    texts = []
    for name in ("envelope_id", "type", "reason"):
        value = message.get(name)
        if value is not None and not isinstance(value, str):
            raise ValueError("read Slack Socket Mode envelope: invalid envelope")
        texts.append(value or "")
    return SocketEnvelope(texts[0], texts[1], message.get("payload"), "payload" in message, texts[2])


def validate_message_matcher(matcher: MessageMatcher, requires_poster: bool) -> None:
    posters = matcher.poster_user_ids or ()
    if any(not poster.strip() for poster in posters) or len(set(posters)) != len(posters):
        raise ValueError("Slack poster user IDs must be non-empty and unique")
    if requires_poster and not posters:
        raise ValueError("Slack reply matcher requires at least one poster user ID")


def validate_channel_thread_created_trigger_configuration(
    configuration: ChannelThreadCreatedTriggerConfiguration,
) -> None:
    """Raise ValueError for a blank channel or blank or repeated poster IDs."""
    if not configuration.channel_id.strip():
        raise ValueError("Slack channel ID is required")
    validate_message_matcher(configuration.thread_trigger_matcher or MessageMatcher(), False)


def validate_thread_reply_created_trigger_configuration(
    configuration: ThreadReplyCreatedTriggerConfiguration,
) -> None:
    """Raise ValueError for a blank channel, blank or repeated poster IDs, or no poster at all."""
    if not configuration.channel_id.strip():
        raise ValueError("Slack channel ID is required")
    validate_message_matcher(configuration.thread_reply_matcher or MessageMatcher(), True)


def message_matches(matcher: MessageMatcher, user_id: str, text: str) -> bool:
    if matcher.message_contains and matcher.message_contains.lower() not in text.lower():
        return False
    posters = matcher.poster_user_ids or ()
    return not posters or user_id in posters


@dataclass(frozen=True, slots=True)
class EventsAPIPayload:
    event_id: str
    event_time: int
    team_id: str
    type: str
    subtype: str
    channel: str
    user: str
    bot_id: str
    text: str
    ts: str
    thread_ts: str


def _optional_text(value: dict[str, Any], name: str) -> str:
    item = value.get(name)
    if item is None:
        return ""
    if not isinstance(item, str):
        raise ValueError(f"decode Slack Events API payload: invalid {name}")
    return item


def decode_events_api_payload(envelope: SocketEnvelope) -> EventsAPIPayload:
    """Decode an Events API payload with Go json.Unmarshal semantics; null is the zero value."""
    if not envelope.has_payload:
        raise ValueError("decode Slack Events API payload: payload is missing")
    payload = {} if envelope.payload is None else envelope.payload
    if not isinstance(payload, dict):
        raise ValueError("decode Slack Events API payload: invalid payload")
    event = payload.get("event")
    event = {} if event is None else event
    if not isinstance(event, dict):
        raise ValueError("decode Slack Events API payload: invalid event")
    event_time = payload.get("event_time")
    event_time = 0 if event_time is None else event_time
    if isinstance(event_time, bool) or not isinstance(event_time, int) or abs(event_time) >= _INT64_LIMIT:
        raise ValueError("decode Slack Events API payload: invalid event_time")
    return EventsAPIPayload(
        _optional_text(payload, "event_id"),
        event_time,
        _optional_text(payload, "team_id"),
        *(
            _optional_text(event, name)
            for name in ("type", "subtype", "channel", "user", "bot_id", "text", "ts", "thread_ts")
        ),
    )


def _unix_time(seconds: int) -> datetime:
    try:
        return _UNIX_EPOCH + timedelta(seconds=seconds)
    except OverflowError:
        return datetime(1, 1, 1, tzinfo=UTC)


def match_message_event(
    source: MessageSource, payload: EventsAPIPayload
) -> TriggerEvent[MessageEvent] | dict[str, TriggerLogValue]:
    """Return the event when the message matches source, else the ignored-record attributes (IDs only)."""
    is_reply = payload.thread_ts != "" and payload.thread_ts != payload.ts

    def ignored(reason: str) -> dict[str, TriggerLogValue]:
        attributes: dict[str, TriggerLogValue] = {
            "event_id": payload.event_id,
            "channel": payload.channel,
            "reason": reason,
        }
        if reason == "subtype":
            attributes["subtype"] = payload.subtype
        return attributes

    if not payload.event_id:
        return ignored("missing_event_id")
    if payload.type != "message":
        return ignored("not_a_message")
    if payload.subtype:
        return ignored("subtype")
    if payload.bot_id:
        return ignored("bot")
    if not payload.user:
        return ignored("missing_user")
    if payload.channel != source.channel_id:
        return ignored("channel_mismatch")
    if source.requires_thread and not is_reply:
        return ignored("not_a_reply")
    if not source.requires_thread and is_reply:
        return ignored("not_a_root")
    if not message_matches(source.matcher, payload.user, payload.text):
        return ignored("matcher_mismatch")
    return TriggerEvent(
        payload.event_id,
        _unix_time(payload.event_time),
        MessageEvent(
            payload.team_id,
            payload.channel,
            payload.ts,
            payload.thread_ts if is_reply else payload.ts,
            payload.user,
            payload.text,
        ),
    )


def decode_message_deliveries(
    context: TriggerContext, routes: list[MessageRoute], envelope: SocketEnvelope
) -> list[PendingMessageDelivery]:
    """Match one envelope against the message routes; raise ValueError for an undecodable payload."""
    if envelope.type != "events_api" or not envelope.envelope_id.strip():
        identity = routes[0].source.identity
        if identity.is_debug_enabled():
            identity.log_connection(
                context,
                TriggerLogLevel.DEBUG,
                "slack socket mode envelope ignored",
                {"type": envelope.type, "envelope_id": envelope.envelope_id},
            )
        return []
    payload = decode_events_api_payload(envelope)
    deliveries: list[PendingMessageDelivery] = []
    for route in routes:
        matched = match_message_event(route.source, payload)
        if isinstance(matched, dict):
            # Most channel traffic is ignored, so build the record only when DEBUG is enabled.
            if route.source.identity.is_debug_enabled():
                log_trigger_record(
                    route.source.identity.binding_logger,
                    context,
                    TriggerLogLevel.DEBUG,
                    "trigger event ignored",
                    matched,
                )
            continue
        deliveries.append(PendingMessageDelivery(route.source, route.target, matched))
    return deliveries


def decode_button_deliveries(
    context: TriggerContext, routes: list[ButtonRoute], envelope: SocketEnvelope
) -> list[PendingButtonDelivery]:
    """Match one interactive envelope against the button routes; raise ValueError when undecodable."""
    if not envelope.has_payload:
        raise ValueError("decode Slack interactive payload: payload is missing")
    payload = decode_block_actions_payload(envelope.payload)
    deliveries: list[PendingButtonDelivery] = []
    for route in routes:
        events, ignored_reasons = match_button_click_payload(route.source.configuration, payload)
        # Most interactions belong to other bindings, so build the record only when DEBUG is enabled.
        if ignored_reasons and route.source.identity.is_debug_enabled():
            for reason in ignored_reasons:
                log_trigger_record(
                    route.source.identity.binding_logger,
                    context,
                    TriggerLogLevel.DEBUG,
                    "trigger event ignored",
                    {
                        "envelope_id": envelope.envelope_id,
                        "channel": payload.clicked_channel_id,
                        "reason": reason,
                    },
                )
        deliveries.extend(PendingButtonDelivery(route.source, route.target, event) for event in events)
    return deliveries


async def deliver_message_deliveries(
    context: TriggerContext, deliveries: list[PendingMessageDelivery]
) -> None:
    """Deliver inline in route order, so a root reaches Dex before any reply received after it."""
    for delivery in deliveries:
        delivery_context = context.with_log_attributes(
            {
                "channel": delivery.event.payload.channel_id,
                "thread_ts": delivery.event.payload.thread_timestamp,
            }
        )
        await deliver_trigger(
            delivery_context, delivery.target, delivery.event, logger=delivery.source.identity.binding_logger
        )


async def deliver_button_deliveries(context: TriggerContext, deliveries: list[PendingButtonDelivery]) -> None:
    """Deliver one envelope's clicks in route order, with channel, thread, and message log attributes."""
    for delivery in deliveries:
        click = delivery.event.payload
        delivery_context = context.with_log_attributes(
            {
                "channel": click.channel_id,
                "thread_ts": click.thread_timestamp,
                "message_ts": click.message_timestamp,
            }
        )
        await deliver_trigger(
            delivery_context, delivery.target, delivery.event, logger=delivery.source.identity.binding_logger
        )
