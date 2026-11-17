# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Douyin mini app behavior summary with an in-memory client-token cache."""

import asyncio
import hashlib
import json
from collections.abc import Callable
from datetime import UTC, datetime, time, timedelta, timezone
from typing import Any, cast

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
from ._generated.definitions import (
    CONNECTOR_ID,
    DEFECT,
    FOUND,
    GET_BEHAVIOR_SUMMARY_DEFINITION,
    GET_BEHAVIOR_SUMMARY_OPERATION_ID,
    INVALID_RESPONSE,
    PROVIDER_REJECTED,
)
from .models import GetBehaviorSummaryInput, GetBehaviorSummaryOutput

_CHINA_TIME = timezone(timedelta(hours=8))
_TOKEN_ENDPOINT = "https://open.douyin.com/oauth/client_token/"
_BEHAVIOR_ENDPOINT = "https://open.douyin.com/api/platform/v2/data_analysis/query_behavior_data/"
_MAX_INT64 = (1 << 63) - 1


class Client:
    """Resolve mini app credentials and cache short-lived tokens in Worker memory."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        current_time: Callable[[], datetime] | None = None,
    ) -> None:
        """Validate settings and own the transport unless one was injected.

        ``current_time`` is an injectable clock for deterministic date and token tests.
        Tokens remain only in this client instance and are discarded on Worker restart.
        """
        if config is None or credentials is None:
            raise ValueError("Douyin configuration and credential source are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("Douyin configuration is invalid")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self.owns_http_client = http_client is None
        self.current_time = current_time or (lambda: datetime.now(UTC))
        self.token_lock = asyncio.Lock()
        self.tokens: dict[str, tuple[str, datetime]] = {}

    def get_behavior_summary(self) -> "GetBehaviorSummaryOperation":
        """Bind the completed daily behavior Query."""
        return GetBehaviorSummaryOperation(self)

    async def aclose(self) -> None:
        """Close the connector-owned transport after the Worker stops."""
        if self.owns_http_client:
            await self.http_client.aclose()

    async def token_for(
        self, credentials: Credentials
    ) -> tuple[str, QueryAttempt[GetBehaviorSummaryOutput] | None]:
        """Reuse a current token or request one with the non-service-provider grant."""
        cache_key = _token_cache_key(credentials)
        async with self.token_lock:
            cached = self.tokens.get(cache_key)
            if cached is not None and self.current_time() + timedelta(minutes=1) < cached[1]:
                return cached[0], None
            try:
                request = self.http_client.build_request(
                    "POST",
                    _TOKEN_ENDPOINT,
                    headers={"Content-Type": "application/json", "Accept": "application/json"},
                    json={
                        "grant_type": "client_credential",
                        "client_key": credentials.client_key,
                        "client_secret": credentials.client_secret.reveal(),
                    },
                )
                response, _ = await send_traced(self.http_client, request)
            except (ProviderTransportError, httpx.HTTPError):
                return "", _retry(FailureKind.AVAILABILITY, "Douyin token request failed")
            try:
                status_attempt = _classify_http(response.status_code, response.headers)
                if status_attempt is not None:
                    return "", status_attempt
                try:
                    body = await read_bounded_body(response, self.max_response_bytes)
                except BodyTooLargeError:
                    return "", _branch(
                        INVALID_RESPONSE,
                        FailureKind.RESPONSE_TOO_LARGE,
                        "Douyin token response exceeded the configured limit",
                    )
                except ProviderTransportError:
                    return "", _retry(FailureKind.AVAILABILITY, "Douyin token response was interrupted")
                wire = _decode_token(body)
                if wire is None:
                    return "", _branch(
                        INVALID_RESPONSE, FailureKind.PROTOCOL, "Douyin returned invalid token data"
                    )
                error_code = wire["error_code"]
                if error_code == 10020:
                    return "", _retry(FailureKind.RATE_LIMIT, "Douyin limited token requests")
                if error_code != 0:
                    return "", _branch(
                        PROVIDER_REJECTED,
                        FailureKind.AUTHENTICATION,
                        f"Douyin rejected token request with code {error_code}",
                    )
                token = wire.get("access_token", "")
                expires_in = wire.get("expires_in")
                if (
                    not _valid_opaque_value(token)
                    or type(expires_in) is not int
                    or not 60 < expires_in <= 86400
                ):
                    return "", _branch(
                        INVALID_RESPONSE, FailureKind.PROTOCOL, "Douyin returned incomplete token data"
                    )
                self.tokens[cache_key] = (token, self.current_time() + timedelta(seconds=expires_in))
                return token, None
            finally:
                await response.aclose()

    async def forget_token(self, credentials: Credentials) -> None:
        """Discard an expired token so the next retry acquires a fresh one."""
        async with self.token_lock:
            self.tokens.pop(_token_cache_key(credentials), None)


class GetBehaviorSummaryOperation:
    """Read aggregate counts for one completed China Standard Time report day."""

    def __init__(self, client: Client) -> None:
        """Bind a validated Douyin client."""
        if client is None:
            raise ValueError("Douyin client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest operation identity and branches."""
        return GET_BEHAVIOR_SUMMARY_DEFINITION

    async def invoke(
        self, call: Call, input: GetBehaviorSummaryInput
    ) -> QueryAttempt[GetBehaviorSummaryOutput]:
        """Acquire a client token and request the selected daily summary."""
        report_day = _report_day(
            input.date if isinstance(input, GetBehaviorSummaryInput) else None, self.client.current_time()
        )
        if report_day is None:
            return _branch(
                DEFECT,
                FailureKind.VALIDATION,
                "Douyin date must be a completed YYYY-MM-DD day within the previous 90 days",
            )
        try:
            credentials = await self.client.credentials.resolve(call)
            client_key = credentials.client_key
            client_secret = credentials.client_secret.reveal()
        except Exception:
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "Douyin mini app credentials are unavailable or invalid"
            )
        if not _valid_opaque_value(client_key) or not _valid_opaque_value(client_secret):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "Douyin mini app credentials are unavailable or invalid"
            )
        token, token_attempt = await self.client.token_for(credentials)
        if token_attempt is not None:
            return token_attempt
        start = datetime.combine(report_day, time.min, _CHINA_TIME)
        end = start + timedelta(days=1) - timedelta(seconds=1)
        try:
            request = self.client.http_client.build_request(
                "GET",
                _BEHAVIOR_ENDPOINT,
                params={"start_time": str(int(start.timestamp())), "end_time": str(int(end.timestamp()))},
                headers={"Accept": "application/json", "access-token": token},
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError):
            return _retry(FailureKind.AVAILABILITY, "Douyin behavior request failed")
        try:
            status_attempt = _classify_http(response.status_code, response.headers)
            if status_attempt is not None:
                return status_attempt
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Douyin response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Douyin behavior response was interrupted")
            wire = _decode_report(body)
            if wire is None:
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL, "Douyin returned invalid behavior data"
                )
            error_number = wire["err_no"]
            if error_number == -1:
                return _retry(FailureKind.AVAILABILITY, "Douyin analytics is temporarily unavailable")
            if error_number == 28001008:
                await self.client.forget_token(credentials)
                return _retry(FailureKind.AUTHENTICATION, "Douyin token expired; renew authentication")
            if error_number != 0:
                return _branch(
                    PROVIDER_REJECTED,
                    FailureKind.PROVIDER_REJECTION,
                    f"Douyin rejected behavior query with code {error_number}",
                )
            summary = _decode_summary(wire)
            if summary is None:
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL, "Douyin returned incomplete behavior summary"
                )
            return QueryAttempt.branch_result(FOUND, summary, Receipt())
        finally:
            await response.aclose()


def _report_day(value: object, now: datetime) -> Any:
    if not isinstance(value, str):
        return None
    try:
        parsed = datetime.strptime(value, "%Y-%m-%d").date()
    except ValueError:
        return None
    if parsed.isoformat() != value:
        return None
    local_now = now.astimezone(_CHINA_TIME)
    today = local_now.date()
    latest = today - timedelta(days=2 if local_now.hour < 10 else 1)
    if parsed < today - timedelta(days=90) or parsed > latest:
        return None
    return parsed


def _valid_opaque_value(value: object) -> bool:
    return isinstance(value, str) and 0 < len(value) <= 1024 and all(0x21 <= ord(c) <= 0x7E for c in value)


def _token_cache_key(credentials: Credentials) -> str:
    value = credentials.client_key + "\x00" + credentials.client_secret.reveal()
    return hashlib.sha256(value.encode("utf-8")).hexdigest()


def _classify_http(status: int, headers: httpx.Headers) -> QueryAttempt[GetBehaviorSummaryOutput] | None:
    if status == 200:
        return None
    retry_after = parse_retry_after(headers.get("Retry-After", ""), datetime.now(UTC))
    if status == 429:
        return _retry(FailureKind.RATE_LIMIT, "Douyin rate limited the request", retry_after)
    if status == 408 or status >= 500:
        return _retry(FailureKind.AVAILABILITY, "Douyin service is temporarily unavailable", retry_after)
    kind = FailureKind.PROVIDER_REJECTION
    if status == 401:
        kind = FailureKind.AUTHENTICATION
    elif status == 403:
        kind = FailureKind.AUTHORIZATION
    return _branch(PROVIDER_REJECTED, kind, f"Douyin rejected request with HTTP {status}")


def _decode_token(body: bytes) -> dict[str, Any] | None:
    try:
        wire: Any = json.loads(body.decode("utf-8"))
    except (UnicodeDecodeError, ValueError):
        return None
    if not isinstance(wire, dict) or not isinstance(wire.get("data"), dict):
        return None
    data = wire["data"]
    if type(data.get("error_code")) is not int or not -(1 << 63) <= data["error_code"] <= _MAX_INT64:
        return None
    if "access_token" in data and not isinstance(data["access_token"], str):
        return None
    if "expires_in" in data and data["expires_in"] is not None and type(data["expires_in"]) is not int:
        return None
    return cast(dict[str, Any], data)


def _decode_report(body: bytes) -> dict[str, Any] | None:
    try:
        wire: Any = json.loads(body.decode("utf-8"))
    except (UnicodeDecodeError, ValueError):
        return None
    if not isinstance(wire, dict) or type(wire.get("err_no")) is not int:
        return None
    if not -(1 << 63) <= wire["err_no"] <= _MAX_INT64:
        return None
    return wire


def _decode_summary(wire: dict[str, Any]) -> GetBehaviorSummaryOutput | None:
    data = wire.get("data")
    summary = data.get("sum") if isinstance(data, dict) else None
    if not isinstance(summary, dict):
        return None
    names = ("active_user_num", "new_user_num", "total_user_num", "open_time", "share_time")
    values = [summary.get(name) for name in names]
    if any(type(value) is not int or not 0 <= value <= _MAX_INT64 for value in values):
        return None
    return GetBehaviorSummaryOutput(*(cast(int, value) for value in values))


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetBehaviorSummaryOutput]:
    return QueryAttempt.branch_result(
        branch,
        GetBehaviorSummaryOutput(),
        Receipt(),
        Failure(kind, CONNECTOR_ID, GET_BEHAVIOR_SUMMARY_OPERATION_ID, message),
    )


def _retry(kind: FailureKind, message: str, after: float = 0) -> QueryAttempt[GetBehaviorSummaryOutput]:
    return QueryAttempt.retry(Failure(kind, CONNECTOR_ID, GET_BEHAVIOR_SUMMARY_OPERATION_ID, message), after)
