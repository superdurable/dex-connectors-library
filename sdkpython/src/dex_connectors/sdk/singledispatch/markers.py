# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Wire-compatible single-dispatch markers and conservative resend decisions."""

from dataclasses import dataclass, field
from enum import IntEnum, StrEnum

from ..types import CallID

MINIMUM_MAX_SENDS = 1
"""Smallest send cap; one send disables bounded resend."""

MAXIMUM_MAX_SENDS = 10
"""Largest send cap accepted by the fence and the Step override."""


class State(StrEnum):
    """Last known provider dispatch state for one Execute attempt."""

    NOT_SENT = "notSent"
    CLAIMED = "claimed"
    CAN_RESEND = "canResend"


@dataclass(frozen=True, slots=True)
class Marker:
    """Non-secret Dex heartbeat checkpoint for a logical call and attempt.

    `send_count` is the number of possibly effective sends this Call ID made
    up to and including `attempt`. Zero means the field is absent, as in
    markers written before bounded resend; decisions then derive a legacy
    upper bound from `attempt` and `state`.
    """

    call_id: CallID = field(metadata={"dex_json_name": "callId"})
    attempt: int
    state: State
    provider_id: str = field(
        default="", metadata={"dex_json_name": "providerId", "dex_json_omit_default": True}
    )
    send_count: int = field(
        default=0, metadata={"dex_json_name": "sendCount", "dex_json_omit_default": True}
    )

    def to_wire(self) -> dict[str, str | int]:
        """Encode Go's camel-case heartbeat marker, omitting empty provider ID and zero count."""
        self.validate()
        result: dict[str, str | int] = {
            "callId": str(self.call_id), "attempt": self.attempt, "state": self.state.value,
        }
        if self.provider_id:
            result["providerId"] = self.provider_id
        if self.send_count:
            result["sendCount"] = self.send_count
        return result

    @classmethod
    def from_wire(cls, value: object) -> "Marker":
        """Decode only declared marker fields and fail closed on malformed data."""
        if not isinstance(value, dict) or not {"callId", "attempt", "state"} <= set(value):
            raise ValueError("invalid single-dispatch checkpoint")
        if set(value) - {"callId", "attempt", "state", "providerId", "sendCount"}:
            raise ValueError("invalid single-dispatch checkpoint")
        if (
            not isinstance(value["callId"], str)
            or not _is_plain_int(value["attempt"])
            or not isinstance(value["state"], str)
            or not isinstance(value.get("providerId", ""), str)
            or not _is_plain_int(value.get("sendCount", 0))
        ):
            raise ValueError("invalid single-dispatch checkpoint")
        try:
            marker = cls(
                CallID(value["callId"]), value["attempt"], State(value["state"]),
                value.get("providerId", ""), value.get("sendCount", 0),
            )
            marker.validate()
        except ValueError as error:
            raise ValueError("invalid single-dispatch checkpoint") from error
        return marker

    def validate(self) -> None:
        """Require a real Call ID, positive attempt, declared state, and non-negative count."""
        self.call_id.validate()
        if (
            not _is_plain_int(self.attempt) or self.attempt < 1
            or not isinstance(self.state, State)
            or not _is_plain_int(self.send_count) or self.send_count < 0
        ):
            raise ValueError("invalid single-dispatch checkpoint")


class DecisionKind(IntEnum):
    """Action permitted before credentials or provider lookups."""

    SEND_ALLOWED = 1
    """Dispatch may send without reading back."""
    READ_BACK_REQUIRED = 2
    """Dispatch is forbidden; read back, or select `uncertain` when unconfirmed."""
    FAIL_CLOSED = 3
    """Dispatch is forbidden because the checkpoint could not be trusted."""
    READ_BACK_THEN_RESEND_ALLOWED = 4
    """Read back when possible; confirmed success selects the happy path.

    A definite not-found or inconclusive read-back permits one Dispatch. A
    connector without read-back may Dispatch directly. Returned only when
    bounded resend is enabled and the send cap has not been reached.
    """


@dataclass(frozen=True, slots=True)
class Decision:
    """Resend decision with prior marker or a safe checkpoint error.

    `prior_send_upper_bound` bounds the possibly effective sends made before
    this attempt. It is zero for a fail-closed decision.
    """

    kind: DecisionKind
    marker: Marker | None = None
    error: str = ""
    prior_send_upper_bound: int = 0


def decide_dispatch(
    call_id: CallID, attempt: int, previous_heartbeat: object | None,
    *, max_sends: int = MINIMUM_MAX_SENDS,
) -> Decision:
    """Permit a send only when proven safe or while the send cap allows another.

    A missing marker on attempt one permits the first send, as does a
    `notSent` or `canResend` marker without provider ID from the immediately
    prior attempt. Any other same-call checkpoint demands read-back; when
    `max_sends` is at least two and fewer than `max_sends` sends may have
    happened, it permits a resend after read-back instead. Foreign or
    malformed checkpoints fail closed. `max_sends` must be within 1..10; the
    default 1 never permits a resend after a possible send.
    """
    call_id.validate()
    if not _is_plain_int(attempt) or attempt < 1:
        raise ValueError("single dispatch requires a positive attempt")
    validate_max_sends(max_sends)
    if previous_heartbeat is None:
        if attempt == 1:
            return Decision(DecisionKind.SEND_ALLOWED)
        return _bounded_read_back(None, attempt - 1, max_sends)
    try:
        if isinstance(previous_heartbeat, Marker):
            # The Dex codec decodes `call_id` as a plain str, so rebuild from fields.
            marker = Marker.from_wire({
                "callId": str(previous_heartbeat.call_id),
                "attempt": previous_heartbeat.attempt,
                "state": str(previous_heartbeat.state),
                "providerId": previous_heartbeat.provider_id,
                "sendCount": previous_heartbeat.send_count,
            })
        else:
            marker = Marker.from_wire(previous_heartbeat)
    except (ValueError, TypeError):
        return Decision(DecisionKind.FAIL_CLOSED, error="invalid single-dispatch checkpoint")
    if marker.call_id != call_id:
        return Decision(DecisionKind.FAIL_CLOSED, marker, "foreign single-dispatch checkpoint")
    if marker.attempt >= attempt:
        # A current or future attempt's marker cannot bound earlier sends.
        return Decision(DecisionKind.READ_BACK_REQUIRED, marker)
    prior_send_upper_bound = _marker_send_count(marker) + (attempt - 1 - marker.attempt)
    if (
        marker.attempt == attempt - 1
        and marker.state in (State.NOT_SENT, State.CAN_RESEND)
        and not marker.provider_id
    ):
        return Decision(DecisionKind.SEND_ALLOWED, marker, prior_send_upper_bound=prior_send_upper_bound)
    return _bounded_read_back(marker, prior_send_upper_bound, max_sends)


def validate_max_sends(max_sends: int) -> None:
    """Reject a send cap outside 1..10, including booleans and non-integers."""
    if not _is_plain_int(max_sends) or not MINIMUM_MAX_SENDS <= max_sends <= MAXIMUM_MAX_SENDS:
        raise ValueError("single-dispatch max sends must be an integer within 1..10")


def _bounded_read_back(
    marker: Marker | None, prior_send_upper_bound: int, max_sends: int
) -> Decision:
    kind = (
        DecisionKind.READ_BACK_THEN_RESEND_ALLOWED
        if prior_send_upper_bound < max_sends and max_sends > MINIMUM_MAX_SENDS
        else DecisionKind.READ_BACK_REQUIRED
    )
    return Decision(kind, marker, prior_send_upper_bound=prior_send_upper_bound)


def _marker_send_count(marker: Marker) -> int:
    if marker.send_count:
        return marker.send_count
    # Legacy markers: each earlier attempt sent at most once.
    return marker.attempt if marker.state == State.CLAIMED else marker.attempt - 1


def _is_plain_int(value: object) -> bool:
    return isinstance(value, int) and not isinstance(value, bool)
