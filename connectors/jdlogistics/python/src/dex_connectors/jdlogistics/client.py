# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""JD Logistics merchant waybill Query with Go-compatible signing and decisions."""

import hashlib
import json
from collections.abc import Callable
from datetime import UTC, datetime, timedelta, timezone
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
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import (
    CONNECTOR_ID as CONNECTOR_ID,
)
from ._generated.definitions import (
    DEFECT as DEFECT,
)
from ._generated.definitions import (
    FOUND as FOUND,
)
from ._generated.definitions import (
    INVALID_RESPONSE as INVALID_RESPONSE,
)
from ._generated.definitions import (
    PROVIDER_REJECTED as PROVIDER_REJECTED,
)
from ._generated.definitions import (
    QUERY_WAYBILL_TRACE_DEFINITION as QUERY_WAYBILL_TRACE_DEFINITION,
)
from ._generated.definitions import (
    QUERY_WAYBILL_TRACE_OPERATION_ID,
)
from .models import QueryWaybillTraceInput, WaybillEvent, WaybillTrace

_QUERY_PATH = "/ecap/v1/orders/trace/query"
_CHINA_TIME = timezone(timedelta(hours=8))


class Client:
    """Bounded provider client resolving merchant secrets for each Query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        clock: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("JD Logistics configuration and credentials are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("JD Logistics timeout and response limit must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        parsed = httpx.URL(self.base_url)
        if parsed.path not in ("", "/") or parsed.query or parsed.fragment:
            raise ValueError("JD Logistics API URL must be a host root")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self.clock = clock or (lambda: datetime.now(UTC))

    def query_waybill_trace(self) -> "QueryWaybillTraceOperation":
        """Bind the merchant waybill Query to this client."""
        return QueryWaybillTraceOperation(self)

    async def aclose(self) -> None:
        """Close the provider transport after Worker shutdown."""
        await self.http_client.aclose()


class QueryWaybillTraceOperation:
    """Read bounded scans without retaining JD Logistics personal fields."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("JD Logistics client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query contract."""
        return QUERY_WAYBILL_TRACE_DEFINITION

    async def invoke(self, call: Call, input: QueryWaybillTraceInput) -> QueryAttempt[WaybillTrace]:
        """Sign one tracking request and classify the provider outcome."""
        if not isinstance(input, QueryWaybillTraceInput) or not _valid_opaque(input.waybill_no, 100):
            return _branch(DEFECT, FailureKind.VALIDATION, "JD Logistics waybill input is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            app_key = credentials.app_key
            app_secret = credentials.app_secret.reveal()
            access_token = credentials.access_token.reveal()
            customer_code = credentials.customer_code
        except Exception:
            return _branch(DEFECT, FailureKind.AUTHENTICATION, "JD Logistics credentials unavailable")
        if (
            not _valid_opaque(app_key, 4096)
            or not _valid_opaque(app_secret, 4096)
            or not _valid_opaque(access_token, 4096)
            or not _valid_opaque(customer_code, 32)
        ):
            return _branch(DEFECT, FailureKind.AUTHENTICATION, "JD Logistics credentials invalid")
        body = json.dumps(
            [{"waybillCode": input.waybill_no, "orderOrigin": 1, "customerCode": customer_code}],
            ensure_ascii=False,
            separators=(",", ":"),
        )
        timestamp = self.client.clock().astimezone(_CHINA_TIME).strftime("%Y-%m-%d %H:%M:%S")
        signed = (
            app_secret
            + "access_token"
            + access_token
            + "app_key"
            + app_key
            + "method"
            + _QUERY_PATH
            + "param_json"
            + body
            + "timestamp"
            + timestamp
            + "v2.0"
            + app_secret
        )
        signature = hashlib.md5(signed.encode()).hexdigest().upper()
        parameters = {
            "app_key": app_key,
            "access_token": access_token,
            "timestamp": timestamp,
            "v": "2.0",
            "LOP-DN": "ECAP",
            "sign": signature,
        }
        endpoint = self.client.base_url + _QUERY_PATH + "?" + urlencode(sorted(parameters.items()))
        try:
            request = self.client.http_client.build_request(
                "POST",
                endpoint,
                content=body.encode(),
                headers={"Accept": "application/json", "Content-Type": "application/json;charset=UTF-8"},
            )
        except (httpx.HTTPError, ValueError):
            return _retry(FailureKind.TRANSPORT, "JD Logistics request failed")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError):
            return _retry(FailureKind.TRANSPORT, "JD Logistics request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if response.status_code == 429:
                return _retry(FailureKind.RATE_LIMIT, "JD Logistics rate limited the request", delay)
            if response.status_code == 408 or response.status_code >= 500:
                return _retry(FailureKind.AVAILABILITY, "JD Logistics is unavailable", delay)
            try:
                raw_body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE, "JD Logistics response too large"
                )
            except ProviderTransportError:
                return _retry(FailureKind.TRANSPORT, "JD Logistics response interrupted")
            if response.status_code != 200:
                return _branch(
                    PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION, "JD Logistics rejected HTTP request"
                )
            try:
                document = json.loads(raw_body.decode("utf-8"), parse_constant=_invalid_constant)
                if not isinstance(document, dict):
                    raise ValueError("invalid document")
                success, code = document.get("success"), document.get("code")
                if type(success) is not bool or type(code) is not int:
                    raise ValueError("incomplete status")
                if not success or code != 0:
                    if 4000 <= code < 5000:
                        return _retry(FailureKind.AVAILABILITY, "JD Logistics transient service error")
                    return _branch(
                        PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION, "JD Logistics rejected the query"
                    )
                data = document.get("data")
                if not isinstance(data, dict):
                    raise ValueError("missing data")
                scans = data.get("traceDetails") or []
                if not isinstance(scans, list) or len(scans) > 1000:
                    raise ValueError("invalid scans")
                events: list[WaybillEvent] = []
                for scan in scans:
                    if not isinstance(scan, dict):
                        raise ValueError("invalid scan")
                    waybill_code = _go_string(scan.get("waybillCode"))
                    state = _go_string(scan.get("state"))
                    category = _go_string(scan.get("categoryName"))
                    operation_time = _go_string(scan.get("operationTime"))
                    if (waybill_code and waybill_code != input.waybill_no) or not state or not operation_time:
                        raise ValueError("invalid scan")
                    events.append(WaybillEvent(state, category, operation_time))
            except (ValueError, TypeError, UnicodeDecodeError):
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL, "JD Logistics returned invalid tracking JSON"
                )
            return QueryAttempt.branch_result(FOUND, WaybillTrace(input.waybill_no, tuple(events)), Receipt())
        finally:
            await response.aclose()


def _go_string(value: object) -> str:
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError("invalid tracking field")
    return value


def _valid_opaque(value: object, maximum: int) -> bool:
    return (
        isinstance(value, str)
        and 0 < len(value) <= maximum
        and all("!" <= character <= "~" for character in value)
    )


def _invalid_constant(value: str) -> None:
    raise ValueError(f"invalid JSON constant {value}")


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[WaybillTrace]:
    return QueryAttempt.branch_result(
        branch,
        WaybillTrace(),
        Receipt(),
        Failure(kind, CONNECTOR_ID, QUERY_WAYBILL_TRACE_OPERATION_ID, message),
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[WaybillTrace]:
    return QueryAttempt.retry(Failure(kind, CONNECTOR_ID, QUERY_WAYBILL_TRACE_OPERATION_ID, message), delay)


Config.__doc__ = "JD Logistics API origin, timeout in seconds, and response bound in bytes."
Credentials.__doc__ = "Merchant application and authorization secrets resolved for each Query."
