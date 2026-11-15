# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Credential broker wire client and shared-connection cache."""

import asyncio
import json
import time
from collections.abc import Awaitable, Callable
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from urllib.parse import urlsplit

import httpx

from ..credentials import ReauthorizationRequiredError
from ..relaysignature import (
    RelayHeaders,
    RelaySignatureRefusal,
    matches_relay_signature,
    parse_relay_signature,
)
from .models import (
    ApplicationSecretRef,
    CredentialBrokerConfiguration,
    go_canonical_json_bytes,
    require_object,
    validate_broker_endpoint,
)

MAXIMUM_BROKER_RESPONSE_BYTES = 1 << 20
MAXIMUM_BROKER_CREDENTIAL_BYTES = 64 << 10
_MAXIMUM_RELAYED_TRIGGER_REGISTRATION_BYTES = 4096
_RELAY_TOKEN_RENEWAL_INTERVAL_SECONDS = 30.0
_BROKER_EXCHANGE_TIMEOUT_SECONDS = 40.0


class CredentialBrokerUnavailableError(OSError):
    """Transient broker transport or service failure."""


class CredentialBrokerRejectedError(ValueError):
    """Terminal broker refusal or invalid wire answer."""


class ConnectionNotSelectedError(ValueError):
    """The broker no longer selects this shared connection for the scope."""


class RejectedCredentialNotRefreshedError(ValueError):
    """Forced read returned the same revision rejected by the provider."""


class RelayedTriggerNotRelayedError(ValueError):
    """The broker does not relay this shared connection's provider events."""

    def __init__(self) -> None:
        """Create the error with its fixed message."""
        super().__init__("the credential broker does not relay this connection's events")


class RelaySignatureInvalidError(ValueError):
    """A relayed request's signature is missing, malformed, stale, or does not match the scope token."""

    def __init__(self) -> None:
        """Create the error with its fixed message."""
        super().__init__("relayed request signature is missing, invalid or stale")


class RelayTokenRenewalDeferredError(CredentialBrokerUnavailableError):
    """A relay signature mismatch arrived within 30 seconds of the last token reread; retry later."""

    def __init__(self) -> None:
        """Create the retryable error with its fixed message."""
        super().__init__("relayed request signature cannot be checked against a renewed scope token yet")


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
        *,
        clock: Callable[[], float] | None = None,
    ) -> None:
        """Bind the trusted endpoint, scope-token readers, optional HTTP client, and monotonic clock.

        A cached token is used without waiting for a token reread, so a stalled reread never blocks
        credential reads. A relay signature mismatch rereads the token at most once every 30 seconds.
        """
        validate_broker_endpoint(configuration.endpoint)
        self._endpoint = configuration.endpoint
        self._token_reference = configuration.token_secret_ref
        self._read_token = read_token
        self._current_token_reference = current_token_reference
        self._client = client or httpx.AsyncClient(follow_redirects=False, timeout=40)
        self._owns_client = client is None
        self._token = ""
        self._token_lock = asyncio.Lock()
        self._clock = clock if clock is not None else time.monotonic
        self._last_mismatch_renewal: float | None = None
        self._mismatch_renewal: asyncio.Task[str | None] | None = None

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
        return self._decode(await self._exchange(self._endpoint, request))

    async def register_relayed_trigger(
        self, shared_connection_id: str, connection_name: str, registration_path: str
    ) -> None:
        """Register a shared connection's relayed Trigger route at registration_path on the broker origin.

        Raises ConnectionNotSelectedError, RelayedTriggerNotRelayedError, CredentialBrokerRejectedError for
        any other answer, CredentialBrokerUnavailableError for a transient failure, and ValueError for an
        invalid path or an oversized request.
        """
        target = self._origin_url(registration_path)
        request: dict[str, object] = {
            "sharedConnectionId": shared_connection_id,
            "connectionName": connection_name,
        }
        if len(go_canonical_json_bytes(request)) > _MAXIMUM_RELAYED_TRIGGER_REGISTRATION_BYTES:
            raise ValueError("relayed Trigger registration request is too large")
        response = await self._exchange(target, request)
        if response.status_code in (408, 425, 429) or response.status_code >= 500:
            raise CredentialBrokerUnavailableError(f"credential broker HTTP {response.status_code}")
        if response.status_code != 200:
            raise CredentialBrokerRejectedError(f"credential broker HTTP {response.status_code}")
        try:
            answer = json.loads(response.content)
        except (ValueError, UnicodeDecodeError):
            raise CredentialBrokerRejectedError("credential broker answer is invalid") from None
        status = answer.get("status") if isinstance(answer, dict) else None
        if status == "REGISTERED":
            return
        if status == "CONNECTION_NOT_SELECTED":
            raise ConnectionNotSelectedError("project connection is not selected by this scope")
        if status == "NOT_RELAYED":
            raise RelayedTriggerNotRelayedError()
        raise CredentialBrokerRejectedError("credential broker status is unknown")

    async def verify_relayed_request(self, headers: RelayHeaders, body: bytes, received_at: datetime) -> None:
        """Verify that the host signed a relayed request with the scope token.

        Only a well-formed, fresh signature that does not match may reread the token, at most once every
        30 seconds; concurrent requests share one reread. Raises RelaySignatureInvalidError for a refused
        request, RelayTokenRenewalDeferredError (retryable) for a mismatch inside the reread window, and
        CredentialBrokerUnavailableError when the token cannot be read.
        """
        parsed = parse_relay_signature(headers, received_at)
        if isinstance(parsed, RelaySignatureRefusal):
            raise RelaySignatureInvalidError()
        try:
            token = await asyncio.wait_for(self._cached_or_read_token(), _BROKER_EXCHANGE_TIMEOUT_SECONDS)
        except (TimeoutError, Exception):
            raise CredentialBrokerUnavailableError("credential broker scope token is unavailable") from None
        if matches_relay_signature(parsed, token, body):
            return
        try:
            renewed = await asyncio.wait_for(
                self._renew_token_after_mismatch(token), _BROKER_EXCHANGE_TIMEOUT_SECONDS
            )
        except RelayTokenRenewalDeferredError:
            raise
        except (TimeoutError, Exception):
            raise CredentialBrokerUnavailableError("credential broker scope token is unavailable") from None
        if renewed is not None and matches_relay_signature(parsed, renewed, body):
            return
        raise RelaySignatureInvalidError()

    async def _renew_token_after_mismatch(self, mismatched: str) -> str | None:
        if self._token and self._token != mismatched:
            return self._token
        if self._mismatch_renewal is not None:
            return await asyncio.shield(self._mismatch_renewal)
        now = self._clock()
        if (
            self._last_mismatch_renewal is not None
            and now - self._last_mismatch_renewal < _RELAY_TOKEN_RENEWAL_INTERVAL_SECONDS
        ):
            raise RelayTokenRenewalDeferredError()
        self._last_mismatch_renewal = now
        renewal = asyncio.ensure_future(self._reread_token_after_mismatch(mismatched))
        self._mismatch_renewal = renewal
        renewal.add_done_callback(self._finish_mismatch_renewal)
        return await asyncio.shield(renewal)

    def _finish_mismatch_renewal(self, renewal: "asyncio.Task[str | None]") -> None:
        if self._mismatch_renewal is renewal:
            self._mismatch_renewal = None
        if not renewal.cancelled():
            # Every waiter may have timed out, so the outcome is retrieved here.
            renewal.exception()

    async def _reread_token_after_mismatch(self, mismatched: str) -> str | None:
        # A rotation is a new secret version, so the current reference is resolved again, as after 401.
        async with self._token_lock:
            if self._token and self._token != mismatched:
                return self._token
            self._token_reference = await self._current_token_reference()
            token = await self._read_token(self._token_reference)
            if not _is_header_safe_token(token):
                raise CredentialBrokerRejectedError("credential broker scope token is invalid")
            self._token = token
            return None if token == mismatched else token

    def _origin_url(self, path: str) -> str:
        has_query_or_fragment = any(character in path for character in "?#\\")
        if not path.startswith("/") or path.startswith("//") or has_query_or_fragment:
            raise ValueError(
                "credential broker endpoint path must be an absolute path without a query or fragment"
            )
        endpoint = urlsplit(self._endpoint)
        return f"{endpoint.scheme}://{endpoint.netloc}{path}"

    async def _exchange(self, target_url: str, body: dict[str, object]) -> httpx.Response:
        token = await self._cached_or_read_token()
        response = await self._post(target_url, token, body)
        if response.status_code == 401:
            renewed = await self._renew_token(token)
            if renewed == token:
                raise CredentialBrokerRejectedError("credential broker refused scope token")
            response = await self._post(target_url, renewed, body)
        return response

    async def _cached_or_read_token(self) -> str:
        # A cached token needs no lock, so a slow mismatch reread cannot hold up reads.
        if self._token:
            return self._token
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

    async def _post(self, target_url: str, token: str, body: dict[str, object]) -> httpx.Response:
        try:
            async with self._client.stream(
                "POST",
                target_url,
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


def _is_header_safe_token(token: str) -> bool:
    return bool(token) and all(0x21 <= ord(character) <= 0x7E for character in token)


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
