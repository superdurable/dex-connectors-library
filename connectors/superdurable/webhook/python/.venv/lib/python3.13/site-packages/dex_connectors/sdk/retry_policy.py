# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Dex Execute retry defaults for connector operations."""

from dataclasses import dataclass, field
from datetime import timedelta


@dataclass(frozen=True, slots=True)
class RetryPolicy:
    """Execute retry schedule; zero attempts means unlimited within total duration."""

    initial_interval: timedelta
    backoff_coefficient: float
    maximum_interval: timedelta
    maximum_attempts: int
    total_duration: timedelta
    _wire_intervals: tuple[int, int, int] | None = field(default=None, repr=False, compare=False)

    def validate(self) -> None:
        """Reject invalid retry intervals and attempt bounds."""
        if (
            self.initial_interval < timedelta(0)
            or self.maximum_interval < timedelta(0)
            or self.total_duration < timedelta(0)
            or self.backoff_coefficient < 0
            or self.maximum_attempts < 0
        ):
            raise ValueError("invalid connector retry policy")

    def to_wire(self) -> dict[str, int | float]:
        """Encode Dex Go's capitalized nanosecond retry JSON shape."""
        self.validate()
        raw = self._wire_intervals
        initial_ns = int(self.initial_interval.total_seconds() * 1_000_000_000)
        maximum_ns = int(self.maximum_interval.total_seconds() * 1_000_000_000)
        total_ns = int(self.total_duration.total_seconds() * 1_000_000_000)
        if raw is not None and (
            self.initial_interval == timedelta(microseconds=raw[0] / 1000)
            and self.maximum_interval == timedelta(microseconds=raw[1] / 1000)
            and self.total_duration == timedelta(microseconds=raw[2] / 1000)
        ):
            initial_ns, maximum_ns, total_ns = raw
        return {
            "InitialInterval": initial_ns,
            "BackoffCoefficient": self.backoff_coefficient,
            "MaximumInterval": maximum_ns,
            "MaximumAttempts": self.maximum_attempts,
            "TotalDuration": total_ns,
        }

    @classmethod
    def from_wire(cls, value: dict[str, int | float]) -> "RetryPolicy":
        """Decode the Go retry policy's integer nanosecond fields."""
        expected = {
            "InitialInterval",
            "BackoffCoefficient",
            "MaximumInterval",
            "MaximumAttempts",
            "TotalDuration",
        }
        if set(value) != expected:
            raise ValueError("retry policy fields are invalid")
        result = cls(
            timedelta(microseconds=value["InitialInterval"] / 1000),
            float(value["BackoffCoefficient"]),
            timedelta(microseconds=value["MaximumInterval"] / 1000),
            int(value["MaximumAttempts"]),
            timedelta(microseconds=value["TotalDuration"] / 1000),
            (int(value["InitialInterval"]), int(value["MaximumInterval"]), int(value["TotalDuration"])),
        )
        result.validate()
        return result


def default_connector_step_execute_retry_policy() -> RetryPolicy:
    """Return a fresh one-hour retry policy with one-second exponential backoff."""
    return RetryPolicy(timedelta(seconds=1), 2, timedelta(minutes=5), 0, timedelta(hours=1))
