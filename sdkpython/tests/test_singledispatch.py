# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Conformance and fail-closed decisions for single-dispatch checkpoints."""

import asyncio
import json
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import cast

import dex
import pytest

from dex_connectors.sdk import Call, CallID, ConnectionRef, OperationRef
from dex_connectors.sdk.singledispatch import (
    CheckpointError,
    DecisionKind,
    DispatchNotAllowedError,
    Fence,
    InsufficientAttemptTimeError,
    Marker,
    State,
    decide_dispatch,
)


def test_go_marker_conformance(conformance_dir: Path) -> None:
    """Round-trip every Go heartbeat marker without wire-name drift."""
    markers = json.loads((conformance_dir / "single-dispatch-markers.json").read_text())
    for value in markers:
        assert Marker.from_wire(value).to_wire() == value


def test_only_proven_prior_attempt_can_resend() -> None:
    """Require the immediately prior not-sent or can-resend marker."""
    call_id = CallID("5af8fafa-06ff-5b2d-bb13-04693809fabf")
    assert decide_dispatch(call_id, 1, None).kind == DecisionKind.SEND_ALLOWED
    assert decide_dispatch(call_id, 2, None).kind == DecisionKind.READ_BACK_REQUIRED
    for state in (State.NOT_SENT, State.CAN_RESEND):
        previous = Marker(call_id, 1, state).to_wire()
        assert decide_dispatch(call_id, 2, previous).kind == DecisionKind.SEND_ALLOWED
        assert decide_dispatch(call_id, 3, previous).kind == DecisionKind.READ_BACK_REQUIRED
    claimed = Marker(call_id, 1, State.CLAIMED).to_wire()
    assert decide_dispatch(call_id, 2, claimed).kind == DecisionKind.READ_BACK_REQUIRED
    accepted = Marker(call_id, 1, State.CLAIMED, "object-42").to_wire()
    assert decide_dispatch(call_id, 2, accepted).kind == DecisionKind.READ_BACK_REQUIRED
    foreign = Marker(CallID("5af8fafa-06ff-5b2d-bb13-04693809fab0"), 1, State.NOT_SENT).to_wire()
    assert decide_dispatch(call_id, 2, foreign).kind == DecisionKind.FAIL_CLOSED
    invalid = {"callId": call_id, "attempt": 1, "state": "unknown"}
    assert decide_dispatch(call_id, 2, invalid).kind == DecisionKind.FAIL_CLOSED


def test_malformed_markers_are_rejected() -> None:
    """Reject omitted, unknown, invalid, and contradictory marker fields."""
    for value in (
        {},
        {"callId": "x", "attempt": 0, "state": "claimed"},
        {"callId": "x", "attempt": 1, "state": "claimed", "secret": "bad"},
    ):
        with pytest.raises(ValueError, match="checkpoint"):
            Marker.from_wire(value)


class FakeContext:
    """Minimal public Dex heartbeat and deadline surface for fence state tests."""

    def __init__(self, attempt: int = 1, previous: object | None = None) -> None:
        self.attempt = attempt
        self.deadline = datetime.now(UTC) + timedelta(seconds=10)
        self.previous = previous
        self.heartbeats: list[object] = []
        self.fail_heartbeat = False
        self.repeated = asyncio.Event()

    def has_last_heartbeat_value(self) -> bool:
        """Distinguish a missing checkpoint from a persisted marker."""
        return self.previous is not None

    def get_last_heartbeat_value(self, value_type: type[Marker]) -> object:
        """Return the previous attempt's decoded public heartbeat value."""
        return self.previous

    async def heartbeat(self, value: object) -> None:
        """Record an inert checkpoint or simulate a failed heartbeat."""
        if self.fail_heartbeat:
            raise OSError("inert heartbeat failure")
        self.heartbeats.append(value)
        if len(self.heartbeats) >= 2:
            self.repeated.set()


def _fence(context: FakeContext, timeout: timedelta = timedelta(seconds=5)) -> Fence:
    call = Call(
        CallID("5af8fafa-06ff-5b2d-bb13-04693809fabf"),
        ConnectionRef("sample", "main"), OperationRef("sample", "createItem"),
        context=cast(dex.AsyncContext, context),
    )
    return Fence(call, timeout)


@pytest.mark.asyncio
async def test_fence_claims_once_and_repeats_heartbeat_while_sending() -> None:
    """A duplicate send is rejected; periodic claims continue during a slow call."""
    context = FakeContext()
    fence = _fence(context)
    assert (await fence.decide()).kind == DecisionKind.SEND_ALLOWED
    fence._heartbeat_interval = timedelta(milliseconds=10)
    sends = 0

    async def send(deadline: datetime) -> str:
        nonlocal sends
        sends += 1
        assert deadline == fence.write_deadline
        await asyncio.wait_for(context.repeated.wait(), 1)
        return "accepted"

    assert await fence.dispatch(send) == "accepted"
    assert sends == 1
    assert all(isinstance(value, Marker) and value.state == State.CLAIMED for value in context.heartbeats)
    with pytest.raises(DispatchNotAllowedError):
        await fence.dispatch(send)
    await fence.record_provider_id("provider-object")
    assert isinstance(context.heartbeats[-1], Marker)
    assert context.heartbeats[-1].provider_id == "provider-object"
    assert decide_dispatch(
        CallID("5af8fafa-06ff-5b2d-bb13-04693809fabf"), 2, context.heartbeats[-1]
    ).kind == DecisionKind.READ_BACK_REQUIRED


@pytest.mark.asyncio
async def test_fence_pre_send_and_lost_response_decisions() -> None:
    """Only a proven not-sent or can-resend marker permits another attempt."""
    not_sent_context = FakeContext()
    not_sent = _fence(not_sent_context)
    await not_sent.decide()
    await not_sent.record_not_sent()
    with pytest.raises(DispatchNotAllowedError):
        await not_sent.dispatch(lambda deadline: asyncio.sleep(0))
    retry_context = FakeContext(2, not_sent_context.heartbeats[-1])
    assert (await _fence(retry_context).decide()).kind == DecisionKind.SEND_ALLOWED

    lost_context = FakeContext()
    lost = _fence(lost_context)
    await lost.decide()

    async def lost_response(deadline: datetime) -> None:
        raise OSError("provider response lost")

    with pytest.raises(OSError):
        await lost.dispatch(lost_response)
    assert (await _fence(FakeContext(2, lost_context.heartbeats[-1])).decide()).kind == (
        DecisionKind.READ_BACK_REQUIRED
    )
    await lost.record_can_resend()
    assert (await _fence(FakeContext(2, lost_context.heartbeats[-1])).decide()).kind == (
        DecisionKind.SEND_ALLOWED
    )


@pytest.mark.asyncio
async def test_fence_failed_claim_never_dispatches_and_deadline_limits_window() -> None:
    """A failed claim blocks provider I/O, and Dex's shorter deadline bounds writes."""
    context = FakeContext()
    context.deadline = datetime.now(UTC) + timedelta(seconds=2)
    fence = _fence(context, timedelta(seconds=30))
    assert fence.write_deadline < context.deadline
    await fence.decide()
    context.fail_heartbeat = True
    sent = False

    async def send(deadline: datetime) -> None:
        nonlocal sent
        sent = True

    with pytest.raises(CheckpointError):
        await fence.dispatch(send)
    assert not sent
    assert not context.heartbeats
    with pytest.raises(DispatchNotAllowedError):
        await fence.dispatch(send)


@pytest.mark.asyncio
async def test_fence_short_attempt_and_untrusted_checkpoint_fail_before_send() -> None:
    """A too-short Dex deadline records notSent; malformed persisted state fails closed."""
    short_context = FakeContext()
    short_context.deadline = datetime.now(UTC) - timedelta(seconds=1)
    short = _fence(short_context)
    await short.decide()
    sent = False

    async def send(deadline: datetime) -> None:
        nonlocal sent
        sent = True

    with pytest.raises(InsufficientAttemptTimeError):
        await short.dispatch(send)
    assert not sent
    assert isinstance(short_context.heartbeats[-1], Marker)
    assert short_context.heartbeats[-1].state == State.NOT_SENT

    malformed = FakeContext(2, {"callId": "foreign", "attempt": 1, "state": "claimed"})
    closed = _fence(malformed)
    assert (await closed.decide()).kind == DecisionKind.FAIL_CLOSED
    with pytest.raises(DispatchNotAllowedError):
        await closed.dispatch(send)
    assert not sent
