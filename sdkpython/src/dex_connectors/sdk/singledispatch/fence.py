# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""At-most-once provider dispatch using Dex's public heartbeat and deadline APIs."""

import asyncio
import math
from collections.abc import Awaitable, Callable
from datetime import UTC, datetime, timedelta
from typing import Any, TypeVar

import dex

from ..types import Call
from .checkpoint import _step_checkpoint_for
from .markers import (
    MINIMUM_MAX_SENDS,
    Decision,
    DecisionKind,
    Marker,
    State,
    decide_dispatch,
    validate_max_sends,
)

Result = TypeVar("Result")
_DISPATCH_PERMITTING_KINDS = (
    DecisionKind.SEND_ALLOWED, DecisionKind.READ_BACK_THEN_RESEND_ALLOWED,
)


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

    `max_sends` caps the possibly effective sends of one Call ID across all
    attempts. The default 1 never resends after a possible send. A value from
    2 to 10 enables bounded resend: a decision of
    `READ_BACK_THEN_RESEND_ALLOWED` permits Dispatch after the connector's
    read-back is not found or inconclusive, or directly when the connector
    cannot read back. Pass the same cap on every attempt of a Step execution.
    With a cap of 2 or more, every marker records `send_count`; with the
    default 1, markers omit it. A marker without the count still yields a
    safe upper bound under any later cap.

    One Step may run Fences for several Call IDs, also as concurrent tasks.
    Every heartbeat holds one marker per Call ID the Step attempt knows, so
    the Fences do not lose each other's entries. The heartbeat format is
    internal to the connector SDKs and may change; hosts must not parse it.
    """

    def __init__(
        self, call: Call, execute_timeout: timedelta,
        *, write_window_fraction: float = 7 / 12, max_sends: int = MINIMUM_MAX_SENDS,
    ) -> None:
        context = call.context
        if context is None or not call.id or context.attempt < 1 or execute_timeout <= timedelta(0):
            raise ValueError("single dispatch requires a call, positive attempt, and Execute timeout")
        validate_max_sends(max_sends)
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
        self._max_sends = max_sends
        self._context: dex.AsyncContext = context
        self._checkpoint = _step_checkpoint_for(context)
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
        """Read this call's prior checkpoint entry before any credential or provider lookup.

        A heartbeat the Dex codec cannot map counts as no marker; any other
        read error fails closed.
        """
        async with self._lock:
            if self._decision is not None:
                return self._decision
            try:
                previous: object | None = None
                if self._context.has_last_heartbeat_value():
                    try:
                        previous = self._context.get_last_heartbeat_value(dict[str, Any])
                    except dex.ValueMappingError:
                        previous = None
                self._decision = decide_dispatch(
                    self._call.id, self._context.attempt, previous, max_sends=self._max_sends
                )
            except Exception:
                self._decision = Decision(
                    DecisionKind.FAIL_CLOSED, error="single-dispatch checkpoint could not be read"
                )
            return self._decision

    async def dispatch(self, send: Callable[[datetime], Awaitable[Result]]) -> Result:
        """Checkpoint a claim, call `send` once within the write window, and repeat heartbeats.

        Allowed once after `SEND_ALLOWED`, or after `READ_BACK_THEN_RESEND_ALLOWED`
        when read-back did not confirm the write. The claim records one more
        send than the decision's prior send upper bound. A too-short write
        window or failed claim records `notSent` only after `SEND_ALLOWED`.
        """
        async with self._lock:
            if (
                self._decision is None or self._decision.kind not in _DISPATCH_PERMITTING_KINDS
                or self._has_dispatched or self._has_pre_send_retry or send is None
            ):
                raise DispatchNotAllowedError("single dispatch is not allowed for this attempt")
            # notSent after an unresolved earlier send would let the next attempt skip read-back.
            should_record_not_sent = self._decision.kind == DecisionKind.SEND_ALLOWED
            if (
                self._minimum_request_time <= timedelta(0)
                or self.write_deadline - datetime.now(UTC) < self._minimum_request_time
            ):
                self._has_pre_send_retry = True
                if should_record_not_sent:
                    await self._record(self._not_sent_marker())
                raise InsufficientAttemptTimeError("insufficient attempt time for single dispatch")
            self._has_dispatched = True
            claim = self._attempt_marker(State.CLAIMED, send_count=self._prior_send_count() + 1)
            try:
                await self._record(claim)
            except Exception as error:
                self._has_pre_send_retry = True
                if should_record_not_sent:
                    try:
                        await self._record(self._not_sent_marker())
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
        """Checkpoint a pre-send Retry and forbid Dispatch for this attempt.

        Allowed only after `SEND_ALLOWED`. After `READ_BACK_THEN_RESEND_ALLOWED`
        a possible earlier send is unresolved; a `notSent` would let the next
        attempt send without reading back, so return Retry without it.
        The marker carries the prior send upper bound.
        """
        async with self._lock:
            if (
                self._decision is None or self._decision.kind != DecisionKind.SEND_ALLOWED
                or self._has_dispatched or self._has_pre_send_retry
            ):
                raise DispatchNotAllowedError("single dispatch is not allowed for this attempt")
            self._has_pre_send_retry = True
            await self._record(self._not_sent_marker())

    async def record_can_resend(self) -> None:
        """Checkpoint positive proof that the provider did not process the request.

        The marker carries the send count from before this attempt's claim.
        """
        async with self._lock:
            if (
                not self._has_send_finished or self._marker is None
                or self._marker.state != State.CLAIMED or self._marker.provider_id
            ):
                raise DispatchNotAllowedError("single dispatch is not allowed for this attempt")
            await self._record(
                self._attempt_marker(State.CAN_RESEND, send_count=self._prior_send_count())
            )

    async def record_provider_id(self, provider_id: str) -> None:
        """Checkpoint an accepted resource identity for later read-back.

        The marker carries the same send count as this attempt's claim.
        """
        async with self._lock:
            if (
                not self._has_send_finished or self._marker is None
                or self._marker.state != State.CLAIMED or not provider_id
            ):
                raise DispatchNotAllowedError("single dispatch is not allowed for this attempt")
            await self._record(
                self._attempt_marker(
                    State.CLAIMED, provider_id, send_count=self._prior_send_count() + 1
                )
            )

    def _not_sent_marker(self) -> Marker:
        return self._attempt_marker(State.NOT_SENT, send_count=self._prior_send_count())

    def _prior_send_count(self) -> int:
        return self._decision.prior_send_upper_bound if self._decision is not None else 0

    def _attempt_marker(self, state: State, provider_id: str = "", *, send_count: int) -> Marker:
        # A cap of 1 omits the count, as Go does.
        recorded_send_count = send_count if self._max_sends > MINIMUM_MAX_SENDS else 0
        return Marker(
            self._call.id, self._context.attempt, state, provider_id, recorded_send_count
        )

    async def _record(self, marker: Marker) -> None:
        await self._checkpoint.record(self._context, marker)
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
                        await self._checkpoint.record(self._context, claim)
                    except Exception:
                        pass
