# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Conformance and fail-closed decisions for single-dispatch checkpoints."""

import json
from pathlib import Path

import pytest

from dex_connectors.sdk import CallID
from dex_connectors.sdk.singledispatch import DecisionKind, Marker, State, decide_dispatch


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
