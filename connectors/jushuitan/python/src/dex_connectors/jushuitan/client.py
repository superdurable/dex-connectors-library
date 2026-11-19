# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Jushuitan signed shop-list Query and provider decisions."""

from __future__ import annotations

import hashlib
import json
from collections.abc import Callable
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
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import LIST_SHOPS_DEFINITION
from .models import ListShopsInput, ListShopsOutput, Shop

_SHOPS_ENDPOINT = "https://openapi.jushuitan.com/open/shops/query"


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListShopsOutput]:
    return QueryAttempt.branch_result(
        branch, ListShopsOutput(), Receipt(), Failure(kind, "jushuitan", "listShops", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListShopsOutput]:
    return QueryAttempt.retry(Failure(kind, "jushuitan", "listShops", message), delay)


def _valid_credential(value: str) -> bool:
    return (
        bool(value)
        and len(value) <= 1024
        and value.strip() == value
        and all(32 < ord(c) < 127 for c in value)
    )


class Client:
    """Own Jushuitan settings and resolve credentials for each shop page."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        *,
        clock: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Jushuitan configuration and credential source are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("Jushuitan timeout or response limit is invalid")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self.clock = clock or (lambda: datetime.now(UTC))

    def list_shops(self) -> ListShopsOperation:
        """Bind the shop-list Query to this client."""
        return ListShopsOperation(self)

    async def aclose(self) -> None:
        """Close the transport if this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class ListShopsOperation:
    """Read one page of nonsecret ERP shop metadata."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Jushuitan client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity and retry policy."""
        return LIST_SHOPS_DEFINITION

    async def invoke(self, call: Call, input: ListShopsInput) -> QueryAttempt[ListShopsOutput]:
        """Sign one shop-page request and classify the provider response."""
        if (
            not isinstance(input, ListShopsInput)
            or input.page_index < 1
            or input.page_size < 1
            or input.page_size > 100
        ):
            return _branch(
                "defect",
                FailureKind.VALIDATION,
                "pageIndex must be positive and pageSize must be between 1 and 100",
            )
        try:
            credentials = await self.client.credentials.resolve(call)
            app_key = credentials.app_key.reveal()
            app_secret = credentials.app_secret.reveal()
            access_token = credentials.access_token.reveal()
            if not all(_valid_credential(value) for value in (app_key, app_secret, access_token)):
                raise ValueError("invalid credentials")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Jushuitan credentials are unavailable or invalid"
            )
        business = json.dumps(
            {"page_index": input.page_index, "page_size": input.page_size}, separators=(",", ":")
        )
        form = {
            "access_token": access_token,
            "app_key": app_key,
            "biz": business,
            "charset": "utf-8",
            "timestamp": str(int(self.client.clock().timestamp())),
            "version": "2",
        }
        payload = app_secret + "".join(key + form[key] for key in sorted(form) if form[key])
        form["sign"] = hashlib.md5(payload.encode()).hexdigest()
        try:
            request = self.client.http_client.build_request(
                "POST",
                _SHOPS_ENDPOINT,
                content=urlencode(sorted(form.items())),
                headers={
                    "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8",
                    "Accept": "application/json",
                },
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "shop-list request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Jushuitan shop-list request failed")
        try:
            status = response.status_code
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Jushuitan rate limited shop listing", delay)
            if status == 408 or status >= 500:
                return _retry(
                    FailureKind.AVAILABILITY, "Jushuitan shop listing is temporarily unavailable", delay
                )
            if status != 200:
                kind = FailureKind.PROVIDER_REJECTION
                if status == 401:
                    kind = FailureKind.AUTHENTICATION
                elif status == 403:
                    kind = FailureKind.AUTHORIZATION
                return _branch(
                    "providerRejected", kind, f"Jushuitan rejected shop listing with HTTP {status}"
                )
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Jushuitan response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Jushuitan shop-list response was interrupted")
        finally:
            await response.aclose()
        try:
            wire: Any = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Jushuitan returned invalid shop data")
        if not isinstance(wire, dict) or type(wire.get("code")) is not int:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Jushuitan returned invalid shop data")
        code = wire["code"]
        if code in (199, 200):
            return _retry(FailureKind.RATE_LIMIT, "Jushuitan shop listing reached its call limit")
        if code == 150:
            return _retry(FailureKind.AVAILABILITY, "Jushuitan shop listing failed temporarily")
        if code != 0:
            kind = FailureKind.PROVIDER_REJECTION
            if code == 100:
                kind = FailureKind.AUTHENTICATION
            elif code in (110, 190):
                kind = FailureKind.AUTHORIZATION
            return _branch("providerRejected", kind, f"Jushuitan rejected shop listing with code {code}")
        data = wire.get("data")
        if (
            not isinstance(data, dict)
            or type(data.get("page_index")) is not int
            or data["page_index"] != input.page_index
            or type(data.get("page_size")) is not int
            or not 1 <= data["page_size"] <= 100
            or type(data.get("has_next")) is not bool
            or not isinstance(data.get("datas"), list)
        ):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Jushuitan returned invalid shop page metadata"
            )
        shops: list[Shop] = []
        for item in data["datas"]:
            if (
                not isinstance(item, dict)
                or type(item.get("shop_id")) is not int
                or item["shop_id"] <= 0
                or not isinstance(item.get("shop_name"), str)
                or not item["shop_name"]
                or not isinstance(item.get("shop_site", ""), str)
            ):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "Jushuitan returned invalid shop metadata"
                )
            shops.append(
                Shop(shop_id=item["shop_id"], name=item["shop_name"], site=item.get("shop_site", ""))
            )
        return QueryAttempt.branch_result(
            "found",
            ListShopsOutput(
                page_index=data["page_index"],
                page_size=data["page_size"],
                has_next=data["has_next"],
                shops=tuple(shops),
            ),
            Receipt(),
        )
