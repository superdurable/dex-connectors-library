# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Wire-compatible single-dispatch markers and conservative resend decisions."""

from dataclasses import dataclass
from enum import IntEnum, StrEnum

from ..types import CallID


class State(StrEnum):
    """Last known provider dispatch state for one Execute attempt."""

    NOT_SENT = "notSent"
    CLAIMED = "claimed"
    CAN_RESEND = "canResend"


@dataclass(frozen=True, slots=True)
class Marker:
    """Non-secret Dex heartbeat checkpoint for a logical call and attempt."""

    call_id: CallID
    attempt: int
    state: State
    provider_id: str = ""

    def to_wire(self) -> dict[str, str | int]:
        """Encode Go's camel-case heartbeat marker, omitting empty provider ID."""
        self.validate()
        result: dict[str, str | int] = {
            "callId": str(self.call_id), "attempt": self.attempt, "state": self.state.value,
        }
        if self.provider_id:
            result["providerId"] = self.provider_id
        return result

    @classmethod
    def from_wire(cls, value: object) -> "Marker":
        """Decode only declared marker fields and fail closed on malformed data."""
        if not isinstance(value, dict) or not {"callId", "attempt", "state"} <= set(value):
            raise ValueError("invalid single-dispatch checkpoint")
        if set(value) - {"callId", "attempt", "state", "providerId"}:
            raise ValueError("invalid single-dispatch checkpoint")
        if (
            not isinstance(value["callId"], str)
            or not isinstance(value["attempt"], int)
            or isinstance(value["attempt"], bool)
            or not isinstance(value["state"], str)
            or not isinstance(value.get("providerId", ""), str)
        ):
            raise ValueError("invalid single-dispatch checkpoint")
        try:
            marker = cls(
                CallID(value["callId"]), value["attempt"], State(value["state"]),
                value.get("providerId", ""),
            )
            marker.validate()
        except ValueError as error:
            raise ValueError("invalid single-dispatch checkpoint") from error
        return marker

    def validate(self) -> None:
        """Require a real Call ID, positive attempt, and declared marker state."""
        self.call_id.validate()
        if self.attempt < 1 or not isinstance(self.state, State):
            raise ValueError("invalid single-dispatch checkpoint")


class DecisionKind(IntEnum):
    """Action permitted before credentials or provider lookups."""

    SEND_ALLOWED = 1
    READ_BACK_REQUIRED = 2
    FAIL_CLOSED = 3


@dataclass(frozen=True, slots=True)
class Decision:
    """Resend decision with prior marker or a safe checkpoint error."""

    kind: DecisionKind
    marker: Marker | None = None
    error: str = ""


def decide_dispatch(
    call_id: CallID, attempt: int, previous_heartbeat: object | None
) -> Decision:
    """Permit resend only after the immediately prior attempt proved it safe.

    A missing marker on attempt one permits the first send. Missing or stale
    later checkpoints demand read-back; foreign or malformed ones fail closed.
    """
    call_id.validate()
    if attempt < 1:
        raise ValueError("single dispatch requires a positive attempt")
    if previous_heartbeat is None:
        if attempt == 1:
            return Decision(DecisionKind.SEND_ALLOWED)
        return Decision(DecisionKind.READ_BACK_REQUIRED)
    try:
        marker = Marker.from_wire(previous_heartbeat)
    except ValueError:
        return Decision(DecisionKind.FAIL_CLOSED, error="invalid single-dispatch checkpoint")
    if marker.call_id != call_id:
        return Decision(DecisionKind.FAIL_CLOSED, marker, "foreign single-dispatch checkpoint")
    if (
        marker.attempt == attempt - 1
        and marker.state in (State.NOT_SENT, State.CAN_RESEND)
        and not marker.provider_id
    ):
        return Decision(DecisionKind.SEND_ALLOWED, marker)
    return Decision(DecisionKind.READ_BACK_REQUIRED, marker)
