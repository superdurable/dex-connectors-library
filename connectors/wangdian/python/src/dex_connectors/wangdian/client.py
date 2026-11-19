# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Wangdian shop-list signing, transport, and Go-compatible Query decisions."""

import hashlib
import json
import time
from collections.abc import Callable
from datetime import UTC, datetime
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
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import LIST_SHOPS_DEFINITION
from .models import ListShopsInput, ListShopsOutput, ShopSummary

_AUTHENTICATION_CODES = {1090, 1500, 1600, 1610, 2080, 2090, 2100}
_RATE_LIMIT_CODES = {1012, 1013}
_AVAILABILITY_CODES = {1020, 1030, 1040, 1060, 1200, 1510, 1520, 1530}


def _credential(value: str) -> bool:
    return isinstance(value, str) and 0 < len(value) <= 2048 and all(0x21 <= ord(ch) <= 0x7E for ch in value)


def _integer_text(value: object) -> str | None:
    if isinstance(value, bool):
        return None
    text = str(value) if isinstance(value, (int, str)) else ""
    return text if 0 < len(text) <= 30 and text.isascii() and text.isdecimal() else None


class Client:
    """Resolve Wangdian credentials per Query and own the HTTP transport."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        clock_seconds: Callable[[], int] | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or config.max_response_bytes <= 0
        ):
            raise ValueError("Wangdian configuration and credentials are required")
        parsed = urlsplit(config.base_url)
        if (
            parsed.scheme != "https"
            or parsed.netloc != "openapi.huice.com"
            or parsed.path not in ("", "/")
            or parsed.query
            or parsed.fragment
        ):
            raise ValueError("Wangdian base URL must be the documented HTTPS origin")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None
        self.clock_seconds = clock_seconds or (lambda: int(time.time()))

    def list_shops(self) -> "ListShopsOperation":
        """Bind the listShops Query."""
        return ListShopsOperation(self)

    async def aclose(self) -> None:
        """Close the owned HTTP transport."""
        if self._owns_http_client:
            await self.http_client.aclose()


class ListShopsOperation:
    """Read one bounded page of Wangdian shop metadata."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query definition."""
        return LIST_SHOPS_DEFINITION

    def _branch(self, branch: str, kind: FailureKind) -> QueryAttempt[ListShopsOutput]:
        return QueryAttempt.branch_result(
            branch,
            ListShopsOutput(),
            Receipt(),
            Failure(kind, "wangdian", "listShops", "Wangdian shop query failed"),
        )

    def _retry(self, kind: FailureKind, delay: float = 0) -> QueryAttempt[ListShopsOutput]:
        return QueryAttempt.retry(Failure(kind, "wangdian", "listShops", "Wangdian request failed"), delay)

    def _provider_code(self, code: int) -> QueryAttempt[ListShopsOutput]:
        if code in _RATE_LIMIT_CODES:
            return self._retry(FailureKind.RATE_LIMIT)
        if code in _AVAILABILITY_CODES:
            return self._retry(FailureKind.AVAILABILITY)
        kind = FailureKind.AUTHENTICATION if code in _AUTHENTICATION_CODES else FailureKind.PROVIDER_REJECTION
        return self._branch("providerRejected", kind)

    async def invoke(self, call: Call, input: ListShopsInput) -> QueryAttempt[ListShopsOutput]:
        """Read shop identifiers and state without copying provider secrets."""
        if not isinstance(input, ListShopsInput):
            return self._branch("defect", FailureKind.VALIDATION)
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError, AttributeError):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        sid, key, secret = (
            credentials.sid.reveal(),
            credentials.app_key.reveal(),
            credentials.app_secret.reveal(),
        )
        if not all((_credential(sid), _credential(key), _credential(secret))):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        page_size = input.page_size or 30
        if input.page < 0 or input.page > 1000000 or page_size < 1 or page_size > 100:
            return self._branch("defect", FailureKind.VALIDATION)
        parameters = {
            "sid": sid,
            "appkey": key,
            "timestamp": str(self.client.clock_seconds()),
            "page_no": str(input.page),
            "page_size": str(page_size),
        }
        signed_parts = [
            f"{len(name.encode()):02d}-{name}:{len(parameters[name].encode()):04d}-{parameters[name]}"
            for name in sorted(parameters)
        ]
        signature = hashlib.md5((";".join(signed_parts) + secret).encode()).hexdigest()
        request = self.client.http_client.build_request(
            "POST",
            self.client.config.base_url.rstrip("/") + "/openapi/shop.php",
            content=urlencode({**parameters, "sign": signature}).encode(),
            headers={"Accept": "application/json", "Content-Type": "application/x-www-form-urlencoded"},
        )
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
            try:
                body = await read_bounded_body(response, self.client.config.max_response_bytes)
            except BodyTooLargeError:
                return self._branch("invalidResponse", FailureKind.RESPONSE_TOO_LARGE)
            except ProviderTransportError:
                return self._retry(FailureKind.AVAILABILITY)
            if status != 200:
                kind = FailureKind.AUTHENTICATION if status in (401, 403) else FailureKind.PROVIDER_REJECTION
                return self._branch("providerRejected", kind)
        finally:
            await response.aclose()
        try:
            wire = json.loads(body)
        except (UnicodeDecodeError, ValueError):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if (
            not isinstance(wire, dict)
            or not isinstance(wire.get("code"), int)
            or isinstance(wire["code"], bool)
        ):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if wire["code"] != 0:
            return self._provider_code(wire["code"])
        shops_wire, total = wire.get("shoplist"), wire.get("total_count")
        if (
            not isinstance(shops_wire, list)
            or len(shops_wire) > page_size
            or not isinstance(total, int)
            or isinstance(total, bool)
            or total < 0
        ):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        shops: list[ShopSummary] = []
        for item in shops_wire:
            if not isinstance(item, dict):
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            shop_id = _integer_text(item.get("shop_id"))
            state_text = _integer_text(item.get("auth_state"))
            number, name, platform = item.get("shop_no"), item.get("shop_name"), item.get("platform_id")
            if (
                shop_id is None
                or state_text is None
                or not 0 <= int(state_text) <= 3
                or not isinstance(number, str)
                or not 0 < len(number) <= 50
                or not isinstance(name, str)
                or not 0 < len(name) <= 128
                or not isinstance(platform, int)
                or isinstance(platform, bool)
                or platform < 0
            ):
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            shops.append(ShopSummary(shop_id, number, name, platform, int(state_text)))
        next_page = 0
        if (input.page + 1) * page_size < total:
            if not shops or input.page == 1000000:
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            next_page = input.page + 1
        return QueryAttempt.branch_result(
            "listed", ListShopsOutput(tuple(shops), total, next_page), Receipt()
        )
