# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read the authorized Youzan shop with Go-compatible provider outcomes."""

import json
from datetime import UTC, datetime
from typing import Any
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
from ._generated.definitions import GET_SHOP_BASIC_DEFINITION
from .models import GetShopBasicInput, ShopBasic


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "youzan", "getShopBasic", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ShopBasic]:
    return QueryAttempt.branch_result(branch, ShopBasic(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ShopBasic]:
    return QueryAttempt.retry(_failure(kind, message), delay)


class Client:
    """Resolve a rotated shop token for each read and own an optional HTTP transport."""

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
            or config.max_response_bytes <= 0
        ):
            raise ValueError("Youzan configuration and credentials are required")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None

    def get_shop_basic(self) -> "GetShopBasicOperation":
        """Bind the shop metadata Query."""
        return GetShopBasicOperation(self)

    async def aclose(self) -> None:
        """Close a connector-owned HTTP transport."""
        if self._owns_http_client:
            await self.http_client.aclose()


class GetShopBasicOperation:
    """Read the shop represented by the current access token."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query definition."""
        return GET_SHOP_BASIC_DEFINITION

    async def invoke(self, call: Call, input: GetShopBasicInput) -> QueryAttempt[ShopBasic]:
        """Classify transport, envelope, and business outcomes."""
        try:
            credentials = await self.client.credentials.resolve(call)
            token = credentials.access_token.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Youzan access token is unavailable or invalid"
            )
        if (
            not token
            or len(token) > 512
            or token.strip() != token
            or any(ord(c) < 32 or ord(c) == 127 for c in token)
        ):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Youzan access token is unavailable or invalid"
            )
        url = self.client.base_url + "/api/youzan.shop.basic.get/3.0.0?" + urlencode({"access_token": token})
        try:
            request = self.client.http_client.build_request(
                "POST", url, json={}, headers={"Accept": "application/json"}
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(FailureKind.TRANSPORT, "Youzan request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Youzan rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Youzan is temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Youzan response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.TRANSPORT, "Youzan response was interrupted")
            if status != 200:
                return _branch(
                    "providerRejected", FailureKind.PROVIDER_REJECTION, f"Youzan rejected HTTP {status}"
                )
            try:
                wire: Any = json.loads(body.decode("utf-8"))
            except (ValueError, UnicodeDecodeError):
                return _branch("invalidResponse", FailureKind.PROTOCOL, "Youzan returned invalid shop JSON")
            if not isinstance(wire, dict) or "success" not in wire or type(wire["success"]) is not bool:
                return _branch("invalidResponse", FailureKind.PROTOCOL, "Youzan omitted success status")
            if not wire["success"]:
                if wire.get("code") == 161400100:
                    return _retry(FailureKind.AVAILABILITY, "Youzan reported a system error", delay)
                return _branch(
                    "providerRejected",
                    FailureKind.PROVIDER_REJECTION,
                    f"Youzan rejected code {wire.get('code', 0)}",
                )
            data = wire.get("data")
            if (
                wire.get("code") != 200
                or not isinstance(data, dict)
                or type(data.get("sid")) is not int
                or data["sid"] <= 0
                or not isinstance(data.get("name"), str)
                or not data["name"]
            ):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "Youzan returned incomplete shop information"
                )
            output = ShopBasic(
                shop_id=data["sid"],
                name=data["name"],
                logo=data.get("logo", ""),
                url=data.get("url", ""),
                role=data.get("role", 0),
                line_of_business=data.get("line_of_business", 0),
            )
            return QueryAttempt.branch_result("found", output, Receipt())
        finally:
            await response.aclose()
