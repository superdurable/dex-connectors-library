# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""SF Express signed route Query."""

from __future__ import annotations

import base64
import hashlib
import json
import os
from collections.abc import Callable
from datetime import UTC, datetime
from typing import Any
from urllib.parse import quote_plus
from uuid import UUID

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
from ._generated.definitions import QUERY_ROUTES_DEFINITION
from .models import QueryRoutesInput, RouteEvent, RouteQueryResult


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[RouteQueryResult]:
    return QueryAttempt.branch_result(
        branch, RouteQueryResult(), Receipt(), Failure(kind, "sfexpress", "queryRoutes", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[RouteQueryResult]:
    return QueryAttempt.retry(Failure(kind, "sfexpress", "queryRoutes", message), delay)


def _classify_status(status: int) -> FailureKind:
    return {
        401: FailureKind.AUTHENTICATION,
        403: FailureKind.AUTHORIZATION,
        400: FailureKind.VALIDATION,
        404: FailureKind.VALIDATION,
        412: FailureKind.VALIDATION,
    }.get(status, FailureKind.PROVIDER_REJECTION)


class Client:
    """Own immutable route settings and resolve credentials on each attempt."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        *,
        clock: Callable[[], datetime] | None = None,
        request_id_source: Callable[[int], bytes] | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or config.max_response_bytes <= 0
        ):
            raise ValueError("SF Express configuration and credential source are required")
        self.base_url = validate_base_url(config.base_url)
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self.clock = clock or (lambda: datetime.now(UTC))
        self.request_id_source = request_id_source or os.urandom

    def query_routes(self) -> QueryRoutesOperation:
        """Bind the route Query to this client."""
        return QueryRoutesOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP transport if this client owns it."""
        if self._owns_client:
            await self.http_client.aclose()


class QueryRoutesOperation:
    """Read one owned waybill's standard route history."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("SF Express client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity and retry policy."""
        return QUERY_ROUTES_DEFINITION

    async def invoke(self, call: Call, input: QueryRoutesInput) -> QueryAttempt[RouteQueryResult]:
        """Sign one route request and classify the provider response."""
        waybill = input.waybill_number if isinstance(input, QueryRoutesInput) else ""
        if not 8 <= len(waybill) <= 30 or any(not ("0" <= c <= "9" or "A" <= c <= "Z") for c in waybill):
            return _branch("defect", FailureKind.VALIDATION, "SF Express waybill number is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            partner_id, checkword = credentials.partner_id, credentials.checkword.reveal()
            if (
                not partner_id
                or len(partner_id) > 64
                or any(not 32 < ord(c) < 127 for c in partner_id)
                or not checkword
                or len(checkword) > 2048
                or checkword.strip() != checkword
            ):
                raise ValueError("invalid credentials")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "SF Express credentials are unavailable or invalid"
            )
        message = json.dumps(
            {"language": "0", "trackingType": "1", "trackingNumber": [waybill], "methodType": "1"},
            separators=(",", ":"),
            ensure_ascii=False,
        )
        timestamp = str(int(self.client.clock().timestamp()))
        try:
            random_bytes = self.client.request_id_source(16)
            if len(random_bytes) != 16:
                raise ValueError("invalid request ID bytes")
        except (ValueError, OSError):
            return _retry(FailureKind.AVAILABILITY, "SF Express request ID generation failed")
        request_id = str(UUID(bytes=random_bytes, version=4))
        digest = base64.b64encode(
            hashlib.md5(quote_plus(message + timestamp + checkword, safe="").encode()).digest()
        ).decode()
        form = {
            "partnerID": partner_id,
            "requestID": request_id,
            "serviceCode": "EXP_RECE_SEARCH_ROUTES",
            "timestamp": timestamp,
            "msgData": message,
            "msgDigest": digest,
        }
        try:
            request = self.client.http_client.build_request(
                "POST",
                f"{self.client.base_url}/std/service",
                data=form,
                headers={
                    "Content-Type": "application/x-www-form-urlencoded; charset=UTF-8",
                    "Accept": "application/json",
                },
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "SF Express route request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "SF Express route request failed")
        try:
            status = response.status_code
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "SF Express rate limited route query", delay)
            if status == 408 or status >= 500:
                return _retry(
                    FailureKind.AVAILABILITY, "SF Express route API is temporarily unavailable", delay
                )
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "SF Express route response exceeded configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "SF Express route response was interrupted")
        finally:
            await response.aclose()
        if status != 200:
            return _branch("providerRejected", _classify_status(status), "SF Express rejected route query")
        try:
            envelope: Any = json.loads(body)
            if (
                not isinstance(envelope, dict)
                or not isinstance(envelope.get("apiResultCode"), str)
                or not envelope["apiResultCode"]
            ):
                raise ValueError("invalid envelope")
            code = envelope["apiResultCode"]
            if code in ("A1005", "A1009", "A1099"):
                return _retry(
                    FailureKind.AVAILABILITY, "SF Express route service is temporarily unavailable", delay
                )
            if code != "A1000":
                return _branch(
                    "providerRejected", FailureKind.PROVIDER_REJECTION, "SF Express rejected route query"
                )
            encoded = envelope.get("apiResultData")
            if not isinstance(encoded, str):
                raise ValueError("invalid business payload")
            business = json.loads(encoded)
            if not isinstance(business, dict) or type(business.get("success")) is not bool:
                raise ValueError("invalid business response")
            if not business["success"]:
                if business.get("errorCode") in ("A1005", "A1009", "A1099"):
                    return _retry(
                        FailureKind.AVAILABILITY, "SF Express route service is temporarily unavailable", delay
                    )
                return _branch(
                    "providerRejected", FailureKind.PROVIDER_REJECTION, "SF Express rejected route query"
                )
            data = business.get("msgData")
            responses = data.get("routeResps") if isinstance(data, dict) else None
            if (
                not isinstance(responses, list)
                or len(responses) != 1
                or not isinstance(responses[0], dict)
                or responses[0].get("mailNo") != waybill
            ):
                raise ValueError("invalid route list")
            wire_events = responses[0].get("routes")
            if not isinstance(wire_events, list) or len(wire_events) > 100:
                raise ValueError("invalid route events")
            events = []
            for event in wire_events:
                if not isinstance(event, dict) or any(
                    not isinstance(event.get(key), str) or not event[key]
                    for key in ("acceptTime", "remark", "opcode")
                ):
                    raise ValueError("invalid route event")
                events.append(RouteEvent(occurred_at=event["acceptTime"], opcode=event["opcode"]))
            return QueryAttempt.branch_result(
                "found", RouteQueryResult(waybill_number=waybill, routes=tuple(events)), Receipt()
            )
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "SF Express returned an invalid route response"
            )
