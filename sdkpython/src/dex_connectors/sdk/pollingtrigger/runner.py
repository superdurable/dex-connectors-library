# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The per-connection poll runner and the single-binding TriggerSource adapter."""

import asyncio
import threading
from collections.abc import Sequence
from dataclasses import dataclass
from datetime import datetime, timedelta
from typing import Any, Generic, TypeVar

from ..trigger import TriggerSource
from ..trigger_delivery import (
    TriggerContext,
    TriggerDeliveryAbortedError,
    TriggerEvent,
    TriggerLogger,
    TriggerLogLevel,
    TriggerLogValue,
    TriggerTarget,
    trigger_error_attributes,
)
from ..types import ConnectionRef
from . import schedule
from .fetcher import _REQUEST_LIMITER, RequestLimiter
from .member import _DeliveryMode
from .poller import BindingPollerConfig, Poller, _new_binding_poller, _PollOutcomeKind
from .schedule import PollSchedule

P = TypeVar("P")


@dataclass(frozen=True, slots=True)
class PollRunnerConfig:
    """Configures one runner for every poller of one provider connection."""

    connection: ConnectionRef
    """The provider connection every poller's bindings use."""
    schedule: PollSchedule
    """The interval, jitter, and backoff the pollers share."""
    pollers: Sequence[Poller[Any]]
    """Run in this order whenever several are due; at least one, each belonging to one runner."""
    request_limiter: RequestLimiter | None = None
    """Optional; fetchers wait on it through `wait_for_poll_request`."""
    logger: TriggerLogger | None = None
    """Receives poll and delivery records with binding or feed identity, never payloads, tokens, values,
    or error messages. None uses the standard logging adapter."""


def _milliseconds(delay: timedelta) -> int:
    return delay // timedelta(milliseconds=1)


class PollRunner:
    """Polls every poller of one connection from one task, so polls never overlap.

    Ordering between pollers is deterministic. A rate limit on any poller pauses all of them, because
    provider quotas are per user or per OAuth client. It deliberately is not a TriggerRunner, whose
    identity is one binding; use `new_binding_trigger_source` for that shape.
    """

    def __init__(self, config: PollRunnerConfig) -> None:
        """Validate config and attach its pollers; it reads and writes nothing.

        Raises ValueError for an invalid connection or schedule, an empty poller list, a poller whose
        bindings use another connection, two pollers with one name, a binding served twice, or a poller
        already attached to another runner.
        """
        try:
            config.connection.validate()
        except ValueError as error:
            raise ValueError(f"poll runner connection: {error}") from error
        resolved = schedule._resolve_poll_schedule(config.schedule)
        if not config.pollers:
            raise ValueError("poll runner requires at least one poller")
        names: set[str] = set()
        bindings: set[tuple[str, str]] = set()
        for poller in config.pollers:
            if poller is None:
                raise ValueError("poll runner poller is None")
            if poller.name in names:
                raise ValueError(f"poll runner has two pollers named {poller.name!r}")
            names.add(poller.name)
            for binding in poller.bindings:
                if binding.connection != config.connection:
                    raise ValueError(
                        f"poller {poller.name!r} binding {binding.name!r} uses another connection"
                    )
                identity = (binding.trigger.trigger_name, binding.name)
                if identity in bindings:
                    raise ValueError(
                        f"poll runner serves binding {binding.name!r} of Trigger "
                        f"{binding.trigger.trigger_name!r} twice"
                    )
                bindings.add(identity)
        for poller in config.pollers:
            poller._attach_runner(config.logger, resolved.maximum_pages_per_poll)
        self._schedule = resolved
        self._pollers = tuple(config.pollers)
        self._limiter = config.request_limiter
        self._has_run = False
        self._wake_signal = asyncio.Event()
        self._wake_lock = threading.Lock()
        self._wake_names: set[str] = set()
        self._loop: asyncio.AbstractEventLoop | None = None

    def wake_poller(self, poller_name: str) -> None:
        """Make the poller with that binding or feed name due now.

        It never blocks, coalesces repeated calls, ignores unknown names, and does not shorten a rate-limit
        pause. It may be called from any thread, for example a provider push notification handler.
        """
        with self._wake_lock:
            self._wake_names.add(poller_name)
        loop = self._loop
        try:
            is_loop_thread = loop is None or asyncio.get_running_loop() is loop
        except RuntimeError:
            is_loop_thread = False
        if is_loop_thread or loop is None:
            self._wake_signal.set()
            return
        try:
            loop.call_soon_threadsafe(self._wake_signal.set)
        except RuntimeError:
            # The run ended and its loop closed, so no poll is left to wake.
            return

    async def run(self, context: TriggerContext | None = None) -> None:
        """Check every stored cursor, replay every replaying member target, then poll until cancelled.

        The first poll of a cursor seeds it with the provider's current position, so existing items are
        never delivered. Raises TriggerDeliveryAbortedError once the context is cancelled, RuntimeError
        for a replay failure, and PollCursorDocumentInvalidError before any poll for a stored document it
        can never decode, which it never overwrites. Every other failure is logged and retried: fetch,
        store, and target failures back off per poller, a RateLimitedError pauses every poller, and a
        configuration fault retries every maximum backoff. A runner runs once; a second run raises
        RuntimeError. `asyncio.CancelledError` propagates unchanged.
        """
        if self._has_run:
            raise RuntimeError("poll runner is already running or has run")
        self._has_run = True
        self._loop = asyncio.get_running_loop()
        context = context if context is not None else TriggerContext()
        for poller in self._pollers:
            await poller._check_stored_cursor(context)
        for poller in self._pollers:
            try:
                await poller._replay(context)
            except Exception:
                if context.cancellation.is_cancelled:
                    raise TriggerDeliveryAbortedError() from None
                raise
        limiter_token = _REQUEST_LIMITER.set(self._limiter)
        try:
            await self._do_poll_loop(context)
        finally:
            _REQUEST_LIMITER.reset(limiter_token)

    async def _do_poll_loop(self, context: TriggerContext) -> None:
        resolved = self._schedule
        jitter_fraction = resolved.jitter_fraction or 0.0
        started_at = schedule._now()
        due_at = [
            started_at + resolved.interval * (schedule._jitter_source() * jitter_fraction)
            for _ in self._pollers
        ]
        failures = [0 for _ in self._pollers]
        paused_until: datetime | None = None
        rate_limit_failures = 0
        while True:
            context.cancellation.raise_if_cancelled()
            self._apply_wake_ups(due_at)
            current = schedule._now()
            next_at = min(due_at)
            if paused_until is not None and paused_until > next_at:
                next_at = paused_until
            if next_at > current:
                await schedule._wait_for_poll(context.cancellation, next_at - current, self._wake_signal)
                continue
            for index, poller in enumerate(self._pollers):
                if due_at[index] > current:
                    continue
                turn = schedule._PollTurn(resolved, failures[index])
                outcome = await poller._poll(context, turn)
                finished_at = schedule._now()
                error_attributes: dict[str, TriggerLogValue] = (
                    trigger_error_attributes(outcome.error) if outcome.error is not None else {}
                )
                if outcome.kind is _PollOutcomeKind.CANCELED:
                    raise TriggerDeliveryAbortedError()
                if outcome.kind in (_PollOutcomeKind.SUCCEEDED, _PollOutcomeKind.HAS_MORE_PAGES):
                    failures[index], rate_limit_failures = 0, 0
                    due_at[index] = finished_at
                    if outcome.kind is _PollOutcomeKind.SUCCEEDED:
                        due_at[index] = finished_at + schedule._jittered(resolved, resolved.interval)
                elif outcome.kind is _PollOutcomeKind.FAILED:
                    delay = turn.failure_delay()
                    failures[index] += 1
                    due_at[index] = finished_at + delay
                    if outcome.message:
                        poller._log_record(
                            context,
                            TriggerLogLevel.WARN,
                            outcome.message,
                            {
                                "attempt": failures[index],
                                "delay_ms": _milliseconds(delay),
                                **error_attributes,
                            },
                        )
                elif outcome.kind is _PollOutcomeKind.CONFIGURATION_FAULT:
                    due_at[index] = finished_at + resolved.maximum_backoff
                    poller._log_record(
                        context,
                        TriggerLogLevel.ERROR,
                        outcome.message,
                        {"delay_ms": _milliseconds(resolved.maximum_backoff), **error_attributes},
                    )
                else:
                    rate_limit_failures += 1
                    delay = max(outcome.retry_after, resolved.interval)
                    if outcome.retry_after == timedelta(0):
                        delay = schedule._jittered(resolved, schedule._backoff(resolved, rate_limit_failures))
                    paused_until = finished_at + delay
                    due_at[index] = paused_until
                    poller._log_record(
                        context,
                        TriggerLogLevel.WARN,
                        "poll rate limited; pausing connection",
                        {"delay_ms": _milliseconds(delay), "pollers": len(self._pollers), **error_attributes},
                    )
                    break

    def _apply_wake_ups(self, due_at: list[datetime]) -> None:
        """Make the pollers named by `wake_poller` due now."""
        with self._wake_lock:
            if not self._wake_names:
                return
            names, self._wake_names = self._wake_names, set()
        current = schedule._now()
        for index, poller in enumerate(self._pollers):
            if poller.name in names and due_at[index] > current:
                due_at[index] = current


class _BindingTriggerSource(Generic[P]):
    def __init__(self, config: BindingPollerConfig[P], poll_schedule: PollSchedule) -> None:
        self._config = config
        self._schedule = poll_schedule

    async def run(self, context: TriggerContext, target: TriggerTarget[P]) -> None:
        if target is None:
            raise ValueError("poll Trigger source target is required")
        poller = _new_binding_poller(
            self._config, target, _DeliveryMode.RECORD_AND_DELIVER_BEFORE_COMMIT, False
        )
        runner = PollRunner(PollRunnerConfig(self._config.binding.connection, self._schedule, [poller]))
        await runner.run(context)


class _RejectingTriggerTarget:
    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[Any]) -> None:
        raise RuntimeError("poll Trigger source target is not configured")


def new_binding_trigger_source(
    config: BindingPollerConfig[P], poll_schedule: PollSchedule
) -> TriggerSource[P]:
    """Run a single-binding poll loop as a TriggerSource, so a per-Trigger factory keeps `new_trigger`.

    The target that `new_trigger` passes to `run` replaces `config.target`, which must be None. Because
    that target hides whether events are durable, the source records each event with
    `prepare_trigger_delivery` and also consumes it before the cursor commits. `new_trigger` replays a
    durable target before `run`, so the source does not replay it again. Each `run` builds a fresh runner
    and raises as `PollRunner.run` does. Raises ValueError for invalid wiring or schedule.
    """
    if config.target is not None:
        raise ValueError("poll Trigger source target must be None; new_trigger supplies it")
    _new_binding_poller(
        config, _RejectingTriggerTarget(), _DeliveryMode.RECORD_AND_DELIVER_BEFORE_COMMIT, False
    )
    schedule._resolve_poll_schedule(poll_schedule)
    return _BindingTriggerSource(config, poll_schedule)
