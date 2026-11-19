# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Yunda provider transport and Go-compatible Query decisions."""

import json
from collections.abc import Callable
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
from ._generated.definitions import GET_WAYBILL_BALANCE_DEFINITION
from .models import GetWaybillBalanceInput, GetWaybillBalanceOutput


def _opaque(value: str, limit: int = 1024) -> bool:
    return (
        isinstance(value, str) and 0 < len(value) <= limit and all(0x20 < ord(char) < 0x7F for char in value)
    )


class Client:
    """Resolve credentials for each getWaybillBalance request."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        clock: Callable[[], datetime] | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or not 0 < config.max_response_bytes <= 16 << 20
        ):
            raise ValueError("Yunda configuration and credentials are required")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None
        self.clock = clock or (lambda: datetime.now(UTC))

    def get_waybill_balance(self) -> "GetWaybillBalanceOperation":
        """Bind the getWaybillBalance Query."""
        return GetWaybillBalanceOperation(self)

    async def aclose(self) -> None:
        """Close the owned HTTP transport."""
        if self._owns_http_client:
            await self.http_client.aclose()


class GetWaybillBalanceOperation:
    """Query Yunda and classify the provider outcome."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query definition."""
        return GET_WAYBILL_BALANCE_DEFINITION

    def _branch(self, branch: str, kind: FailureKind) -> QueryAttempt[GetWaybillBalanceOutput]:
        return QueryAttempt.branch_result(
            branch,
            GetWaybillBalanceOutput(),
            Receipt(),
            Failure(kind, "yunda", "getWaybillBalance", "Yunda request failed"),
        )

    def _retry(self, kind: FailureKind, delay: float = 0) -> QueryAttempt[GetWaybillBalanceOutput]:
        return QueryAttempt.retry(Failure(kind, "yunda", "getWaybillBalance", "Yunda request failed"), delay)

    def _request(
        self, input: GetWaybillBalanceInput, credentials: Credentials
    ) -> httpx.Request | QueryAttempt[GetWaybillBalanceOutput]:
        values = (
            credentials.app_key.reveal(),
            credentials.app_secret.reveal(),
            credentials.partner_id.reveal(),
            credentials.baima_secret.reveal(),
        )
        if any(not _opaque(value) for value in values):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        app_key, app_secret, partner_id, baima_secret = values
        body = json.dumps(
            {"appid": app_key, "partner_id": partner_id, "secret": baima_secret, "type": "common"},
            separators=(",", ":"),
        ).encode()
        signature = md5(body + b"_" + app_secret.encode(), usedforsecurity=False).hexdigest()
        request_time = str(int(self.client.clock().timestamp() * 1000))
        return self.client.http_client.build_request(
            "POST",
            "https://openapi.yundaex.com/openapi-api/v1/accountOrder/searchCount",
            content=body,
            headers={
                "Accept": "application/json",
                "Content-Type": "application/json",
                "app-key": app_key,
                "sign": signature,
                "req-time": request_time,
            },
        )

    def _decode(
        self, wire: object, input: GetWaybillBalanceInput
    ) -> GetWaybillBalanceOutput | QueryAttempt[GetWaybillBalanceOutput] | None:
        if (
            not isinstance(wire, dict)
            or not isinstance(wire.get("code"), str)
            or not isinstance(wire.get("result"), bool)
        ):
            return None
        if wire["code"] == "7777":
            return self._retry(FailureKind.AVAILABILITY)
        if wire["code"] != "0000" or not wire["result"]:
            return self._branch("providerRejected", FailureKind.PROVIDER_REJECTION)
        data = wire.get("data")
        if not isinstance(data, dict) or data.get("status") != "1":
            return None
        counts = []
        for field in ("free_num", "remain_num"):
            value = data.get(field)
            if not isinstance(value, str) or not value or len(value) > 19:
                return None
            try:
                count = int(value, 10)
            except ValueError:
                return None
            if count < 0 or count > 9223372036854775807:
                return None
            counts.append(count)
        return GetWaybillBalanceOutput(free_count=counts[0], paid_count=counts[1])

    async def invoke(
        self, call: Call, input: GetWaybillBalanceInput
    ) -> QueryAttempt[GetWaybillBalanceOutput]:
        """Read one bounded provider response without persisting credentials."""
        if not isinstance(input, GetWaybillBalanceInput):
            return self._branch("defect", FailureKind.VALIDATION)
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError, AttributeError):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        request = self._request(input, credentials)
        if isinstance(request, QueryAttempt):
            return request
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return self._retry(FailureKind.AVAILABILITY)
        try:
            status = response.status_code
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return self._retry(FailureKind.RATE_LIMIT, delay)
            if status == 408 or status >= 500:
                return self._retry(FailureKind.AVAILABILITY, delay)
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return self._branch("providerRejected", kind)
            try:
                body = await read_bounded_body(response, self.client.config.max_response_bytes)
            except BodyTooLargeError:
                return self._branch("invalidResponse", FailureKind.RESPONSE_TOO_LARGE)
            except ProviderTransportError:
                return self._retry(FailureKind.AVAILABILITY)
        finally:
            await response.aclose()
        try:
            wire = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        result = self._decode(wire, input)
        if isinstance(result, QueryAttempt):
            return result
        if result is None:
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        return QueryAttempt.branch_result("found", result, Receipt())
