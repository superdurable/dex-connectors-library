# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""ZTO provider transport and Go-compatible query decisions."""

import base64
import json
from datetime import UTC, datetime
from hashlib import md5

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
    """Resolve ZTO credentials for each waybill query."""

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
            raise ValueError("ZTO configuration and credentials are required")
        parsed = httpx.URL(config.base_url)
        if parsed.scheme != "https" or parsed.path not in ("", "/") or parsed.query or parsed.fragment:
            raise ValueError("ZTO API URL must be an HTTPS origin")
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
    """Query ZTO and classify the provider outcome."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated query definition."""
        return QUERY_WAYBILL_TRACE_DEFINITION

    def _failure(self, kind: FailureKind) -> Failure:
        return Failure(kind, "zto", "queryWaybillTrace", "ZTO request failed")

    def _branch(self, branch: str, kind: FailureKind) -> QueryAttempt[WaybillTrace]:
        return QueryAttempt.branch_result(branch, WaybillTrace(), Receipt(), self._failure(kind))

    def _retry(self, kind: FailureKind, delay: float = 0) -> QueryAttempt[WaybillTrace]:
        return QueryAttempt.retry(self._failure(kind), delay)

    async def invoke(self, call: Call, input: QueryWaybillTraceInput) -> QueryAttempt[WaybillTrace]:
        """Read tracking scans without persisting credentials or personal details."""
        if not isinstance(input, QueryWaybillTraceInput) or not _opaque(input.bill_code, 128):
            return self._branch("defect", FailureKind.VALIDATION)
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError, AttributeError):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        if not _opaque(credentials.app_key) or not _opaque(credentials.app_secret.reveal()):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        body = json.dumps({"billCode": input.bill_code}, separators=(",", ":")).encode()
        signature = base64.b64encode(
            md5(body + credentials.app_secret.reveal().encode(), usedforsecurity=False).digest()
        ).decode()
        request = self.client.http_client.build_request(
            "POST",
            self.client.config.base_url.rstrip("/") + "/zto.merchant.waybill.track.query",
            content=body,
            headers={
                "Accept": "application/json",
                "Content-Type": "application/json",
                "x-appKey": credentials.app_key,
                "x-dataDigest": signature,
            },
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
        if (
            not isinstance(wire, dict)
            or type(wire.get("status")) is not bool
            or not isinstance(wire.get("statusCode"), str)
            or not wire["statusCode"]
        ):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if not wire["status"]:
            if wire["statusCode"] == "E500":
                return self._retry(FailureKind.AVAILABILITY)
            return self._branch("providerRejected", FailureKind.PROVIDER_REJECTION)
        scans = wire.get("result")
        if wire["statusCode"] != "SYS000" or not isinstance(scans, list) or len(scans) > 1000:
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        events = []
        for scan in scans:
            if (
                not isinstance(scan, dict)
                or scan.get("billCode") not in (None, "", input.bill_code)
                or not isinstance(scan.get("scanType"), str)
                or not scan["scanType"]
                or type(scan.get("scanDate")) is not int
                or scan["scanDate"] <= 0
            ):
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            site = scan.get("scanSite")
            if site is not None and (not isinstance(site, dict) or not isinstance(site.get("name"), str)):
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            events.append(
                WaybillEvent(
                    scan_type=scan["scanType"],
                    scan_date=scan["scanDate"],
                    scan_site_name=site["name"] if site else "",
                )
            )
        return QueryAttempt.branch_result(
            "found", WaybillTrace(bill_code=input.bill_code, events=tuple(events)), Receipt()
        )
