# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Secret-safe async credential lookup and refresh interfaces."""

from collections.abc import Mapping
from dataclasses import dataclass, field
from datetime import datetime
from typing import Any, Generic, Protocol, TypeVar, cast, runtime_checkable

from .types import Call, ConnectionRef

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


@runtime_checkable
class RefreshingCredentialProvider(Protocol[C]):
    """Resolve, refresh, and persist credentials under a connection lock.

    Implementations reload current material after acquiring the lock, then
    atomically persist complete replacement material before returning it.
    """

    async def resolve_with_refresh(self, call: Call, driver: "CredentialRefreshDriver[C]") -> C:
        """Return a usable credential, refreshing it when required."""
        ...


@runtime_checkable
class RejectedCredentialRefreshingProvider(Protocol[C]):
    """Force a coordinated refresh after one provider authentication rejection."""

    async def resolve_after_rejection(self, call: Call, driver: "CredentialRefreshDriver[C]") -> C:
        """Reload and replace rejected material under the connection lock."""
        ...


@dataclass(frozen=True, slots=True)
class CredentialRefreshState(Generic[C]):
    """Current credential material, optional expiry, and decision time."""

    credentials: C = field(repr=False)
    expires_at: datetime | None
    now: datetime


@dataclass(frozen=True, slots=True)
class CredentialRefreshResult(Generic[C]):
    """Complete replacement credential material and required expiry."""

    credentials: C = field(repr=False)
    expires_at: datetime


class CredentialRefreshDriver(Protocol[C]):
    """Connector-specific refresh decision and provider exchange without storage."""

    def refresh_required(self, state: CredentialRefreshState[C]) -> bool:
        """Decide deterministically whether this credential needs refresh."""
        ...

    async def refresh(self, state: CredentialRefreshState[C]) -> CredentialRefreshResult[C]:
        """Exchange credentials; callers persist the complete result atomically."""
        ...


async def resolve_credential(
    provider: CredentialProvider[C], call: Call, driver: CredentialRefreshDriver[C]
) -> C:
    """Resolve current credentials through coordinated refresh when available.

    A provider without refresh support performs an ordinary call-time lookup.
    Provider and driver are required even when the provider is static.
    """
    if provider is None or driver is None:
        raise ValueError("credential provider and refresh driver are required")
    if isinstance(provider, RefreshingCredentialProvider):
        return cast(C, await provider.resolve_with_refresh(call, driver))
    return await provider.resolve(call)


async def resolve_credential_after_rejection(
    provider: CredentialProvider[C], call: Call, driver: CredentialRefreshDriver[C]
) -> C:
    """Force one coordinated refresh after a rejected access credential.

    A connector calls this at most once per provider operation. Missing refresh
    support is an error, so rejected credentials are never silently reused.
    """
    if provider is None or driver is None:
        raise ValueError("credential provider and refresh driver are required")
    if not isinstance(provider, RejectedCredentialRefreshingProvider):
        raise ValueError("credential provider does not support refresh after rejection")
    return cast(C, await provider.resolve_after_rejection(call, driver))


class StaticCredentialProvider(Generic[C]):
    """Resolve in-memory credentials by logical connection without logging them."""

    def __init__(self, credentials: Mapping[ConnectionRef, C]) -> None:
        self._credentials = dict(credentials)

    async def resolve(self, call: Call) -> C:
        """Return configured credentials or fail without revealing connection data."""
        try:
            return self._credentials[call.connection]
        except KeyError as error:
            raise ValueError("connection is not configured") from error

    async def resolve_with_refresh(self, call: Call, driver: CredentialRefreshDriver[C]) -> C:
        """Return static credentials, which have no expiry or persistence."""
        return await self.resolve(call)
