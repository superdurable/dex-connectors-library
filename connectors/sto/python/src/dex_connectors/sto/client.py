# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""STO provider transport and Go-compatible query decisions."""

import base64
import json
from datetime import UTC, datetime
from hashlib import md5
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
from ._generated.definitions import QUERY_WAYBILL_TRACE_DEFINITION
from .models import QueryWaybillTraceInput, WaybillEvent, WaybillTrace


def _opaque(value: str, limit: int = 4096) -> bool:
    return isinstance(value, str) and 0 < len(value) <= limit and all(0x20 < ord(c) < 0x7F for c in value)


class Client:
    """Resolve STO credentials for each waybill query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or not 0 < config.max_response_bytes <= 16 << 20
        ):
            raise ValueError("STO configuration and credentials are required")
        parsed = httpx.URL(config.base_url)
        if parsed.scheme != "https" or parsed.path != "/gateway/link.do" or parsed.query or parsed.fragment:
            raise ValueError("STO gateway URL must end in /gateway/link.do")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None

    def query_waybill_trace(self) -> "QueryWaybillTraceOperation":
        """Bind the queryWaybillTrace query."""
        return QueryWaybillTraceOperation(self)

    async def aclose(self) -> None:
        """Close the owned transport."""
        if self._owns_http_client:
            await self.http_client.aclose()


class QueryWaybillTraceOperation:
    """Query STO and classify the provider outcome."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated query definition."""
        return QUERY_WAYBILL_TRACE_DEFINITION

    def _failure(self, kind: FailureKind) -> Failure:
        return Failure(kind, "sto", "queryWaybillTrace", "STO request failed")

    def _branch(self, branch: str, kind: FailureKind) -> QueryAttempt[WaybillTrace]:
        return QueryAttempt.branch_result(branch, WaybillTrace(), Receipt(), self._failure(kind))

    def _retry(self, kind: FailureKind, delay: float = 0) -> QueryAttempt[WaybillTrace]:
        return QueryAttempt.retry(self._failure(kind), delay)

    async def invoke(self, call: Call, input: QueryWaybillTraceInput) -> QueryAttempt[WaybillTrace]:
        """Read tracking scans without persisting credentials or personal details."""
        if not isinstance(input, QueryWaybillTraceInput) or not _opaque(input.waybill_no, 128):
            return self._branch("defect", FailureKind.VALIDATION)
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError, AttributeError):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        if (
            not _opaque(credentials.app_key)
            or not _opaque(credentials.app_secret.reveal())
            or not _opaque(credentials.from_code)
        ):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        content = json.dumps({"waybillNoList": [input.waybill_no]}, separators=(",", ":")).encode()
        signature = base64.b64encode(
            md5(content + credentials.app_secret.reveal().encode(), usedforsecurity=False).digest()
        ).decode()
        fields = {
            "api_name": "STO_TRACE_QUERY_COMMON",
            "content": content.decode(),
            "from_appkey": credentials.app_key,
            "from_code": credentials.from_code,
            "to_appkey": "sto_trace_query",
            "to_code": "sto_trace_query",
            "data_digest": signature,
        }
        request = self.client.http_client.build_request(
            "POST",
            self.client.config.base_url,
            content=urlencode(sorted(fields.items())),
            headers={"Accept": "application/json", "Content-Type": "application/x-www-form-urlencoded"},
        )
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return self._retry(FailureKind.TRANSPORT)
        try:
            status = response.status_code
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return self._retry(FailureKind.RATE_LIMIT, delay)
            if status == 408 or status >= 500:
                return self._retry(FailureKind.AVAILABILITY, delay)
            try:
                body = await read_bounded_body(response, self.client.config.max_response_bytes)
            except BodyTooLargeError:
                return self._branch("invalidResponse", FailureKind.RESPONSE_TOO_LARGE)
            except ProviderTransportError:
                return self._retry(FailureKind.TRANSPORT)
            if status != 200:
                return self._branch("providerRejected", FailureKind.PROVIDER_REJECTION)
        finally:
            await response.aclose()
        try:
            wire = json.loads(body)
        except (UnicodeDecodeError, ValueError):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if not isinstance(wire, dict) or type(wire.get("success")) is not bool:
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if not wire["success"]:
            if wire.get("needRetry") is True or wire.get("errorCode") in (
                "S04",
                "S06",
                "S07",
                "S20",
                "S30",
                "S69",
                "S302",
            ):
                return self._retry(FailureKind.AVAILABILITY)
            return self._branch("providerRejected", FailureKind.PROVIDER_REJECTION)
        data = wire.get("data")
        if not isinstance(data, dict) or len(data) > 1 or any(number != input.waybill_no for number in data):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        scans = data.get(input.waybill_no, [])
        if not isinstance(scans, list) or len(scans) > 1000:
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        events = []
        for scan in scans:
            if (
                not isinstance(scan, dict)
                or scan.get("waybillNo") not in (None, "", input.waybill_no)
                or not isinstance(scan.get("scanType"), str)
                or not scan["scanType"]
                or not isinstance(scan.get("opTime"), str)
                or not scan["opTime"]
                or not isinstance(scan.get("opOrgName", ""), str)
            ):
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            events.append(
                WaybillEvent(
                    scan_type=scan["scanType"], op_time=scan["opTime"], op_org_name=scan.get("opOrgName", "")
                )
            )
        return QueryAttempt.branch_result(
            "found", WaybillTrace(waybill_no=input.waybill_no, events=tuple(events)), Receipt()
        )
