# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Poll schedules: interval, jitter, backoff, and the clock and wait the runner uses."""

import asyncio
import random
from dataclasses import dataclass, replace
from datetime import UTC, datetime, timedelta
from typing import Final

from ..trigger_delivery import TriggerCancellation

_DEFAULT_JITTER_FRACTION: Final = 0.1
_DEFAULT_MAXIMUM_BACKOFF: Final = timedelta(minutes=15)
_DEFAULT_MAXIMUM_PAGES_PER_POLL: Final = 10
_UNIX_EPOCH: Final = datetime(1970, 1, 1, tzinfo=UTC)


@dataclass(frozen=True, slots=True)
class PollSchedule:
    """How often a runner polls and how it backs off."""

    interval: timedelta
    """Wait between the end of one poll of a poller and the start of its next; at least one second."""
    jitter_fraction: float | None = None
    """Spreads each wait uniformly within ±interval*jitter_fraction and delays each poller's first poll by
    a uniform [0, interval*jitter_fraction]. None means 0.1, zero disables jitter, and the maximum is 0.5.
    """
    maximum_backoff: timedelta = timedelta(0)
    """Caps the wait after consecutive failures and is the retry period of a configuration fault.

    Zero means 15 minutes, or the interval when that is longer; otherwise at least the interval.
    """
    maximum_pages_per_poll: int = 0
    """Pages one poll reads before yielding to the other pollers; zero means 10.

    A poller with more pages polls again right after them.
    """


def _resolve_poll_schedule(schedule: PollSchedule) -> PollSchedule:
    if schedule.interval < timedelta(seconds=1):
        raise ValueError("poll interval must be at least one second")
    if schedule.jitter_fraction is None:
        schedule = replace(schedule, jitter_fraction=_DEFAULT_JITTER_FRACTION)
    if not 0 <= (schedule.jitter_fraction or 0.0) <= 0.5:
        raise ValueError("poll jitter fraction must be 0 to 0.5")
    if schedule.maximum_backoff == timedelta(0):
        schedule = replace(schedule, maximum_backoff=max(_DEFAULT_MAXIMUM_BACKOFF, schedule.interval))
    if schedule.maximum_backoff < schedule.interval:
        raise ValueError("poll maximum backoff must be at least the interval")
    if schedule.maximum_pages_per_poll == 0:
        schedule = replace(schedule, maximum_pages_per_poll=_DEFAULT_MAXIMUM_PAGES_PER_POLL)
    if schedule.maximum_pages_per_poll < 1:
        raise ValueError("poll maximum pages per poll must be positive")
    return schedule


def _now() -> datetime:
    return datetime.now(UTC)


def _jitter_source() -> float:
    return random.random()


def _unix_milliseconds(moment: datetime) -> int:
    return (moment - _UNIX_EPOCH) // timedelta(milliseconds=1)


async def _wait_for_poll(cancellation: TriggerCancellation, delay: timedelta, wake: asyncio.Event) -> None:
    """Wait for delay or a wake-up; raise TriggerDeliveryAbortedError as soon as cancellation fires."""
    cancellation.raise_if_cancelled()
    waits = {
        asyncio.ensure_future(asyncio.sleep(max(delay.total_seconds(), 0.0))),
        asyncio.ensure_future(wake.wait()),
        asyncio.ensure_future(cancellation.wait()),
    }
    try:
        await asyncio.wait(waits, return_when=asyncio.FIRST_COMPLETED)
    finally:
        for waiting in waits:
            waiting.cancel()
    wake.clear()
    cancellation.raise_if_cancelled()


def _backoff(schedule: PollSchedule, failures: int) -> timedelta:
    """Return min(interval * 2^(failures-1), maximum backoff) for failures starting at 1."""
    delay = schedule.interval
    completed = 1
    while completed < failures and delay < schedule.maximum_backoff:
        delay *= 2
        completed += 1
    return min(delay, schedule.maximum_backoff)


def _jittered(schedule: PollSchedule, delay: timedelta) -> timedelta:
    """Spread delay uniformly within ±delay*jitter_fraction."""
    fraction = schedule.jitter_fraction or 0.0
    if fraction == 0:
        return delay
    return delay * (1 + fraction * (2 * _jitter_source() - 1))


class _PollTurn:
    """One poll of one poller; its failure delay is computed once so records and the wait agree."""

    def __init__(self, schedule: PollSchedule, failures: int) -> None:
        self._schedule = schedule
        self._failures = failures
        self._delay: timedelta | None = None

    def failure_delay(self) -> timedelta:
        if self._delay is None:
            self._delay = _jittered(self._schedule, _backoff(self._schedule, self._failures + 1))
        return self._delay
