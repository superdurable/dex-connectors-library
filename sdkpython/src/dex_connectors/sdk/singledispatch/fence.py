# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""At-most-once provider dispatch using Dex's public heartbeat and deadline APIs."""

import asyncio
import math
from collections.abc import Awaitable, Callable
from datetime import UTC, datetime, timedelta
from typing import TypeVar

import dex

from ..types import Call
from .markers import Decision, DecisionKind, Marker, State, decide_dispatch

Result = TypeVar("Result")


class DispatchNotAllowedError(RuntimeError):
    """The attempt has no remaining permission to send the provider request."""


class InsufficientAttemptTimeError(RuntimeError):
    """The configured write window cannot fit the minimum request time."""


class CheckpointError(RuntimeError):
    """The claim heartbeat failed before the provider request was sent."""


class Fence:
    """Fence one non-idempotent provider send in a synchronous Execute attempt.

    Construct one Fence per logical Call and attempt. Decide before resolving
    credentials. Dispatch records the claim before calling `send`, repeats it
    every five seconds while the request runs, and never calls `send` twice.
    """

    def __init__(
        self, call: Call, execute_timeout: timedelta,
        *, write_window_fraction: float = 7 / 12,
    ) -> None:
        context = call.context
        if context is None or not call.id or context.attempt < 1 or execute_timeout <= timedelta(0):
            raise ValueError("single dispatch requires a call, positive attempt, and Execute timeout")
        if (
            not math.isfinite(write_window_fraction)
            or write_window_fraction <= 0
            or write_window_fraction > 5 / 6
        ):
            raise ValueError("single-dispatch write window must be within (0, 5/6]")
        started_at = datetime.now(UTC)
        budget = execute_timeout
        if context.deadline is not None:
            budget = min(budget, context.deadline - started_at)
        budget = max(budget, timedelta(0))
        self._call = call
        self._context: dex.AsyncContext = context
        self.write_deadline = started_at + budget * write_window_fraction
        self._minimum_request_time = budget / 10
        self._heartbeat_interval = timedelta(seconds=5)
        self._lock = asyncio.Lock()
        self._decision: Decision | None = None
        self._has_dispatched = False
        self._has_send_finished = False
        self._has_pre_send_retry = False
        self._marker: Marker | None = None

    async def decide(self) -> Decision:
        """Read the prior heartbeat before any credential or provider lookup."""
        async with self._lock:
            if self._decision is not None:
                return self._decision
            try:
                previous: object | None = None
                if self._context.has_last_heartbeat_value():
                    previous = self._context.get_last_heartbeat_value(Marker)
                    if previous is None:
                        raise ValueError("present heartbeat has no checkpoint")
                self._decision = decide_dispatch(self._call.id, self._context.attempt, previous)
            except Exception:
                self._decision = Decision(
                    DecisionKind.FAIL_CLOSED, error="single-dispatch checkpoint could not be read"
                )
            return self._decision

    async def dispatch(self, send: Callable[[datetime], Awaitable[Result]]) -> Result:
        """Checkpoint a claim, call `send` once within the write window, and repeat heartbeats."""
        async with self._lock:
            if (
                self._decision is None or self._decision.kind != DecisionKind.SEND_ALLOWED
                or self._has_dispatched or self._has_pre_send_retry or send is None
            ):
                raise DispatchNotAllowedError("single dispatch is not allowed for this attempt")
            if (
                self._minimum_request_time <= timedelta(0)
                or self.write_deadline - datetime.now(UTC) < self._minimum_request_time
            ):
                self._has_pre_send_retry = True
                await self._record(Marker(self._call.id, self._context.attempt, State.NOT_SENT))
                raise InsufficientAttemptTimeError("insufficient attempt time for single dispatch")
            self._has_dispatched = True
            claim = Marker(self._call.id, self._context.attempt, State.CLAIMED)
            try:
                await self._record(claim)
            except Exception as error:
                self._has_pre_send_retry = True
                try:
                    await self._record(Marker(self._call.id, self._context.attempt, State.NOT_SENT))
                except Exception:
                    pass
                raise CheckpointError("single-dispatch checkpoint could not be recorded") from error
        stop = asyncio.Event()
        repeater = asyncio.create_task(self._repeat_claim(claim, stop))
        try:
            remaining = max((self.write_deadline - datetime.now(UTC)).total_seconds(), 0)
            return await asyncio.wait_for(send(self.write_deadline), remaining)
        finally:
            async with self._lock:
                self._has_send_finished = True
            stop.set()
            await repeater

    async def record_not_sent(self) -> None:
        """Checkpoint a pre-send Retry and forbid Dispatch for this attempt."""
        async with self._lock:
            if (
                self._decision is None or self._decision.kind != DecisionKind.SEND_ALLOWED
                or self._has_dispatched or self._has_pre_send_retry
            ):
                raise DispatchNotAllowedError("single dispatch is not allowed for this attempt")
            self._has_pre_send_retry = True
            await self._record(Marker(self._call.id, self._context.attempt, State.NOT_SENT))

    async def record_can_resend(self) -> None:
        """Checkpoint positive proof that the provider did not process the request."""
        async with self._lock:
            if (
                not self._has_send_finished or self._marker is None
                or self._marker.state != State.CLAIMED or self._marker.provider_id
            ):
                raise DispatchNotAllowedError("single dispatch is not allowed for this attempt")
            await self._record(Marker(self._call.id, self._context.attempt, State.CAN_RESEND))

    async def record_provider_id(self, provider_id: str) -> None:
        """Checkpoint an accepted resource identity for later read-back."""
        async with self._lock:
            if (
                not self._has_send_finished or self._marker is None
                or self._marker.state != State.CLAIMED or not provider_id
            ):
                raise DispatchNotAllowedError("single dispatch is not allowed for this attempt")
            await self._record(
                Marker(self._call.id, self._context.attempt, State.CLAIMED, provider_id)
            )

    async def _record(self, marker: Marker) -> None:
        await self._context.heartbeat(marker)
        self._marker = marker

    async def _repeat_claim(self, claim: Marker, stop: asyncio.Event) -> None:
        while True:
            try:
                await asyncio.wait_for(stop.wait(), self._heartbeat_interval.total_seconds())
                return
            except TimeoutError:
                pass
            async with self._lock:
                if self._marker == claim:
                    try:
                        await self._context.heartbeat(claim)
                    except Exception:
                        pass
