# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Cainiao TOP waybill trace Query with Go-compatible request signing and decisions."""

import hashlib
import hmac
import json
from collections.abc import Callable
from datetime import UTC, datetime, timedelta, timezone
from urllib.parse import urlencode, urlsplit

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
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import (
    DEFECT,
    FOUND,
    INVALID_RESPONSE,
    PROVIDER_REJECTED,
    QUERY_WAYBILL_TRACE_DEFINITION,
)
from .models import QueryWaybillTraceInput, WaybillEvent, WaybillTrace


class Client:
    """Own TOP router settings and resolve the merchant SessionKey for each Query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        *,
        clock: Callable[[], datetime] = lambda: datetime.now(UTC),
    ) -> None:
        if config is None or credentials is None or clock is None:
            raise ValueError("Cainiao configuration, credentials, and clock are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("Cainiao timeout and response limit must be positive")
        base_url = validate_base_url(config.base_url)
        parsed = urlsplit(base_url)
        if parsed.path not in ("", "/router/rest") or parsed.query or parsed.fragment:
            raise ValueError("Cainiao TOP URL must be a router endpoint without query parameters")
        self.base_url = base_url
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self.clock = clock

    def query_waybill_trace(self) -> "QueryWaybillTraceOperation":
        """Bind the electronic-waybill trace Query."""
        return QueryWaybillTraceOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after Worker shutdown."""
        await self.http_client.aclose()


class QueryWaybillTraceOperation:
    """Read one authorized electronic waybill's bounded event history."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Cainiao client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest Query contract."""
        return QUERY_WAYBILL_TRACE_DEFINITION

    async def invoke(self, call: Call, input: QueryWaybillTraceInput) -> QueryAttempt[WaybillTrace]:
        """Sign one TOP request and classify its provider result."""
        if (
            not isinstance(input, QueryWaybillTraceInput)
            or not _waybill_field(input.carrier_code, 32)
            or not _waybill_field(input.waybill_code, 128)
        ):
            return _branch(DEFECT, FailureKind.VALIDATION, "Cainiao waybill input is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "Cainiao credentials are unavailable or invalid"
            )
        if not isinstance(credentials, Credentials):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "Cainiao credentials are unavailable or invalid"
            )
        app_key, app_secret, session_key = (
            credentials.app_key,
            credentials.app_secret.reveal(),
            credentials.session_key.reveal(),
        )
        if not all(_opaque_credential(value) for value in (app_key, app_secret, session_key)):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "Cainiao credentials are unavailable or invalid"
            )
        timestamp = self.client.clock().astimezone(timezone(timedelta(hours=8))).strftime("%Y-%m-%d %H:%M:%S")
        fields = {
            "app_key": app_key,
            "method": "cainiao.waybill.logisticstrace.get",
            "format": "json",
            "session": session_key,
            "sign_method": "hmac",
            "timestamp": timestamp,
            "v": "2.0",
            "cp_code": input.carrier_code,
            "waybill_code": input.waybill_code,
        }
        fields["sign"] = _sign_request(app_secret, fields)
        try:
            request = self.client.http_client.build_request(
                "POST",
                self.client.base_url,
                headers={
                    "Content-Type": "application/x-www-form-urlencoded; charset=utf-8",
                    "Accept": "application/json",
                },
                content=urlencode(sorted(fields.items())).encode(),
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(FailureKind.TRANSPORT, "Cainiao request failed")
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), self.client.clock())
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Cainiao rate limited the query", retry_after)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Cainiao is temporarily unavailable", retry_after)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Cainiao response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.TRANSPORT, "Cainiao response was interrupted")
            if status != 200:
                return _branch(
                    PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION, f"Cainiao rejected HTTP {status}"
                )
            try:
                return _decode_trace(body, input)
            except (UnicodeDecodeError, ValueError, TypeError):
                return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "Cainiao returned invalid trace JSON")
        finally:
            await response.aclose()


def _sign_request(app_secret: str, fields: dict[str, str]) -> str:
    canonical = "".join(key + fields[key] for key in sorted(fields) if key != "sign" and fields[key])
    return hmac.new(app_secret.encode(), canonical.encode(), hashlib.md5).hexdigest().upper()  # noqa: S324 - TOP HMAC-MD5 wire contract


def _opaque_credential(value: object) -> bool:
    return (
        isinstance(value, str) and 0 < len(value) <= 4096 and all(0x21 <= ord(char) <= 0x7E for char in value)
    )


def _waybill_field(value: object, maximum: int) -> bool:
    return (
        isinstance(value, str)
        and 0 < len(value.encode("utf-8")) <= maximum
        and all(0x21 <= ord(char) <= 0x7E for char in value)
    )


def _decode_trace(body: bytes, input: QueryWaybillTraceInput) -> QueryAttempt[WaybillTrace]:
    text = body.decode("utf-8")
    decoder = json.JSONDecoder(parse_constant=lambda _: (_ for _ in ()).throw(ValueError("invalid number")))
    document, end = decoder.raw_decode(text.lstrip())
    if text.lstrip()[end:].strip():
        return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "Cainiao returned trailing JSON")
    if not isinstance(document, dict):
        raise ValueError("invalid TOP response")
    error = document.get("error_response")
    success = document.get("cainiao_waybill_logisticstrace_get_response")
    if error is not None:
        if success is not None:
            return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "Cainiao returned conflicting outcomes")
        if not isinstance(error, dict) or type(error.get("code")) not in (int, float):
            return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "Cainiao returned incomplete error")
        code = str(error["code"])
        return _branch(PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION, "Cainiao rejected code " + code)
    if not isinstance(success, dict) or not isinstance(success.get("logistics_detail_dto_list"), dict):
        return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "Cainiao returned incomplete trace")
    events = success["logistics_detail_dto_list"].get("logistics_detail_dto")
    if not isinstance(events, list) or len(events) > 1000:
        return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "Cainiao returned incomplete trace")
    mapped = []
    for event in events:
        if not isinstance(event, dict) or any(
            not isinstance(event.get(field), str) or not event[field]
            for field in ("action", "description", "time")
        ):
            return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "Cainiao returned incomplete event")
        mapped.append(WaybillEvent(event["action"], event["description"], event["time"]))
    return QueryAttempt.branch_result(
        FOUND, WaybillTrace(input.carrier_code, input.waybill_code, tuple(mapped)), Receipt()
    )


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[WaybillTrace]:
    return QueryAttempt.branch_result(
        branch, WaybillTrace(), Receipt(), Failure(kind, "cainiao", "queryWaybillTrace", message)
    )


def _retry(kind: FailureKind, message: str, retry_after: float = 0) -> QueryAttempt[WaybillTrace]:
    return QueryAttempt.retry(Failure(kind, "cainiao", "queryWaybillTrace", message), retry_after)
