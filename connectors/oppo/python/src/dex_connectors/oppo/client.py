# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""OPPO Push quota Query with signed authentication and bounded responses."""

import hashlib
import json
from collections.abc import Callable
from datetime import UTC, datetime
from typing import Any, cast
from urllib.parse import urlencode

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
from ._generated.definitions import GET_PUSH_QUOTA_DEFINITION
from .models import GetPushQuotaInput, GetPushQuotaOutput

_AUTH_ENDPOINT = "https://api.push.oppomobile.com/server/v1/auth"
_QUOTA_ENDPOINT = "https://feedback.push.oppomobile.com/server/v1/feedback/fetch_push_permit"


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetPushQuotaOutput]:
    return QueryAttempt.branch_result(
        branch, GetPushQuotaOutput(), Receipt(), Failure(kind, "oppo", "getPushQuota", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[GetPushQuotaOutput]:
    return QueryAttempt.retry(Failure(kind, "oppo", "getPushQuota", message), delay)


def _valid_secret(value: str) -> bool:
    return (
        bool(value)
        and len(value.encode()) <= 1024
        and value.strip() == value
        and all(ord(char) >= 0x20 and ord(char) != 0x7F for char in value)
    )


class Client:
    """Own OPPO settings and resolve both secrets for each quota lookup."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        clock: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("OPPO configuration and credentials are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("OPPO configuration is invalid")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self.clock = clock or (lambda: datetime.now(UTC))

    def get_push_quota(self) -> "GetPushQuotaOperation":
        """Bind the generated Push quota Query."""
        return GetPushQuotaOperation(self)

    async def aclose(self) -> None:
        """Close only an HTTP client this instance created."""
        if self._owns_client:
            await self.http_client.aclose()


class GetPushQuotaOperation:
    """Exchange signed credentials for a token, then read daily counters."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("OPPO client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated quota Query identity and retry policy."""
        return GET_PUSH_QUOTA_DEFINITION

    async def invoke(self, call: Call, input: GetPushQuotaInput) -> QueryAttempt[GetPushQuotaOutput]:
        """Send the auth and quota requests, classifying each response."""
        try:
            credentials = await self.client.credentials.resolve(call)
            app_key = credentials.app_key.reveal()
            master_secret = credentials.master_secret.reveal()
            if not _valid_secret(app_key) or not _valid_secret(master_secret):
                raise ValueError("invalid OPPO credentials")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "OPPO Push credentials are unavailable or invalid"
            )
        timestamp = str(int(self.client.clock().timestamp() * 1000))
        signature = hashlib.sha256((app_key + timestamp + master_secret).encode()).hexdigest()
        form = urlencode({"app_key": app_key, "timestamp": timestamp, "sign": signature})
        try:
            auth_request = self.client.http_client.build_request(
                "POST",
                _AUTH_ENDPOINT,
                headers={"Accept": "application/json", "Content-Type": "application/x-www-form-urlencoded"},
                content=form.encode(),
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "OPPO Push auth request is invalid")
        auth = await self._read_response(auth_request)
        if isinstance(auth, QueryAttempt):
            return auth
        auth_body, status, delay = auth
        if status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"OPPO Push rejected authentication with HTTP {status}",
            )
        try:
            document: Any = json.loads(auth_body)
        except (ValueError, UnicodeDecodeError):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "OPPO Push returned malformed authentication data"
            )
        code = document.get("code") if isinstance(document, dict) else None
        if type(code) is not int:
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "OPPO Push returned malformed authentication data"
            )
        if code != 0:
            return _classify_code(code, "authentication", delay)
        data = document.get("data")
        token = data.get("auth_token") if isinstance(data, dict) else None
        if not isinstance(token, str) or not _valid_secret(token):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "OPPO Push omitted an auth token")
        try:
            quota_request = self.client.http_client.build_request(
                "GET", _QUOTA_ENDPOINT, headers={"Accept": "application/json", "auth_token": token}
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "OPPO Push quota request is invalid")
        quota = await self._read_response(quota_request)
        if isinstance(quota, QueryAttempt):
            return quota
        quota_body, status, delay = quota
        if status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"OPPO Push rejected quota request with HTTP {status}",
            )
        try:
            document = json.loads(quota_body)
        except (ValueError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "OPPO Push returned malformed quota data")
        code = document.get("code") if isinstance(document, dict) else None
        if type(code) is not int:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "OPPO Push returned malformed quota data")
        if code != 0:
            return _classify_code(code, "quota", delay)
        data = document.get("data")
        if not isinstance(data, dict):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "OPPO Push returned invalid quota counters"
            )
        total, push, remain = (data.get(name) for name in ("total_count", "push_count", "remain_count"))
        if any(type(value) is not int or value < 0 for value in (total, push, remain)) or cast(
            int, push
        ) + cast(int, remain) > cast(int, total):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "OPPO Push returned invalid quota counters"
            )
        return QueryAttempt.branch_result(
            "found",
            GetPushQuotaOutput(cast(int, total), cast(int, push), cast(int, remain)),
            Receipt(),
        )

    async def _read_response(
        self, request: httpx.Request
    ) -> tuple[bytes, int, float] | QueryAttempt[GetPushQuotaOutput]:
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "OPPO Push request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "OPPO Push rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "OPPO Push API is temporarily unavailable", delay)
            if status != 200:
                return b"", status, delay
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "OPPO Push response exceeded configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "OPPO Push response was interrupted")
            return body, status, delay
        finally:
            await response.aclose()


def _classify_code(code: int, operation: str, delay: float) -> QueryAttempt[GetPushQuotaOutput]:
    if code in (-2, 13):
        return _retry(FailureKind.RATE_LIMIT, f"OPPO Push {operation} is rate limited", delay)
    if code == -1:
        return _retry(FailureKind.AVAILABILITY, f"OPPO Push {operation} is unavailable", delay)
    return _branch(
        "providerRejected", FailureKind.PROVIDER_REJECTION, f"OPPO Push rejected {operation} with code {code}"
    )
