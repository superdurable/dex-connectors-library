# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Credential broker wire client and shared-connection cache."""

import asyncio
import json
from collections.abc import Awaitable, Callable
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta

import httpx

from ..credentials import ReauthorizationRequiredError
from .models import (
    ApplicationSecretRef,
    CredentialBrokerConfiguration,
    require_object,
    validate_broker_endpoint,
)

MAXIMUM_BROKER_RESPONSE_BYTES = 1 << 20
MAXIMUM_BROKER_CREDENTIAL_BYTES = 64 << 10


class CredentialBrokerUnavailableError(OSError):
    """Transient broker transport or service failure."""


class CredentialBrokerRejectedError(ValueError):
    """Terminal broker refusal or invalid wire answer."""


class ConnectionNotSelectedError(ValueError):
    """The broker no longer selects this shared connection for the scope."""


class RejectedCredentialNotRefreshedError(ValueError):
    """Forced read returned the same revision rejected by the provider."""


@dataclass(frozen=True, slots=True)
class BrokerCredential:
    """Private credential body and broker revision metadata."""

    credential: dict[str, object]
    revision: int
    issued_at: datetime | None
    expires_at: datetime | None
    read_at: datetime

    def __repr__(self) -> str:
        """Redact credential material from debugging output."""
        return f"BrokerCredential(revision={self.revision}, credential=[REDACTED])"


class CredentialBrokerClient:
    """Async scope-token client for the shared credential read protocol."""

    def __init__(
        self,
        configuration: CredentialBrokerConfiguration,
        read_token: Callable[[ApplicationSecretRef], Awaitable[str]],
        current_token_reference: Callable[[], Awaitable[ApplicationSecretRef]],
        client: httpx.AsyncClient | None = None,
    ) -> None:
        validate_broker_endpoint(configuration.endpoint)
        self._endpoint = configuration.endpoint
        self._token_reference = configuration.token_secret_ref
        self._read_token = read_token
        self._current_token_reference = current_token_reference
        self._client = client or httpx.AsyncClient(follow_redirects=False, timeout=40)
        self._owns_client = client is None
        self._token = ""
        self._token_lock = asyncio.Lock()

    async def close(self) -> None:
        """Close the internally owned HTTP client after all calls finish."""
        if self._owns_client:
            await self._client.aclose()

    async def read(self, shared_connection_id: str, rejected_revision: int = 0) -> BrokerCredential:
        """Read one shared credential, renewing a rotated scope token once on 401."""
        if not shared_connection_id or rejected_revision < 0:
            raise ValueError("invalid shared credential read")
        request: dict[str, object] = {"sharedConnectionId": shared_connection_id}
        if rejected_revision:
            request["rejectedRevision"] = rejected_revision
        token = await self._get_token()
        response = await self._post(token, request)
        if response.status_code == 401:
            renewed = await self._renew_token(token)
            if renewed == token:
                raise CredentialBrokerRejectedError("credential broker refused scope token")
            response = await self._post(renewed, request)
        return self._decode(response)

    async def _get_token(self) -> str:
        async with self._token_lock:
            if not self._token:
                self._token = await self._read_token(self._token_reference)
            return self._token

    async def _renew_token(self, refused: str) -> str:
        async with self._token_lock:
            if self._token and self._token != refused:
                return self._token
            self._token = ""
            self._token_reference = await self._current_token_reference()
            self._token = await self._read_token(self._token_reference)
            return self._token

    async def _post(self, token: str, body: dict[str, object]) -> httpx.Response:
        try:
            async with self._client.stream(
                "POST",
                self._endpoint,
                json=body,
                headers={
                    "Authorization": "Bearer " + token,
                    "Content-Type": "application/json",
                    "Accept": "application/json",
                },
                follow_redirects=False,
            ) as response:
                if response.status_code != 200:
                    return httpx.Response(response.status_code)
                chunks: list[bytes] = []
                size = 0
                async for chunk in response.aiter_bytes():
                    size += len(chunk)
                    if size > MAXIMUM_BROKER_RESPONSE_BYTES:
                        raise CredentialBrokerRejectedError("credential broker response is too large")
                    chunks.append(chunk)
                return httpx.Response(response.status_code, content=b"".join(chunks))
        except httpx.TransportError:
            raise CredentialBrokerUnavailableError("credential broker is unavailable") from None

    @staticmethod
    def _decode(response: httpx.Response) -> BrokerCredential:
        status_code = response.status_code
        if status_code in (408, 425, 429) or status_code >= 500:
            raise CredentialBrokerUnavailableError(f"credential broker HTTP {status_code}")
        if status_code != 200:
            raise CredentialBrokerRejectedError(f"credential broker HTTP {status_code}")
        if len(response.content) > MAXIMUM_BROKER_RESPONSE_BYTES:
            raise CredentialBrokerRejectedError("credential broker response is too large")
        try:
            answer = require_object(json.loads(response.content), "broker answer")
        except (ValueError, UnicodeDecodeError, json.JSONDecodeError):
            raise CredentialBrokerRejectedError("credential broker answer is invalid") from None
        status = answer.get("status")
        if status == "REAUTHORIZATION_REQUIRED":
            raise ReauthorizationRequiredError()
        if status == "CONNECTION_NOT_SELECTED":
            raise ConnectionNotSelectedError("project connection is not selected by this scope")
        if status != "READY":
            raise CredentialBrokerRejectedError("credential broker status is unknown")
        credential = answer.get("credential")
        revision = answer.get("revision")
        if (
            not isinstance(credential, dict)
            or not isinstance(revision, int)
            or revision <= 0
            or len(json.dumps(credential).encode()) > MAXIMUM_BROKER_CREDENTIAL_BYTES
        ):
            raise CredentialBrokerRejectedError("credential broker READY answer is invalid")
        try:
            issued_at = (
                datetime.fromisoformat(answer["issuedAt"].replace("Z", "+00:00"))
                if answer.get("issuedAt")
                else None
            )
            expires_at = (
                datetime.fromisoformat(answer["expiresAt"].replace("Z", "+00:00"))
                if answer.get("expiresAt")
                else None
            )
        except (ValueError, AttributeError):
            raise CredentialBrokerRejectedError("credential broker timestamps are invalid") from None
        return BrokerCredential(credential, revision, issued_at, expires_at, datetime.now(UTC))


class SharedConnection:
    """Revision-aware broker cache for one immutable shared connection selection."""

    def __init__(self, shared_connection_id: str, broker: CredentialBrokerClient) -> None:
        if not shared_connection_id:
            raise ValueError("shared connection ID is required")
        self.shared_connection_id = shared_connection_id
        self._broker = broker
        self._credential: BrokerCredential | None = None
        self._lock = asyncio.Lock()
        self._issued_by_call: dict[str, tuple[int, datetime]] = {}
        self._next_read_after = datetime.min.replace(tzinfo=UTC)

    async def resolve(self, call_id: str) -> dict[str, object]:
        """Return an unexpired credential; refresh stale material in one task."""
        now = datetime.now(UTC)
        current = self._credential
        if current is not None and not self._is_expired(current, now) and now < self._next_read_after:
            self._record_issued(call_id, current.revision, now)
            return current.credential
        if current is not None and not self._is_stale(current, now):
            self._record_issued(call_id, current.revision, now)
            return current.credential
        async with self._lock:
            current = self._credential
            now = datetime.now(UTC)
            if current is None or (self._is_stale(current, now) and now >= self._next_read_after):
                try:
                    current = await self._broker.read(self.shared_connection_id)
                    self._credential = current
                except CredentialBrokerUnavailableError:
                    self._next_read_after = now + timedelta(seconds=10)
                    if current is None or self._is_expired(current, now):
                        raise
            if current is None or self._is_expired(current, now):
                raise CredentialBrokerUnavailableError("shared credential is expired")
            self._record_issued(call_id, current.revision, now)
            return current.credential

    async def resolve_after_rejection(self, call_id: str) -> dict[str, object]:
        """Force broker refresh for the revision this provider call received."""
        async with self._lock:
            now = datetime.now(UTC)
            issued = self._issued_by_call.get(call_id)
            rejected_revision = (
                issued[0] if issued else (self._credential.revision if self._credential else 0)
            )
            if self._credential is not None and self._credential.revision != rejected_revision:
                current = self._credential
            else:
                current = await self._broker.read(self.shared_connection_id, rejected_revision)
                self._credential = current
            if current.revision == rejected_revision:
                raise RejectedCredentialNotRefreshedError("the rejected credential was not refreshed")
            self._record_issued(call_id, current.revision, now)
            return current.credential

    def _record_issued(self, call_id: str, revision: int, now: datetime) -> None:
        if not call_id:
            return
        self._issued_by_call[call_id] = (revision, now)
        if len(self._issued_by_call) > 4096:
            cutoff = now - timedelta(minutes=10)
            self._issued_by_call = {
                key: value for key, value in self._issued_by_call.items() if value[1] > cutoff
            }
            if len(self._issued_by_call) > 4096:
                self._issued_by_call.pop(next(iter(self._issued_by_call)))

    @staticmethod
    def _is_expired(credential: BrokerCredential, now: datetime) -> bool:
        return credential.expires_at is not None and credential.expires_at <= now

    @staticmethod
    def _is_stale(credential: BrokerCredential, now: datetime) -> bool:
        if credential.expires_at is None:
            return now - credential.read_at >= timedelta(minutes=15)
        lifetime = credential.expires_at - (credential.issued_at or credential.read_at)
        skew = min(timedelta(minutes=5), lifetime / 4) if lifetime > timedelta(0) else timedelta(0)
        return now >= credential.expires_at - skew
