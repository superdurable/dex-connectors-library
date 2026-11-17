# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Conformance and fail-closed decisions for single-dispatch checkpoints."""

import asyncio
import json
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any, cast

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
from dex_connectors.sdk.singledispatch.markers import _checkpoint_heartbeat as checkpoint_heartbeat
from dex_connectors.sdk.singledispatch.markers import _markers_from_heartbeat as markers_from_heartbeat

CALL_ID =CallID("5af8fafa-06ff-5b2d-bb13-04693809fabf")
MARKER_CODEC = dex.CodecRegistry().resolve(Marker)


def _checkpoint(*markers: Marker) -> dict[str, object]:
    """Encode markers in the map format a Fence records."""
    return checkpoint_heartbeat({str(marker.call_id): marker for marker in markers})


def _own(heartbeat: object, call_id: CallID = CALL_ID) -> Marker:
    """Return one call's entry from a recorded heartbeat."""
    return markers_from_heartbeat(heartbeat)[str(call_id)]


def test_go_marker_conformance(conformance_dir: Path) -> None:
    """Round-trip every Go heartbeat marker without wire-name or field-order drift."""
    fixture = json.loads((conformance_dir / "single-dispatch-markers.json").read_text())
    markers = fixture["markers"]
    assert any("sendCount" in value for value in markers)
    for value in markers:
        marker = Marker.from_wire(value)
        assert json.dumps(marker.to_wire()) == json.dumps(value)
        persisted = MARKER_CODEC.encode(marker)
        assert persisted.data == json.dumps(value, separators=(",", ":"), sort_keys=True)
        assert MARKER_CODEC.decode(persisted) == marker


def test_send_count_wire_shape() -> None:
    """Zero send count is omitted; invalid counts are rejected as checkpoints."""
    assert "sendCount" not in Marker(CALL_ID, 1, State.NOT_SENT).to_wire()
    legacy_zero = {"callId": str(CALL_ID), "attempt": 2, "state": "claimed", "sendCount": 0}
    assert Marker.from_wire(legacy_zero) == Marker(CALL_ID, 2, State.CLAIMED)
    for send_count in (-1, True, "2", 2.0, None):
        with pytest.raises(ValueError, match="checkpoint"):
            Marker.from_wire({**legacy_zero, "sendCount": send_count})
    with pytest.raises(ValueError, match="checkpoint"):
        Marker(CALL_ID, 2, State.CLAIMED, send_count=-1).to_wire()


def test_only_proven_prior_attempt_can_resend() -> None:
    """Require the immediately prior not-sent or can-resend marker."""
    call_id = CallID("5af8fafa-06ff-5b2d-bb13-04693809fabf")
    assert decide_dispatch(call_id, 1, None).kind == DecisionKind.SEND_ALLOWED
    assert decide_dispatch(call_id, 2, None).kind == DecisionKind.READ_BACK_REQUIRED
    for state in (State.NOT_SENT, State.CAN_RESEND):
        previous = _checkpoint(Marker(call_id, 1, state))
        assert decide_dispatch(call_id, 2, previous).kind == DecisionKind.SEND_ALLOWED
        assert decide_dispatch(call_id, 3, previous).kind == DecisionKind.READ_BACK_REQUIRED
    claimed = _checkpoint(Marker(call_id, 1, State.CLAIMED))
    assert decide_dispatch(call_id, 2, claimed).kind == DecisionKind.READ_BACK_REQUIRED
    accepted = _checkpoint(Marker(call_id, 1, State.CLAIMED, "object-42"))
    assert decide_dispatch(call_id, 2, accepted).kind == DecisionKind.READ_BACK_REQUIRED
    foreign = _checkpoint(Marker(CallID("5af8fafa-06ff-5b2d-bb13-04693809fab0"), 1, State.NOT_SENT))
    assert decide_dispatch(call_id, 2, foreign).kind == DecisionKind.READ_BACK_REQUIRED
    invalid = {"singleDispatchMarkers": {call_id: {"callId": call_id, "attempt": 1, "state": "unknown"}}}
    assert decide_dispatch(call_id, 2, invalid).kind == DecisionKind.READ_BACK_REQUIRED


def test_unreadable_or_legacy_heartbeats_count_as_no_marker() -> None:
    """A legacy single marker, garbage, or a mismatched entry never fails closed."""
    proven = Marker(CALL_ID, 1, State.NOT_SENT)
    for heartbeat in (
        proven, proven.to_wire(), "garbage", [1, 2], 3, {"progress": 1},
        {"singleDispatchMarkers": None},
        {"singleDispatchMarkers": {"5af8fafa-06ff-5b2d-bb13-04693809fab0": proven.to_wire()}},
        {"singleDispatchMarkers": {str(CALL_ID): {**proven.to_wire(), "extra": True}}},
    ):
        first = decide_dispatch(CALL_ID, 1, heartbeat, max_sends=3)
        assert (first.kind, first.prior_send_upper_bound, first.marker) == (
            DecisionKind.SEND_ALLOWED, 0, None
        )
        later = decide_dispatch(CALL_ID, 3, heartbeat, max_sends=3)
        assert (later.kind, later.prior_send_upper_bound, later.error) == (
            DecisionKind.READ_BACK_THEN_RESEND_ALLOWED, 2, ""
        )


def test_malformed_markers_are_rejected() -> None:
    """Reject omitted, unknown, invalid, and contradictory marker fields."""
    for value in (
        {},
        {"callId": "x", "attempt": 0, "state": "claimed"},
        {"callId": "x", "attempt": 1, "state": "claimed", "secret": "bad"},
    ):
        with pytest.raises(ValueError, match="checkpoint"):
            Marker.from_wire(value)


def _legacy_decision_kind(attempt: int, previous: Marker | None) -> DecisionKind:
    """Mirror the 0.9.0 decision rules as an oracle for the default send cap."""
    if previous is None:
        return DecisionKind.SEND_ALLOWED if attempt == 1 else DecisionKind.READ_BACK_REQUIRED
    if (
        previous.attempt == attempt - 1
        and previous.state in (State.NOT_SENT, State.CAN_RESEND)
        and not previous.provider_id
    ):
        return DecisionKind.SEND_ALLOWED
    return DecisionKind.READ_BACK_REQUIRED


def test_default_send_cap_matches_previous_decisions() -> None:
    """With max_sends=1 every same-call marker yields the 0.9.0 decision."""
    previous_markers: list[Marker | None] = [None]
    for marker_attempt in range(1, 6):
        for state in State:
            for provider_id in ("", "object-42"):
                maximum_count = marker_attempt if state == State.CLAIMED else marker_attempt - 1
                for send_count in sorted({0, min(1, maximum_count), maximum_count}):
                    previous_markers.append(
                        Marker(CALL_ID, marker_attempt, state, provider_id, send_count)
                    )
    for attempt in range(1, 8):
        for previous in previous_markers:
            wire = _checkpoint(previous) if previous is not None else None
            for max_sends in ({}, {"max_sends": 1}):
                decision = decide_dispatch(CALL_ID, attempt, wire, **max_sends)
                assert decision.kind == _legacy_decision_kind(attempt, previous)
                assert decision.kind != DecisionKind.READ_BACK_THEN_RESEND_ALLOWED


@pytest.mark.parametrize(
    ("attempt", "previous", "max_sends", "expected_kind", "expected_bound"),
    [
        # No marker: attempt one sends, later attempts may each have sent once.
        (1, None, 3, DecisionKind.SEND_ALLOWED, 0),
        (2, None, 3, DecisionKind.READ_BACK_THEN_RESEND_ALLOWED, 1),
        (3, None, 3, DecisionKind.READ_BACK_THEN_RESEND_ALLOWED, 2),
        (4, None, 3, DecisionKind.READ_BACK_REQUIRED, 3),
        (4, None, 4, DecisionKind.READ_BACK_THEN_RESEND_ALLOWED, 3),
        # Marker with sendCount: S = sendCount + (A - 1 - k).
        (3, Marker(CALL_ID, 2, State.CLAIMED, send_count=1), 3,
         DecisionKind.READ_BACK_THEN_RESEND_ALLOWED, 1),
        (4, Marker(CALL_ID, 2, State.CLAIMED, send_count=1), 3,
         DecisionKind.READ_BACK_THEN_RESEND_ALLOWED, 2),
        (5, Marker(CALL_ID, 2, State.CLAIMED, send_count=1), 3,
         DecisionKind.READ_BACK_REQUIRED, 3),
        (6, Marker(CALL_ID, 5, State.CLAIMED, send_count=2), 3,
         DecisionKind.READ_BACK_THEN_RESEND_ALLOWED, 2),
        (6, Marker(CALL_ID, 5, State.CLAIMED, send_count=3), 3,
         DecisionKind.READ_BACK_REQUIRED, 3),
        (7, Marker(CALL_ID, 6, State.NOT_SENT, send_count=2), 3,
         DecisionKind.SEND_ALLOWED, 2),
        (8, Marker(CALL_ID, 6, State.NOT_SENT, send_count=2), 3,
         DecisionKind.READ_BACK_REQUIRED, 3),
        (8, Marker(CALL_ID, 7, State.CAN_RESEND, send_count=2), 3,
         DecisionKind.SEND_ALLOWED, 2),
        (9, Marker(CALL_ID, 7, State.CAN_RESEND, send_count=2), 4,
         DecisionKind.READ_BACK_THEN_RESEND_ALLOWED, 3),
        # Legacy markers: claimed counts k, notSent and canResend count k - 1.
        (3, Marker(CALL_ID, 2, State.CLAIMED), 3, DecisionKind.READ_BACK_THEN_RESEND_ALLOWED, 2),
        (4, Marker(CALL_ID, 3, State.CLAIMED), 3, DecisionKind.READ_BACK_REQUIRED, 3),
        (3, Marker(CALL_ID, 2, State.NOT_SENT), 3, DecisionKind.SEND_ALLOWED, 1),
        (4, Marker(CALL_ID, 2, State.NOT_SENT), 3, DecisionKind.READ_BACK_THEN_RESEND_ALLOWED, 2),
        (5, Marker(CALL_ID, 3, State.CAN_RESEND), 3, DecisionKind.READ_BACK_REQUIRED, 3),
        # A provider ID always demands read-back; resend only below the cap.
        (2, Marker(CALL_ID, 1, State.CLAIMED, "object-42"), 3,
         DecisionKind.READ_BACK_THEN_RESEND_ALLOWED, 1),
        (9, Marker(CALL_ID, 8, State.CLAIMED, "object-43", 3), 3,
         DecisionKind.READ_BACK_REQUIRED, 3),
        (9, Marker(CALL_ID, 8, State.CLAIMED, "object-43", 3), 4,
         DecisionKind.READ_BACK_THEN_RESEND_ALLOWED, 3),
        (2, Marker(CALL_ID, 1, State.NOT_SENT, "object-42"), 3,
         DecisionKind.READ_BACK_THEN_RESEND_ALLOWED, 0),
        # A marker from this or a later attempt cannot bound earlier sends.
        (2, Marker(CALL_ID, 2, State.NOT_SENT), 3, DecisionKind.READ_BACK_REQUIRED, 0),
        (2, Marker(CALL_ID, 5, State.CLAIMED, send_count=1), 10,
         DecisionKind.READ_BACK_REQUIRED, 0),
    ],
)
def test_bounded_resend_decisions(
    attempt: int, previous: Marker | None, max_sends: int,
    expected_kind: DecisionKind, expected_bound: int,
) -> None:
    """Compute S from each marker form and compare it with the send cap."""
    heartbeat = _checkpoint(previous) if previous is not None else None
    decision = decide_dispatch(CALL_ID, attempt, heartbeat, max_sends=max_sends)
    assert decision.kind == expected_kind
    assert decision.prior_send_upper_bound == expected_bound
    assert decision.marker == previous


def test_foreign_and_unreadable_entries_are_ignored_with_bounded_resend() -> None:
    """Unreadable entries count as no marker, so each earlier attempt counts as one send."""
    foreign = Marker(CallID("5af8fafa-06ff-5b2d-bb13-04693809fab0"), 1, State.NOT_SENT, send_count=0)
    decision = decide_dispatch(CALL_ID, 2, _checkpoint(foreign), max_sends=10)
    assert decision.kind == DecisionKind.READ_BACK_THEN_RESEND_ALLOWED
    assert decision.prior_send_upper_bound == 1
    base = {"callId": str(CALL_ID), "attempt": 1, "state": "claimed"}
    for invalid in (
        {**base, "sendCount": -1},
        {**base, "sendCount": True},
        {**base, "sendCount": "1"},
        {**base, "state": "unknown"},
        {**base, "sendCount": 1, "extra": "x"},
    ):
        heartbeat = {"singleDispatchMarkers": {str(CALL_ID): invalid}}
        unreadable = decide_dispatch(CALL_ID, 2, heartbeat, max_sends=10)
        assert unreadable.kind == DecisionKind.READ_BACK_THEN_RESEND_ALLOWED
        assert unreadable.prior_send_upper_bound == 1


@pytest.mark.parametrize(
    ("attempt", "previous", "max_sends", "expected_kind", "expected_bound"),
    [
        # The same vectors run in sdkgo and sdkts to keep the count validity rule identical.
        (3, Marker(CALL_ID, 2, State.CLAIMED, send_count=2), 3,
         DecisionKind.READ_BACK_THEN_RESEND_ALLOWED, 2),
        (3, Marker(CALL_ID, 2, State.NOT_SENT, send_count=1), 3, DecisionKind.SEND_ALLOWED, 1),
        (3, Marker(CALL_ID, 2, State.CAN_RESEND, send_count=1), 3, DecisionKind.SEND_ALLOWED, 1),
        (4, Marker(CALL_ID, 2, State.NOT_SENT, send_count=1), 3,
         DecisionKind.READ_BACK_THEN_RESEND_ALLOWED, 2),
        (3, Marker(CALL_ID, 2, State.CLAIMED, send_count=3), 3, DecisionKind.FAIL_CLOSED, 0),
        (3, Marker(CALL_ID, 2, State.NOT_SENT, send_count=2), 3, DecisionKind.FAIL_CLOSED, 0),
        (3, Marker(CALL_ID, 2, State.CAN_RESEND, send_count=2), 3, DecisionKind.FAIL_CLOSED, 0),
        (2, Marker(CALL_ID, 1, State.NOT_SENT, send_count=1), 10, DecisionKind.FAIL_CLOSED, 0),
        (2, Marker(CALL_ID, 1, State.CAN_RESEND, send_count=1), 10, DecisionKind.FAIL_CLOSED, 0),
        (3, Marker(CALL_ID, 3, State.CLAIMED, send_count=4), 10, DecisionKind.FAIL_CLOSED, 0),
        (3, Marker(CALL_ID, 2, State.NOT_SENT, send_count=2), 1, DecisionKind.FAIL_CLOSED, 0),
        (3, Marker(CALL_ID, 2, State.CLAIMED, send_count=2), 1, DecisionKind.READ_BACK_REQUIRED, 2),
    ],
)
def test_marker_send_count_range_matches_go(
    attempt: int, previous: Marker, max_sends: int,
    expected_kind: DecisionKind, expected_bound: int,
) -> None:
    """A send count that one send per attempt could not produce fails closed."""
    decision = decide_dispatch(CALL_ID, attempt, _checkpoint(previous), max_sends=max_sends)
    assert decision.kind == expected_kind
    assert decision.prior_send_upper_bound == expected_bound
    assert decision.marker == previous
    expected_error = (
        "invalid single-dispatch checkpoint" if expected_kind == DecisionKind.FAIL_CLOSED else ""
    )
    assert decision.error == expected_error


@pytest.mark.parametrize("max_sends", [0, 11, -1, True, 2.0, "3"])
def test_invalid_send_cap_is_rejected(max_sends: object) -> None:
    """The cap is an integer within 1..10 for decisions and fences."""
    with pytest.raises(ValueError, match="max sends"):
        decide_dispatch(CALL_ID, 1, None, max_sends=cast(int, max_sends))
    with pytest.raises(ValueError, match="max sends"):
        _fence(FakeContext(), max_sends=cast(int, max_sends))


class FakeContext:
    """Minimal public Dex heartbeat and deadline surface for fence state tests."""

    def __init__(self, attempt: int = 1, previous: object | None = None) -> None:
        self.attempt = attempt
        self.deadline = datetime.now(UTC) + timedelta(seconds=10)
        self.previous = previous
        self.heartbeats: list[object] = []
        self.fail_heartbeat = False
        self.fail_read = False
        self.failing_heartbeat_count = 0
        self.repeated = asyncio.Event()

    def has_last_heartbeat_value(self) -> bool:
        """Distinguish a missing checkpoint from a persisted marker."""
        if self.fail_read:
            raise OSError("inert heartbeat read failure")
        return self.previous is not None

    def get_last_heartbeat_value(self, value_type: object) -> object:
        """Decode the previous heartbeat as the Dex codec would; a Marker means its own entry."""
        if isinstance(self.previous, Marker):
            return _checkpoint(self.previous)
        if not isinstance(self.previous, dict):
            raise dex.ValueMappingError("decode", "heartbeat is not a JSON object")
        return json.loads(json.dumps(self.previous))

    async def heartbeat(self, value: object) -> None:
        """Record an inert checkpoint or simulate a failed heartbeat."""
        if self.fail_heartbeat:
            raise OSError("inert heartbeat failure")
        if self.failing_heartbeat_count:
            self.failing_heartbeat_count -= 1
            raise OSError("inert heartbeat failure")
        self.heartbeats.append(value)
        if len(self.heartbeats) >= 2:
            self.repeated.set()


def _fence(
    context: FakeContext, timeout: timedelta = timedelta(seconds=5), *, max_sends: int = 1
) -> Fence:
    call = Call(
        CallID("5af8fafa-06ff-5b2d-bb13-04693809fabf"),
        ConnectionRef("sample", "main"), OperationRef("sample", "createItem"),
        context=cast(dex.AsyncContext, context),
    )
    return Fence(call, timeout, max_sends=max_sends)


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
    assert all(_own(value).state == State.CLAIMED for value in context.heartbeats)
    with pytest.raises(DispatchNotAllowedError):
        await fence.dispatch(send)
    await fence.record_provider_id("provider-object")
    assert _own(context.heartbeats[-1]).provider_id == "provider-object"
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
    """A too-short Dex deadline records notSent; unreadable state reads back; read errors fail closed."""
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
    assert _own(short_context.heartbeats[-1]).state == State.NOT_SENT

    malformed = FakeContext(2, {"callId": "foreign", "attempt": 1, "state": "claimed"})
    read_back = _fence(malformed)
    assert (await read_back.decide()).kind == DecisionKind.READ_BACK_REQUIRED
    with pytest.raises(DispatchNotAllowedError):
        await read_back.dispatch(send)
    assert not sent

    unreadable = FakeContext(2, "not a checkpoint")
    assert (await _fence(unreadable, max_sends=3).decide()).kind == (
        DecisionKind.READ_BACK_THEN_RESEND_ALLOWED
    )

    failing_read = FakeContext(2, _checkpoint(Marker(CALL_ID, 1, State.NOT_SENT)))
    failing_read.fail_read = True
    closed = _fence(failing_read)
    assert (await closed.decide()).kind == DecisionKind.FAIL_CLOSED
    with pytest.raises(DispatchNotAllowedError):
        await closed.dispatch(send)
    assert not sent


CHECKPOINT_CODEC = dex.CodecRegistry().resolve(dict[str, Any])


def _persisted(context: FakeContext) -> Marker:
    """Round-trip the latest checkpoint through the Dex heartbeat codec."""
    frame = context.heartbeats[-1]
    assert isinstance(frame, dict)
    return _own(CHECKPOINT_CODEC.decode(CHECKPOINT_CODEC.encode(frame)))


async def _lose_response(fence: Fence) -> None:
    async def lost_response(deadline: datetime) -> None:
        raise OSError("provider response lost")

    with pytest.raises(OSError):
        await fence.dispatch(lost_response)


@pytest.mark.asyncio
async def test_bounded_fence_resends_until_cap_then_requires_read_back() -> None:
    """Each lost response consumes one send; the cap stops further dispatch."""
    first_context = FakeContext()
    first = _fence(first_context, max_sends=3)
    assert (await first.decide()).kind == DecisionKind.SEND_ALLOWED
    await _lose_response(first)
    assert _persisted(first_context) == Marker(CALL_ID, 1, State.CLAIMED, send_count=1)

    second_context = FakeContext(2, _persisted(first_context))
    second = _fence(second_context, max_sends=3)
    second_decision = await second.decide()
    assert second_decision.kind == DecisionKind.READ_BACK_THEN_RESEND_ALLOWED
    assert second_decision.prior_send_upper_bound == 1
    second._heartbeat_interval = timedelta(milliseconds=10)

    async def slow_lost_response(deadline: datetime) -> None:
        await asyncio.wait_for(second_context.repeated.wait(), 1)
        raise OSError("provider response lost")

    with pytest.raises(OSError):
        await second.dispatch(slow_lost_response)
    assert len(second_context.heartbeats) >= 2
    assert all(
        _own(value) == Marker(CALL_ID, 2, State.CLAIMED, send_count=2)
        for value in second_context.heartbeats
    )

    third_context = FakeContext(3, _persisted(second_context))
    third = _fence(third_context, max_sends=3)
    assert (await third.decide()).kind == DecisionKind.READ_BACK_THEN_RESEND_ALLOWED
    await _lose_response(third)
    assert _persisted(third_context).send_count == 3

    capped_context = FakeContext(4, _persisted(third_context))
    capped = _fence(capped_context, max_sends=3)
    capped_decision = await capped.decide()
    assert capped_decision.kind == DecisionKind.READ_BACK_REQUIRED
    assert capped_decision.prior_send_upper_bound == 3
    with pytest.raises(DispatchNotAllowedError):
        await capped.dispatch(lambda deadline: asyncio.sleep(0))
    with pytest.raises(DispatchNotAllowedError):
        await capped.record_not_sent()
    assert not capped_context.heartbeats

    # A higher cap on a later Step execution still honors the recorded count.
    raised = _fence(FakeContext(4, _persisted(third_context)), max_sends=4)
    assert (await raised.decide()).kind == DecisionKind.READ_BACK_THEN_RESEND_ALLOWED


@pytest.mark.asyncio
async def test_bounded_fence_records_counts_on_follow_up_markers() -> None:
    """notSent and canResend carry the prior count; a provider ID keeps the claim's."""
    claimed = Marker(CALL_ID, 1, State.CLAIMED, send_count=1)

    accepted_context = FakeContext(2, claimed)
    accepted = _fence(accepted_context, max_sends=3)
    await accepted.decide()
    assert await accepted.dispatch(lambda deadline: asyncio.sleep(0, "object-43")) == "object-43"
    await accepted.record_provider_id("object-43")
    assert _persisted(accepted_context) == Marker(CALL_ID, 2, State.CLAIMED, "object-43", 2)
    after_accepted = FakeContext(3, _persisted(accepted_context))
    after_accepted_decision = await _fence(after_accepted, max_sends=3).decide()
    assert after_accepted_decision.kind == DecisionKind.READ_BACK_THEN_RESEND_ALLOWED
    assert after_accepted_decision.marker is not None
    assert after_accepted_decision.marker.provider_id == "object-43"

    rejected_context = FakeContext(2, claimed)
    rejected = _fence(rejected_context, max_sends=3)
    await rejected.decide()
    await _lose_response(rejected)
    await rejected.record_can_resend()
    assert _persisted(rejected_context) == Marker(CALL_ID, 2, State.CAN_RESEND, send_count=1)
    resend_context = FakeContext(3, _persisted(rejected_context))
    resend = _fence(resend_context, max_sends=3)
    resend_decision = await resend.decide()
    assert resend_decision.kind == DecisionKind.SEND_ALLOWED
    assert resend_decision.prior_send_upper_bound == 1
    await resend.dispatch(lambda deadline: asyncio.sleep(0))
    assert _persisted(resend_context).send_count == 2

    pre_send_context = FakeContext(3, Marker(CALL_ID, 2, State.CAN_RESEND, send_count=1))
    pre_send = _fence(pre_send_context, max_sends=3)
    assert (await pre_send.decide()).kind == DecisionKind.SEND_ALLOWED
    await pre_send.record_not_sent()
    assert _persisted(pre_send_context) == Marker(CALL_ID, 3, State.NOT_SENT, send_count=1)
    with pytest.raises(DispatchNotAllowedError):
        await pre_send.dispatch(lambda deadline: asyncio.sleep(0))
    after_pre_send = await _fence(FakeContext(4, _persisted(pre_send_context)), max_sends=3).decide()
    assert after_pre_send.kind == DecisionKind.SEND_ALLOWED
    assert after_pre_send.prior_send_upper_bound == 1


@pytest.mark.asyncio
async def test_bounded_fence_pre_send_failures_keep_prior_count() -> None:
    """After SEND_ALLOWED, short attempts and failed claims record notSent with the prior count."""
    proven = Marker(CALL_ID, 3, State.CAN_RESEND, send_count=2)

    short_context = FakeContext(4, proven)
    short_context.deadline = datetime.now(UTC) - timedelta(seconds=1)
    short = _fence(short_context, max_sends=3)
    assert (await short.decide()).kind == DecisionKind.SEND_ALLOWED
    with pytest.raises(InsufficientAttemptTimeError):
        await short.dispatch(lambda deadline: asyncio.sleep(0))
    assert _persisted(short_context) == Marker(CALL_ID, 4, State.NOT_SENT, send_count=2)

    failed_claim_context = FakeContext(4, proven)
    failed_claim_context.failing_heartbeat_count = 1
    failed_claim = _fence(failed_claim_context, max_sends=3)
    assert (await failed_claim.decide()).kind == DecisionKind.SEND_ALLOWED
    with pytest.raises(CheckpointError):
        await failed_claim.dispatch(lambda deadline: asyncio.sleep(0))
    assert [_own(value) for value in failed_claim_context.heartbeats] == [
        Marker(CALL_ID, 4, State.NOT_SENT, send_count=2)
    ]


@pytest.mark.asyncio
async def test_bounded_fence_short_window_after_unresolved_send_writes_no_marker() -> None:
    """READ_BACK_THEN_RESEND_ALLOWED with a short window retries without notSent."""
    claimed = Marker(CALL_ID, 2, State.CLAIMED, send_count=2)
    context = FakeContext(3, claimed)
    context.deadline = datetime.now(UTC) - timedelta(seconds=1)
    fence = _fence(context, max_sends=3)
    assert (await fence.decide()).kind == DecisionKind.READ_BACK_THEN_RESEND_ALLOWED
    with pytest.raises(InsufficientAttemptTimeError):
        await fence.dispatch(lambda deadline: asyncio.sleep(0))
    assert not context.heartbeats
    with pytest.raises(DispatchNotAllowedError):
        await fence.dispatch(lambda deadline: asyncio.sleep(0))
    # The next attempt still sees the unresolved claim and must read back.
    next_decision = await _fence(FakeContext(4, claimed), max_sends=3).decide()
    assert next_decision.kind == DecisionKind.READ_BACK_REQUIRED


@pytest.mark.asyncio
async def test_bounded_fence_failed_claim_after_unresolved_send_writes_no_marker() -> None:
    """READ_BACK_THEN_RESEND_ALLOWED with a failed claim retries without notSent."""
    claimed = Marker(CALL_ID, 2, State.CLAIMED, send_count=1)
    context = FakeContext(3, claimed)
    context.failing_heartbeat_count = 1
    fence = _fence(context, max_sends=3)
    assert (await fence.decide()).kind == DecisionKind.READ_BACK_THEN_RESEND_ALLOWED
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
    next_decision = await _fence(FakeContext(4, claimed), max_sends=3).decide()
    assert next_decision.kind == DecisionKind.READ_BACK_THEN_RESEND_ALLOWED


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "previous",
    [
        Marker(CALL_ID, 1, State.CLAIMED, "object-42", 1),
        Marker(CALL_ID, 1, State.CLAIMED, send_count=1),
        None,
    ],
)
async def test_bounded_fence_keeps_unresolved_send_for_read_back(previous: Marker | None) -> None:
    """notSent is refused after a possible send, so the next attempt still reads back."""
    context = FakeContext(2, previous)
    fence = _fence(context, max_sends=3)
    decision = await fence.decide()
    assert decision.kind == DecisionKind.READ_BACK_THEN_RESEND_ALLOWED
    with pytest.raises(DispatchNotAllowedError):
        await fence.record_not_sent()
    assert not context.heartbeats
    await fence.dispatch(lambda deadline: asyncio.sleep(0))
    assert _persisted(context) == Marker(CALL_ID, 2, State.CLAIMED, send_count=2)


@pytest.mark.asyncio
async def test_default_fence_records_counts_without_resending() -> None:
    """max_sends=1 keeps 0.9.0 decisions and records markers without sendCount."""
    first_context = FakeContext()
    first = _fence(first_context)
    await first.decide()
    await _lose_response(first)
    assert _own(first_context.heartbeats[-1]) == Marker(CALL_ID, 1, State.CLAIMED)
    assert json.dumps(first_context.heartbeats[-1], separators=(",", ":")) == (
        '{"singleDispatchMarkers":{"5af8fafa-06ff-5b2d-bb13-04693809fabf":'
        '{"callId":"5af8fafa-06ff-5b2d-bb13-04693809fabf","attempt":1,"state":"claimed"}}}'
    )
    await first.record_can_resend()
    assert _own(first_context.heartbeats[-1]) == Marker(CALL_ID, 1, State.CAN_RESEND)

    resend_context = FakeContext(2, _persisted(first_context))
    resend = _fence(resend_context)
    assert (await resend.decide()).kind == DecisionKind.SEND_ALLOWED
    assert await resend.dispatch(lambda deadline: asyncio.sleep(0, "object-42")) == "object-42"
    await resend.record_provider_id("object-42")
    assert _own(resend_context.heartbeats[-1]) == Marker(CALL_ID, 2, State.CLAIMED, "object-42")

    not_sent_context = FakeContext()
    not_sent = _fence(not_sent_context)
    await not_sent.decide()
    await not_sent.record_not_sent()
    assert [_own(value) for value in not_sent_context.heartbeats] == [Marker(CALL_ID, 1, State.NOT_SENT)]

    lost_context = FakeContext()
    lost = _fence(lost_context)
    await lost.decide()
    await _lose_response(lost)
    second_context = FakeContext(2, _persisted(lost_context))
    second = _fence(second_context)
    assert (await second.decide()).kind == DecisionKind.READ_BACK_REQUIRED
    with pytest.raises(DispatchNotAllowedError):
        await second.dispatch(lambda deadline: asyncio.sleep(0))
    assert not second_context.heartbeats

    # A count-free marker still bounds sends when a later Step raises the cap.
    raised = await _fence(FakeContext(2, _persisted(lost_context)), max_sends=2).decide()
    assert raised.kind == DecisionKind.READ_BACK_THEN_RESEND_ALLOWED
    assert raised.prior_send_upper_bound == 1


_DECISION_NAMES = {
    DecisionKind.SEND_ALLOWED: "sendAllowed",
    DecisionKind.READ_BACK_REQUIRED: "readBackRequired",
    DecisionKind.FAIL_CLOSED: "failClosed",
    DecisionKind.READ_BACK_THEN_RESEND_ALLOWED: "readBackThenResendAllowed",
}


def _call_fence(context: FakeContext, call_id: str, max_sends: int) -> Fence:
    call = Call(
        CallID(call_id), ConnectionRef("sample", "main"), OperationRef("sample", "createItem"),
        context=cast(dex.AsyncContext, context),
    )
    return Fence(call, timedelta(minutes=1), max_sends=max_sends)


@pytest.mark.asyncio
async def test_go_checkpoint_scenarios(conformance_dir: Path) -> None:
    """Replay Go's multi-call checkpoint scenarios with identical decisions and heartbeats."""
    fixture = json.loads((conformance_dir / "single-dispatch-markers.json").read_text())
    scenarios = fixture["checkpointScenarios"]
    assert len(scenarios) >= 10
    for scenario in scenarios:
        context = FakeContext(scenario["attempt"], scenario.get("previousHeartbeat"))
        for scenario_call in scenario["calls"]:
            fence = _call_fence(context, scenario_call["callId"], scenario_call["maxSends"])
            decision = await fence.decide()
            assert _DECISION_NAMES[decision.kind] == scenario_call["decision"], scenario["name"]
            assert decision.prior_send_upper_bound == scenario_call["maximumPriorSends"], scenario["name"]
            if scenario_call["dispatch"] and decision.kind in (
                DecisionKind.SEND_ALLOWED, DecisionKind.READ_BACK_THEN_RESEND_ALLOWED,
            ):
                await fence.dispatch(lambda deadline: asyncio.sleep(0))
                if scenario_call.get("providerId"):
                    await fence.record_provider_id(scenario_call["providerId"])
        compact = [json.dumps(frame, separators=(",", ":")) for frame in context.heartbeats]
        expected = [json.dumps(frame, separators=(",", ":")) for frame in scenario["heartbeats"]]
        assert compact == expected, scenario["name"]


GMAIL_CALL_ID = "0b9d3c3e-6a51-5f0e-9a43-7d2f1f7c2b10"


@pytest.mark.asyncio
async def test_two_fenced_calls_in_one_step_both_send_and_survive_retry() -> None:
    """Gmail then Slack in one Step both send; a retry of one keeps the other's claim."""
    context = FakeContext()
    for call_id in (GMAIL_CALL_ID, str(CALL_ID)):
        fence = _call_fence(context, call_id, 1)
        assert (await fence.decide()).kind == DecisionKind.SEND_ALLOWED
        await fence.dispatch(lambda deadline: asyncio.sleep(0))
    assert markers_from_heartbeat(context.heartbeats[-1]) == {
        GMAIL_CALL_ID: Marker(CallID(GMAIL_CALL_ID), 1, State.CLAIMED),
        str(CALL_ID): Marker(CALL_ID, 1, State.CLAIMED),
    }

    previous = _checkpoint(
        Marker(CallID(GMAIL_CALL_ID), 1, State.CLAIMED), Marker(CALL_ID, 1, State.CAN_RESEND),
    )
    retry = FakeContext(2, previous)
    slack = _call_fence(retry, str(CALL_ID), 1)
    assert (await slack.decide()).kind == DecisionKind.SEND_ALLOWED
    await slack.dispatch(lambda deadline: asyncio.sleep(0))
    gmail = await _call_fence(retry, GMAIL_CALL_ID, 1).decide()
    assert gmail.kind == DecisionKind.READ_BACK_REQUIRED
    assert _own(retry.heartbeats[-1], CallID(GMAIL_CALL_ID)) == Marker(
        CallID(GMAIL_CALL_ID), 1, State.CLAIMED
    )


@pytest.mark.asyncio
async def test_concurrent_fences_in_one_step_keep_every_entry() -> None:
    """Concurrent tasks never lose each other's entries."""

    class YieldingContext(FakeContext):
        async def heartbeat(self, value: object) -> None:
            await asyncio.sleep(0)
            await super().heartbeat(value)

    context = YieldingContext()
    call_ids = [f"5af8fafa-06ff-5b2d-bb13-0469380900{index:02d}" for index in range(16)]

    async def run(call_id: str) -> None:
        fence = _call_fence(context, call_id, 3)
        assert (await fence.decide()).kind == DecisionKind.SEND_ALLOWED
        await fence.dispatch(lambda deadline: asyncio.sleep(0))
        await fence.record_provider_id(f"object-{call_id[-2:]}")

    await asyncio.gather(*(run(call_id) for call_id in call_ids))
    markers = markers_from_heartbeat(context.heartbeats[-1])
    assert sorted(markers) == sorted(call_ids)
    for call_id, marker in markers.items():
        assert marker == Marker(CallID(call_id), 1, State.CLAIMED, f"object-{call_id[-2:]}", 1)


@pytest.mark.asyncio
async def test_failed_heartbeat_does_not_commit_entry() -> None:
    """A failed claim leaves no entry for later Fences to carry."""
    context = FakeContext()
    failing = _call_fence(context, str(CALL_ID), 3)
    await failing.decide()
    context.fail_heartbeat = True
    with pytest.raises(CheckpointError):
        await failing.dispatch(lambda deadline: asyncio.sleep(0))
    context.fail_heartbeat = False
    other = _call_fence(context, GMAIL_CALL_ID, 1)
    await other.decide()
    await other.dispatch(lambda deadline: asyncio.sleep(0))
    assert list(markers_from_heartbeat(context.heartbeats[-1])) == [GMAIL_CALL_ID]


def test_step_checkpoint_is_shared_per_attempt_and_released_with_context() -> None:
    """Fences of one attempt share the checkpoint; a later attempt or a dropped context does not."""
    import gc

    from dex_connectors.sdk.singledispatch import checkpoint as checkpoint_module

    context = FakeContext()
    first = _call_fence(context, str(CALL_ID), 1)
    second = _call_fence(context, GMAIL_CALL_ID, 1)
    assert first._checkpoint is second._checkpoint
    context.attempt = 2
    assert _call_fence(context, str(CALL_ID), 1)._checkpoint is not first._checkpoint
    count = len(checkpoint_module._step_checkpoints)
    del first, second, context
    gc.collect()
    assert len(checkpoint_module._step_checkpoints) == count - 1
