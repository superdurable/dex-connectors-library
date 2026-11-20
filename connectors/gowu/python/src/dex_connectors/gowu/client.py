# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Signed Gowu application Query with Go-compatible failure decisions."""

import hashlib
import hmac
import json
import secrets
from collections.abc import Callable
from datetime import UTC, datetime
from typing import Any

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    QueryAttempt,
    QueryDefinition,
    Receipt,
)
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import GET_ACCESS_CLIENT_DEFINITION
from .models import AccessClientResult, GetAccessClientInput


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[AccessClientResult]:
    return QueryAttempt.branch_result(
        branch, AccessClientResult(), Receipt(), Failure(kind, "gowu", "getAccessClient", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[AccessClientResult]:
    return QueryAttempt.retry(Failure(kind, "gowu", "getAccessClient", message), delay)


class Client:
    """Resolve rotated credentials and sign each application-info request."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        clock: Callable[[], datetime] | None = None,
        nonce: Callable[[], bytes] | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or not 0 < config.max_response_bytes <= 16 << 20
        ):
            raise ValueError("Gowu configuration and credentials are invalid")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.base_url = config.base_url.rstrip("/")
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self.clock = clock or (lambda: datetime.now(UTC))
        self.nonce = nonce or (lambda: secrets.token_bytes(16))

    def get_access_client(self) -> "GetAccessClientOperation":
        """Bind the application-info Query."""
        return GetAccessClientOperation(self)

    async def aclose(self) -> None:
        """Close the transport if this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class GetAccessClientOperation:
    """Read the authorized application without exposing its signing secret."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Gowu client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the stable generated Query definition."""
        return GET_ACCESS_CLIENT_DEFINITION

    async def invoke(self, call: Call, input: GetAccessClientInput) -> QueryAttempt[AccessClientResult]:
        """Make one signed request and classify its bounded response."""
        if not isinstance(input, GetAccessClientInput):
            return _branch("defect", FailureKind.VALIDATION, "Gowu input is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            app_key = credentials.app_key
            app_secret = credentials.app_secret.reveal()
            if not app_key or len(app_key) > 128 or app_key != app_key.strip():
                raise ValueError("invalid app key")
            if not app_secret or len(app_secret) > 2048 or app_secret != app_secret.strip():
                raise ValueError("invalid app secret")
        except (ValueError, LookupError, OSError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Gowu credentials are unavailable or invalid"
            )
        try:
            nonce = self.client.nonce().hex()
            if len(nonce) != 32:
                raise ValueError("nonce length")
        except (OSError, ValueError):
            return _retry(FailureKind.AVAILABILITY, "Gowu request nonce is unavailable")
        timestamp = str(int(self.client.clock().timestamp()))
        empty_body_hash = hashlib.sha256(b"").hexdigest()
        canonical = "\n".join(
            ("OCP-SIGN-V2", timestamp, nonce, "GET", "/v1/access-clients/me", "", empty_body_hash, "")
        )
        signature = hmac.new(app_secret.encode(), canonical.encode(), hashlib.sha256).hexdigest()
        try:
            request = self.client.http_client.build_request(
                "GET",
                self.client.base_url + "/v1/access-clients/me",
                headers={
                    "Accept": "application/json",
                    "x-app-key": app_key,
                    "x-timestamp": timestamp,
                    "x-nonce": nonce,
                    "x-sign-version": "2",
                    "x-signature": signature,
                },
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(FailureKind.AVAILABILITY, "Gowu application request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), self.client.clock())
            if response.status_code == 429:
                return _retry(FailureKind.RATE_LIMIT, "Gowu rate limited application query", delay)
            if response.status_code == 408 or response.status_code >= 500:
                return _retry(FailureKind.AVAILABILITY, "Gowu application API is unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Gowu response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Gowu application response was interrupted")
            if response.status_code != 200:
                kind = {
                    401: FailureKind.AUTHENTICATION,
                    403: FailureKind.AUTHORIZATION,
                    400: FailureKind.VALIDATION,
                    404: FailureKind.VALIDATION,
                    412: FailureKind.VALIDATION,
                }.get(response.status_code, FailureKind.PROVIDER_REJECTION)
                return _branch(
                    "providerRejected",
                    kind,
                    f"Gowu rejected application query with HTTP {response.status_code}",
                )
            try:
                data: Any = json.loads(
                    body, parse_constant=lambda _: (_ for _ in ()).throw(ValueError("invalid number"))
                )
            except (UnicodeDecodeError, ValueError):
                return _branch("invalidResponse", FailureKind.PROTOCOL, "Gowu returned invalid JSON")
            if (
                not isinstance(data, dict)
                or data.get("appKey") != app_key
                or type(data.get("apiCallCount")) is not int
                or data["apiCallCount"] < 0
            ):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "Gowu returned an invalid application response"
                )
            remark = data.get("remark")
            if remark is None:
                remark = ""
            if not isinstance(remark, str) or len(remark.encode()) > 256:
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "Gowu returned an invalid application remark"
                )
            return QueryAttempt.branch_result(
                "found", AccessClientResult(remark, data["apiCallCount"]), Receipt()
            )
        finally:
            await response.aclose()
