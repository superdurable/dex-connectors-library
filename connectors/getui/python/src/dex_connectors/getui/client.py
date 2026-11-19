# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Getui historical push report transport and Go-compatible decisions."""

import hashlib
import json
import re
from collections.abc import Callable
from datetime import UTC, datetime, timedelta, timezone
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
from ._generated.definitions import GET_DAILY_PUSH_REPORT_DEFINITION
from .models import GetDailyPushReportInput, GetDailyPushReportOutput

_BASE = "https://restapi.getui.com/v2/"


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetDailyPushReportOutput]:
    return QueryAttempt.branch_result(
        branch, GetDailyPushReportOutput(), Receipt(), Failure(kind, "getui", "getDailyPushReport", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[GetDailyPushReportOutput]:
    return QueryAttempt.retry(Failure(kind, "getui", "getDailyPushReport", message), delay)


def _valid_opaque(value: str) -> bool:
    return (
        bool(value)
        and len(value) <= 1024
        and value.strip() == value
        and all(0x20 < ord(character) < 0x7F for character in value)
    )


class Client:
    """Resolve Getui app credentials and cache short-lived auth tokens in memory."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Getui configuration and credential source are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("Getui timeout or response limit is invalid")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self.now = now or (lambda: datetime.now(UTC))
        self.tokens: dict[str, tuple[str, datetime]] = {}

    def get_daily_push_report(self) -> "GetDailyPushReportOperation":
        """Bind the historical daily report Query to this client."""
        return GetDailyPushReportOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP transport when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def token_for(
        self, credentials: Credentials
    ) -> tuple[str, QueryAttempt[GetDailyPushReportOutput] | None]:
        """Use a valid cached token or exchange app credentials for a new one."""
        app_key = credentials.app_key.reveal()
        master_secret = credentials.master_secret.reveal()
        cache_key = hashlib.sha256(
            (credentials.app_id + "\x00" + app_key + "\x00" + master_secret).encode()
        ).hexdigest()
        cached = self.tokens.get(cache_key)
        if cached and self.now() + timedelta(minutes=1) < cached[1]:
            return cached[0], None
        timestamp = str(int(self.now().timestamp() * 1000))
        signature = hashlib.sha256((app_key + timestamp + master_secret).encode()).hexdigest()
        payload = json.dumps(
            {"appkey": app_key, "sign": signature, "timestamp": timestamp},
            separators=(",", ":"),
        ).encode()
        request = self.http_client.build_request(
            "POST",
            _BASE + credentials.app_id + "/auth",
            content=payload,
            headers={"Content-Type": "application/json;charset=utf-8", "Accept": "application/json"},
        )
        body, error = await self._request(request, "auth")
        if error is not None:
            return "", error
        assert body is not None
        try:
            wire: Any = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return "", _branch("invalidResponse", FailureKind.PROTOCOL, "Getui returned invalid auth data")
        if not isinstance(wire, dict) or type(wire.get("code")) is not int:
            return "", _branch("invalidResponse", FailureKind.PROTOCOL, "Getui returned invalid auth data")
        if wire["code"] != 0:
            return "", _branch(
                "providerRejected",
                FailureKind.AUTHENTICATION,
                f"Getui rejected auth with code {wire['code']}",
            )
        data = wire.get("data")
        if not isinstance(data, dict):
            return "", _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Getui returned incomplete auth token"
            )
        token = data.get("token")
        expiry_text = data.get("expire_time")
        if not isinstance(expiry_text, str):
            return "", _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Getui returned incomplete auth token"
            )
        try:
            expiry = datetime.fromtimestamp(int(expiry_text) / 1000, UTC)
        except (ValueError, TypeError, OverflowError):
            return "", _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Getui returned incomplete auth token"
            )
        if (
            not isinstance(token, str)
            or not _valid_opaque(token)
            or not self.now() + timedelta(minutes=1) < expiry
        ):
            return "", _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Getui returned incomplete auth token"
            )
        self.tokens[cache_key] = (token, expiry)
        return token, None

    async def _request(
        self, request: httpx.Request, phase: str
    ) -> tuple[bytes | None, QueryAttempt[GetDailyPushReportOutput] | None]:
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return None, _retry(FailureKind.AVAILABILITY, f"Getui {phase} request failed")
        try:
            status = response.status_code
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return None, _retry(FailureKind.RATE_LIMIT, "Getui rate limited the request", delay)
            if status == 408 or status >= 500:
                return None, _retry(
                    FailureKind.AVAILABILITY, "Getui service is temporarily unavailable", delay
                )
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return None, _branch("providerRejected", kind, f"Getui rejected request with HTTP {status}")
            try:
                return await read_bounded_body(response, self.max_response_bytes), None
            except BodyTooLargeError:
                return None, _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    f"Getui {phase} exceeded the configured response limit",
                )
            except ProviderTransportError:
                return None, _retry(FailureKind.AVAILABILITY, f"Getui {phase} response was interrupted")
        finally:
            await response.aclose()


class GetDailyPushReportOperation:
    """Read one prior-day aggregate push report without sending messages."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Getui client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated daily report Query identity."""
        return GET_DAILY_PUSH_REPORT_DEFINITION

    async def invoke(
        self, call: Call, input: GetDailyPushReportInput
    ) -> QueryAttempt[GetDailyPushReportOutput]:
        """Exchange app credentials and classify one historical report response."""
        try:
            parsed_date = datetime.strptime(input.date, "%Y-%m-%d").date()
            if parsed_date.isoformat() != input.date:
                raise ValueError("noncanonical date")
        except ValueError:
            return _branch(
                "defect", FailureKind.VALIDATION, "Getui report date must be a prior YYYY-MM-DD date"
            )
        today = self.client.now().astimezone(timezone(timedelta(hours=8))).date()
        if parsed_date >= today:
            return _branch(
                "defect", FailureKind.VALIDATION, "Getui report date must be a prior YYYY-MM-DD date"
            )
        try:
            credentials = await self.client.credentials.resolve(call)
            if (
                not re.fullmatch(r"[A-Za-z0-9_-]{1,128}", credentials.app_id)
                or not _valid_opaque(credentials.app_key.reveal())
                or not _valid_opaque(credentials.master_secret.reveal())
            ):
                raise ValueError("invalid credentials")
        except (ValueError, LookupError, OSError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Getui app credentials are unavailable or invalid"
            )
        token, error = await self.client.token_for(credentials)
        if error is not None:
            return error
        request = self.client.http_client.build_request(
            "GET",
            _BASE + credentials.app_id + "/report/push/date/" + input.date,
            headers={"Accept": "application/json", "token": token},
        )
        body, error = await self.client._request(request, "report")
        if error is not None:
            return error
        assert body is not None
        return self._decode_report(body, input, credentials)

    def _decode_report(
        self, body: bytes, input: GetDailyPushReportInput, credentials: Credentials
    ) -> QueryAttempt[GetDailyPushReportOutput]:
        try:
            wire: Any = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Getui returned invalid report data")
        if not isinstance(wire, dict) or type(wire.get("code")) is not int:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Getui returned invalid report data")
        code = wire["code"]
        if code == 10001:
            cache_key = hashlib.sha256(
                (
                    credentials.app_id
                    + "\x00"
                    + credentials.app_key.reveal()
                    + "\x00"
                    + credentials.master_secret.reveal()
                ).encode()
            ).hexdigest()
            self.client.tokens.pop(cache_key, None)
            return _retry(FailureKind.AUTHENTICATION, "Getui token expired; renew authentication")
        if code != 0:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"Getui rejected the report with code {code}",
            )
        data = wire.get("data")
        if not isinstance(data, dict) or set(data) != {input.date}:
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Getui returned an incomplete daily report"
            )
        item = data[input.date]
        total = item.get("total") if isinstance(item, dict) else None
        fields = ("request_num", "msg_num", "target_num", "receive_num", "display_num", "click_num")
        if (
            not isinstance(total, dict)
            or any(type(total.get(field)) is not int for field in fields)
            or any(total[field] < (-1 if field == "display_num" else 0) for field in fields)
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Getui returned incomplete report totals")
        return QueryAttempt.branch_result(
            "found",
            GetDailyPushReportOutput(
                date=input.date,
                request_count=total["request_num"],
                eligible_message_count=total["msg_num"],
                sent_count=total["target_num"],
                received_count=total["receive_num"],
                displayed_count=total["display_num"],
                clicked_count=total["click_num"],
            ),
            Receipt(),
        )
