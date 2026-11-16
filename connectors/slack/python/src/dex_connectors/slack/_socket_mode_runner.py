# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Receive Slack Socket Mode envelopes and deliver them to the message and button routes of one connection."""

from __future__ import annotations

import asyncio
import time
from collections import deque
from collections.abc import Awaitable
from dataclasses import dataclass, field
from datetime import timedelta
from typing import Final, TypeVar
from uuid import uuid4

from dex_connectors.sdk import (
    CallID,
    TriggerCancellation,
    TriggerContext,
    TriggerDeliveryAbortedError,
    TriggerDeliveryReplayer,
    TriggerLogLevel,
    has_durable_trigger_inbox,
    prepare_trigger_delivery,
)

from ._trigger_sources import (
    BindingIdentity,
    ButtonRoute,
    MessageRoute,
    PendingButtonDelivery,
    PendingMessageDelivery,
    SocketEnvelope,
    decode_button_deliveries,
    decode_message_deliveries,
    decode_socket_envelope,
    deliver_button_deliveries,
    deliver_message_deliveries,
)
from .client import slack_code
from .socket_mode import SlackSocketConnection, SocketModeConnectionError, redact_socket_url

ResultT = TypeVar("ResultT")

# Slack's interactive acknowledgement deadline; later, the clicker sees an error.
_SLACK_INTERACTIVE_ACKNOWLEDGEMENT_LIMIT_SECONDS: Final = 3.0


@dataclass(slots=True)
class SocketModeRoutes:
    """Routes that share one socket, because Slack sends each envelope to only one app connection."""

    messages: list[MessageRoute] = field(default_factory=list)
    buttons: list[ButtonRoute] = field(default_factory=list)

    @property
    def identity(self) -> BindingIdentity:
        """Return the first route's identity, which names the shared connection."""
        return self.messages[0].source.identity if self.messages else self.buttons[0].source.identity

    def has_durable_inboxes(self) -> bool:
        """Report whether every target records and replays events, so queued deliveries lose none."""
        return all(has_durable_trigger_inbox(route.target) for route in self.messages) and all(
            has_durable_trigger_inbox(route.target) for route in self.buttons
        )


@dataclass(frozen=True, slots=True)
class _Deliveries:
    """One envelope's matched deliveries; at most one list is non-empty."""

    messages: list[PendingMessageDelivery] = field(default_factory=list)
    buttons: list[PendingButtonDelivery] = field(default_factory=list)

    def is_empty(self) -> bool:
        return not self.messages and not self.buttons


class SocketModeDisconnect(Exception):
    """Slack asked the runner to close its connection and open a new one."""

    def __init__(self, reason: str) -> None:
        super().__init__(f"Slack requested a Socket Mode reconnect: {reason}")
        self.reason = reason


@dataclass(slots=True)
class _ConnectionHealth:
    is_healthy: bool = False


async def until_cancelled(awaitable: Awaitable[ResultT], cancellation: TriggerCancellation) -> ResultT:
    """Await awaitable, or cancel it and raise TriggerDeliveryAbortedError once cancellation fires."""
    cancellation.raise_if_cancelled()
    task = asyncio.ensure_future(awaitable)
    waiter = asyncio.ensure_future(cancellation.wait())
    try:
        await asyncio.wait({task, waiter}, return_when=asyncio.FIRST_COMPLETED)
    except BaseException:
        task.cancel()
        raise
    finally:
        waiter.cancel()
    if task.done():
        return task.result()
    task.cancel()
    try:
        await task
    except BaseException:  # noqa: BLE001
        pass  # The abandoned work's outcome no longer matters once delivery was aborted.
    raise TriggerDeliveryAbortedError()


async def wait_for_delay(cancellation: TriggerCancellation, delay: timedelta) -> None:
    """Sleep for delay, raising TriggerDeliveryAbortedError as soon as cancellation fires."""
    await until_cancelled(asyncio.sleep(delay.total_seconds()), cancellation)


async def replay_routes(context: TriggerContext, targets: list[object]) -> None:
    """Deliver every target's pending durable inbox events in route order."""
    for target in targets:
        if not isinstance(target, TriggerDeliveryReplayer):
            continue
        try:
            await target.replay_trigger_deliveries(context)
        except TriggerDeliveryAbortedError:
            raise
        except Exception as error:
            raise RuntimeError(f"replay Slack Trigger deliveries: {error}") from error


async def replay_socket_mode_routes(context: TriggerContext, routes: SocketModeRoutes) -> None:
    """Replay roots and replies first, then button clicks."""
    await replay_routes(context, [route.target for route in routes.messages])
    await replay_routes(context, [route.target for route in routes.buttons])


async def run_socket_mode_routes(context: TriggerContext, routes: SocketModeRoutes) -> None:
    """Serve Socket Mode connections until the context is cancelled, reconnecting with backoff."""
    identity = routes.identity
    is_reconnecting = False
    failures = 0
    while True:
        context.cancellation.raise_if_cancelled()
        is_healthy, error = await _run_connection(context, routes, is_reconnecting)
        is_reconnecting = True
        context.cancellation.raise_if_cancelled()
        if isinstance(error, TriggerDeliveryAbortedError):
            raise error
        if is_healthy:
            failures = 0
        delay = identity.options.timings.reconnect_delay
        if isinstance(error, SocketModeDisconnect):
            # Slack refreshes connections routinely, so a requested reconnect is not a failure.
            identity.log_connection(
                context,
                TriggerLogLevel.INFO,
                "slack socket mode disconnected; reconnecting",
                {"reason": error.reason, "delay_ms": delay // timedelta(milliseconds=1)},
            )
        else:
            failures += 1
            delay = identity.reconnect_backoff(failures)
            identity.log_connection(
                context,
                TriggerLogLevel.WARN,
                "slack socket mode connection failed; reconnecting",
                {"attempt": failures, "delay_ms": delay // timedelta(milliseconds=1), "error": str(error)},
            )
        await wait_for_delay(context.cancellation, delay)


async def _run_connection(
    context: TriggerContext, routes: SocketModeRoutes, is_reconnecting: bool
) -> tuple[bool, BaseException]:
    """Serve one connection until it fails or Slack asks to reconnect; report whether it received any.

    With durable inboxes, a separate task delivers a bounded queue in arrival order, and the connection's
    end waits for it.
    """
    identity = routes.identity
    connection_cancellation = TriggerCancellation(context.cancellation)
    reader_context = TriggerContext(connection_cancellation).with_log_attributes(context.log_attributes)
    health = _ConnectionHealth()
    socket: SlackSocketConnection | None = None
    try:
        # A connection can end after an event was persisted but before it was delivered. Deliver it before
        # reading newer envelopes, so a reply cannot overtake its root.
        if is_reconnecting:
            await replay_socket_mode_routes(context, routes)
        call = identity.credential_call(CallID(str(uuid4())))
        try:
            credentials = await until_cancelled(
                identity.client.resolve_trigger_credentials(call), context.cancellation
            )
        except TriggerDeliveryAbortedError:
            raise
        except Exception:  # noqa: BLE001
            # A credential provider's error may quote what it read, so only its absence is reported.
            return False, ValueError("Slack Socket Mode credentials are unavailable")
        app_token = credentials.app_token.reveal() if credentials.app_token is not None else ""
        if not app_token:
            return False, ValueError(
                "Slack Socket Mode credentials are unavailable: credential app_token is required"
            )
        socket_url = await until_cancelled(
            identity.client.open_socket_mode_connection(app_token), connection_cancellation
        )
        try:
            socket = await until_cancelled(identity.socket_dialer()(socket_url), connection_cancellation)
        except TriggerDeliveryAbortedError:
            raise
        except Exception as error:  # noqa: BLE001
            return False, SocketModeConnectionError(redact_socket_url(str(error), socket_url))
        identity.log_connection(
            context, TriggerLogLevel.INFO, "slack socket mode connected", {"reconnect": is_reconnecting}
        )
        if not routes.has_durable_inboxes():
            # Only one acknowledged event can then await delivery, so a stopped process loses at most one.
            return health.is_healthy, await _read_envelopes(reader_context, socket, routes, None, health)
        queue = _DeliveryQueue(identity.options.delivery_queue_capacity)
        delivering = asyncio.ensure_future(_deliver_queued(context, queue, connection_cancellation))
        try:
            read_error = await _read_envelopes(reader_context, socket, routes, queue, health)
        finally:
            queue.close()
        delivery_error = await delivering
        return health.is_healthy, delivery_error if delivery_error is not None else read_error
    except Exception as error:  # noqa: BLE001
        return health.is_healthy, error
    finally:
        if socket is not None:
            await socket.close()


async def _read_envelopes(
    context: TriggerContext,
    socket: SlackSocketConnection,
    routes: SocketModeRoutes,
    queue: _DeliveryQueue | None,
    health: _ConnectionHealth,
) -> BaseException:
    """Record and acknowledge envelopes until the connection ends; queue deliveries, or deliver inline."""
    identity = routes.identity
    while True:
        try:
            message = await until_cancelled(
                socket.read_message(identity.options.timings.read_timeout), context.cancellation
            )
            envelope = decode_socket_envelope(message)
        except Exception as error:  # noqa: BLE001
            return error
        received_at = time.monotonic()
        health.is_healthy = True
        if envelope.type == "disconnect":
            return SocketModeDisconnect(slack_code(envelope.reason))
        try:
            deliveries = _decode_deliveries(context, routes, envelope)
        except ValueError as error:
            identity.log_connection(
                context,
                TriggerLogLevel.WARN,
                "trigger event skipped: undecodable",
                {"envelope_id": envelope.envelope_id, "error": str(error)},
            )
            try:
                await _acknowledge(socket, envelope.envelope_id)
            except Exception as acknowledge_error:  # noqa: BLE001
                return acknowledge_error
            continue
        try:
            for message_delivery in deliveries.messages:
                await prepare_trigger_delivery(context, message_delivery.target, message_delivery.event)
            for button_delivery in deliveries.buttons:
                await prepare_trigger_delivery(context, button_delivery.target, button_delivery.event)
            await _acknowledge(socket, envelope.envelope_id)
        except Exception as error:  # noqa: BLE001
            return error
        if deliveries.is_empty():
            continue
        elapsed = time.monotonic() - received_at
        if envelope.type == "interactive" and elapsed > _SLACK_INTERACTIVE_ACKNOWLEDGEMENT_LIMIT_SECONDS:
            # Slack has already shown the clicker an error, but the recorded click is still delivered.
            identity.log_connection(
                context,
                TriggerLogLevel.WARN,
                "slack interactive envelope acknowledged late",
                {"envelope_id": envelope.envelope_id, "elapsed_ms": int(elapsed * 1000)},
            )
        try:
            if queue is None:
                await _deliver(context, deliveries)
            else:
                # A full queue pauses reading; inboxes hold what was acknowledged, and Slack resends the rest.
                await queue.push(deliveries, context.cancellation)
        except Exception as error:  # noqa: BLE001
            return error


def _decode_deliveries(
    context: TriggerContext, routes: SocketModeRoutes, envelope: SocketEnvelope
) -> _Deliveries:
    """Send interactive envelopes to button routes, when any, and every other envelope to message routes."""
    if envelope.type == "interactive" and envelope.envelope_id.strip() and routes.buttons:
        return _Deliveries(buttons=decode_button_deliveries(context, routes.buttons, envelope))
    if routes.messages:
        return _Deliveries(messages=decode_message_deliveries(context, routes.messages, envelope))
    identity = routes.identity
    if identity.is_debug_enabled():
        identity.log_connection(
            context,
            TriggerLogLevel.DEBUG,
            "slack socket mode envelope ignored",
            {"type": envelope.type, "envelope_id": envelope.envelope_id},
        )
    return _Deliveries()


async def _deliver(context: TriggerContext, deliveries: _Deliveries) -> None:
    await deliver_message_deliveries(context, deliveries.messages)
    await deliver_button_deliveries(context, deliveries.buttons)


async def _deliver_queued(
    context: TriggerContext, queue: _DeliveryQueue, connection_cancellation: TriggerCancellation
) -> BaseException | None:
    """Deliver queued envelopes in order; after an error, stop the connection and leave the rest pending."""
    delivery_error: BaseException | None = None
    while True:
        deliveries = await queue.next()
        if deliveries is None:
            return delivery_error
        if delivery_error is not None:
            continue
        try:
            await _deliver(context, deliveries)
        except Exception as error:  # noqa: BLE001
            delivery_error = error
            connection_cancellation.cancel()


async def _acknowledge(socket: SlackSocketConnection, envelope_id: str) -> None:
    if not envelope_id:
        return
    try:
        await socket.write_message({"envelope_id": envelope_id})
    except Exception as error:
        raise SocketModeConnectionError(f"acknowledge Slack Socket Mode envelope: {error}") from error


class _DeliveryQueue:
    """Bounded FIFO of one connection's acknowledged deliveries."""

    def __init__(self, capacity: int) -> None:
        self._capacity = capacity
        self._items: deque[_Deliveries] = deque()
        self._is_closed = False
        self._changed = asyncio.Event()

    async def push(self, deliveries: _Deliveries, cancellation: TriggerCancellation) -> None:
        while len(self._items) >= self._capacity:
            self._changed.clear()
            await until_cancelled(self._changed.wait(), cancellation)
        cancellation.raise_if_cancelled()
        self._items.append(deliveries)
        self._changed.set()

    async def next(self) -> _Deliveries | None:
        while not self._items:
            if self._is_closed:
                return None
            self._changed.clear()
            await self._changed.wait()
        item = self._items.popleft()
        self._changed.set()
        return item

    def close(self) -> None:
        self._is_closed = True
        self._changed.set()
