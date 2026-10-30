# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Secret-safe async credential lookup and refresh interfaces."""

from dataclasses import dataclass
from datetime import datetime
from typing import Any, Generic, Protocol, TypeVar

from .types import Call

C = TypeVar("C")
CovariantCredential = TypeVar("CovariantCredential", covariant=True)


class ReauthorizationRequiredError(Exception):
    """Terminal credential state requiring an interactive authorization flow."""

    def __init__(self, cause: Exception | None = None) -> None:
        self.cause = cause
        super().__init__("connector reauthorization is required")

    def __str__(self) -> str:
        """Keep provider details and credentials out of logs."""
        return "connector reauthorization is required"


class SecretString:
    """Credential wrapper that reveals only at a provider request boundary."""

    __slots__ = ("_value",)

    def __init__(self, value: str) -> None:
        self._value = value

    def reveal(self) -> str:
        """Return the secret for immediate provider request construction."""
        return self._value

    def __str__(self) -> str:
        """Return a redacted representation."""
        return "[REDACTED]"

    def __repr__(self) -> str:
        """Return a redacted debug representation."""
        return "SecretString([REDACTED])"

    def __reduce__(self) -> tuple[Any, ...]:
        """Reject accidental serialization into Flow state."""
        raise TypeError("connector secrets cannot be serialized")


class CredentialProvider(Protocol[CovariantCredential]):
    """Resolve current credentials at call time outside durable Flow state."""

    async def resolve(self, call: Call) -> CovariantCredential:
        """Return connector-specific credentials for one provider call."""
        ...


@dataclass(frozen=True, slots=True)
class CredentialRefreshState(Generic[C]):
    """Current credential material, optional expiry, and decision time."""

    credentials: C
    expires_at: datetime | None
    now: datetime


@dataclass(frozen=True, slots=True)
class CredentialRefreshResult(Generic[C]):
    """Complete replacement credential material and required expiry."""

    credentials: C
    expires_at: datetime


class CredentialRefreshDriver(Protocol[C]):
    """Connector-specific refresh decision and provider exchange without storage."""

    def refresh_required(self, state: CredentialRefreshState[C]) -> bool:
        """Decide deterministically whether this credential needs refresh."""
        ...

    async def refresh(self, state: CredentialRefreshState[C]) -> CredentialRefreshResult[C]:
        """Exchange credentials; callers persist the complete result atomically."""
        ...
