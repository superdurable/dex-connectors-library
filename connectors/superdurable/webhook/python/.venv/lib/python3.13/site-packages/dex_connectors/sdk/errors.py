# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Safe provider failure values and retry classification."""

from dataclasses import dataclass
from enum import StrEnum
from math import isfinite


class FailureKind(StrEnum):
    """Provider or local fact; a kind alone does not choose a Step route."""

    VALIDATION = "VALIDATION"
    AUTHENTICATION = "AUTHENTICATION"
    AUTHORIZATION = "AUTHORIZATION"
    NOT_FOUND = "NOT_FOUND"
    CONFLICT = "CONFLICT"
    RATE_LIMIT = "RATE_LIMIT"
    QUOTA_EXHAUSTED = "QUOTA_EXHAUSTED"
    AVAILABILITY = "AVAILABILITY"
    PROVIDER_REJECTION = "PROVIDER_REJECTION"
    TRANSPORT = "TRANSPORT"
    RESPONSE_TOO_LARGE = "RESPONSE_TOO_LARGE"
    PROTOCOL = "PROTOCOL"
    LOCAL_DEFECT = "LOCAL_DEFECT"


@dataclass(frozen=True, slots=True)
class Failure:
    """Secret-safe failure that may be persisted in a Flow result."""

    kind: FailureKind
    provider: str
    operation: str
    message: str

    def validate(self) -> None:
        """Reject unknown kinds and empty identity or message fields."""
        if not isinstance(self.kind, FailureKind):
            raise ValueError("failure kind is invalid")
        if not self.provider or not self.operation or not self.message:
            raise ValueError("failure provider, operation, and message are required")

    def to_wire(self) -> dict[str, str]:
        """Encode the Go-compatible failure object."""
        self.validate()
        return {
            "kind": self.kind.value,
            "provider": self.provider,
            "operation": self.operation,
            "message": self.message,
        }

    @classmethod
    def from_wire(cls, value: object) -> "Failure":
        """Decode a failure while rejecting unknown members."""
        if not isinstance(value, dict) or set(value) != {"kind", "provider", "operation", "message"}:
            raise ValueError("invalid failure object")
        if not all(isinstance(item, str) for item in value.values()):
            raise ValueError("invalid failure fields")
        result = cls(FailureKind(value["kind"]), value["provider"], value["operation"], value["message"])
        result.validate()
        return result


class RetryError(Exception):
    """Retryable connector failure; Step factories later map it to Dex retry."""

    def __init__(self, failure: Failure, retry_after_seconds: float = 0) -> None:
        failure.validate()
        if retry_after_seconds < 0 or not isfinite(retry_after_seconds):
            raise ValueError("retry delay must be finite and nonnegative")
        self.failure = failure
        self.retry_after_seconds = retry_after_seconds
        super().__init__(
            f"connector {failure.provider} {failure.operation} retry ({failure.kind}): {failure.message}"
        )

    @property
    def retry_after_whole_seconds(self) -> int:
        """Return the whole-second delay that Dex can serialize, truncating fractions."""
        return int(self.retry_after_seconds)
