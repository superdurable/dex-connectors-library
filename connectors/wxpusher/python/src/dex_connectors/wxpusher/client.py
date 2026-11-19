# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider transport and Go-compatible Query decisions."""

import json
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
from ._generated.definitions import GET_SUBSCRIBER_COUNT_DEFINITION
from .models import GetSubscriberCountInput, SubscriberCountResult

_PROVIDER = "wxpusher"
_OPERATION = "getSubscriberCount"


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[SubscriberCountResult]:
    return QueryAttempt.branch_result(
        branch, SubscriberCountResult(), Receipt(), Failure(kind, _PROVIDER, _OPERATION, message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[SubscriberCountResult]:
    return QueryAttempt.retry(Failure(kind, _PROVIDER, _OPERATION, message), delay)


class Client:
    """Own the provider HTTP transport and resolve rotating credentials per Query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("WxPusher configuration and credentials are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("WxPusher configuration is invalid")
        from dex_connectors.sdk.providerhttp import validate_base_url

        base_url = validate_base_url(config.base_url)
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self.base_url = base_url

    def get_subscriber_count(self) -> "GetSubscriberCountOperation":
        """Bind the getSubscriberCount Query."""
        return GetSubscriberCountOperation(self)

    async def aclose(self) -> None:
        """Close this client's own transport."""
        if self._owns_client:
            await self.http_client.aclose()


class GetSubscriberCountOperation:
    """Run the getSubscriberCount provider Query."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("WxPusher client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity."""
        return GET_SUBSCRIBER_COUNT_DEFINITION

    async def invoke(self, call: Call, input: GetSubscriberCountInput) -> QueryAttempt[SubscriberCountResult]:
        """Send one request and classify its provider outcome."""
        prepared = await _prepare(self.client, call, input)
        if isinstance(prepared, QueryAttempt):
            return prepared
        try:
            response, _ = await send_traced(self.client.http_client, prepared)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "WxPusher subscriber request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "WxPusher rate limited subscriber query", delay)
            if status == 408 or status >= 500:
                return _retry(
                    FailureKind.AVAILABILITY, "WxPusher subscriber API is temporarily unavailable", delay
                )
            if status != 200 and not True:
                return _http_rejection(status)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "WxPusher response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "WxPusher subscriber response was interrupted")
            if status != 200:
                return _http_rejection(status)
        finally:
            await response.aclose()
        return _decode(body, input, delay)


def _valid_token(value: str) -> bool:
    return value.startswith("AT_") and len(value) <= 256 and all(33 <= ord(c) <= 126 for c in value)


async def _prepare(
    client: Client, call: Call, input: GetSubscriberCountInput
) -> httpx.Request | QueryAttempt[SubscriberCountResult]:
    try:
        credentials = await client.credentials.resolve(call)
        token = credentials.app_token.reveal()
        if not _valid_token(token):
            raise ValueError("invalid token")
    except (ValueError, LookupError, OSError):
        return _branch(
            "defect", FailureKind.AUTHENTICATION, "WxPusher application token is unavailable or invalid"
        )
    try:
        return client.http_client.build_request(
            "GET",
            client.base_url + "/api/fun/wxuser/v2",
            params={"appToken": token, "page": "1", "pageSize": "1"},
            headers={"Accept": "application/json"},
        )
    except (httpx.HTTPError, ValueError):
        return _branch("defect", FailureKind.VALIDATION, "WxPusher subscriber request is invalid")


def _http_rejection(status: int) -> QueryAttempt[SubscriberCountResult]:
    kind = FailureKind.PROVIDER_REJECTION
    if status == 401:
        kind = FailureKind.AUTHENTICATION
    elif status == 403:
        kind = FailureKind.AUTHORIZATION
    elif status in (400, 404, 412):
        kind = FailureKind.VALIDATION
    return _branch("providerRejected", kind, f"WxPusher rejected subscriber query with HTTP {status}")


def _decode(body: bytes, input: GetSubscriberCountInput, delay: float) -> QueryAttempt[SubscriberCountResult]:
    try:
        document: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        document = None
    if not isinstance(document, dict) or type(document.get("code")) is not int:
        return _branch(
            "invalidResponse", FailureKind.PROTOCOL, "WxPusher returned an invalid subscriber response"
        )
    if document["code"] != 1000:
        return _branch(
            "providerRejected", FailureKind.PROVIDER_REJECTION, "WxPusher rejected subscriber query"
        )
    data = document.get("data")
    if (
        document.get("success") is not True
        or not isinstance(data, dict)
        or type(data.get("total")) is not int
        or data["total"] < 0
        or data.get("page") != 1
        or data.get("pageSize") != 1
        or "records" not in data
    ):
        return _branch(
            "invalidResponse", FailureKind.PROTOCOL, "WxPusher returned an invalid subscriber response"
        )
    return QueryAttempt.branch_result("counted", SubscriberCountResult(data["total"]), Receipt())
