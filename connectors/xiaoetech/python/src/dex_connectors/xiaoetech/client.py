# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""XiaoeTech token exchange and root goods category Query."""

import asyncio
import hashlib
import json
from datetime import UTC, datetime, timedelta

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
from dex_connectors.sdk.providerhttp import new_provider_http_client, parse_retry_after

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import LIST_ROOT_GOODS_CATEGORIES_DEFINITION
from .models import GoodsCategory, ListRootGoodsCategoriesInput, ListRootGoodsCategoriesOutput


def _valid_secret(value: object) -> bool:
    return (
        isinstance(value, str)
        and 0 < len(value) <= 512
        and value.strip() == value
        and all(0x20 < ord(c) < 0x7F for c in value)
    )


def _code_failure(code: int, *, is_category: bool) -> tuple[str, FailureKind]:
    if code == 1001:
        return "retry", FailureKind.AVAILABILITY
    if code in {2053, 2054}:
        return "retry", FailureKind.RATE_LIMIT
    if code == 1002:
        return "invalidResponse", FailureKind.PROTOCOL
    if code == 2008 and is_category:
        return "retry", FailureKind.AUTHENTICATION
    if code in {2008, 2001, 2002, 2003, 2004, 2005, 2006, 2020}:
        return "providerRejected", FailureKind.AUTHENTICATION
    if code in {2017, 2051}:
        return "providerRejected", FailureKind.AUTHORIZATION
    return "providerRejected", FailureKind.PROVIDER_REJECTION


class Client:
    """Resolve rotating application credentials and cache short-lived tokens."""

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
            raise ValueError("XiaoeTech configuration and credentials are invalid")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None
        self._token_lock = asyncio.Lock()
        self._token = ""
        self._token_expiration = datetime.min.replace(tzinfo=UTC)
        self._credential_fingerprint = b""

    def list_root_goods_categories(self) -> "ListRootGoodsCategoriesOperation":
        """Bind the generated category Query."""
        return ListRootGoodsCategoriesOperation(self)

    async def aclose(self) -> None:
        """Close the owned HTTP transport."""
        if self._owns_http_client:
            await self.http_client.aclose()

    async def _access_token(self, credentials: Credentials) -> tuple[str, str, FailureKind]:
        fingerprint = hashlib.sha256(
            (
                credentials.app_id + "\x00" + credentials.client_id + "\x00" + credentials.secret_key.reveal()
            ).encode()
        ).digest()
        async with self._token_lock:
            if (
                self._token
                and fingerprint == self._credential_fingerprint
                and self._token_expiration - datetime.now(UTC) > timedelta(minutes=5)
            ):
                return self._token, "", FailureKind.VALIDATION
            params = {
                "app_id": credentials.app_id,
                "client_id": credentials.client_id,
                "secret_key": credentials.secret_key.reveal(),
                "grant_type": "client_credential",
            }
            try:
                response = await self.http_client.get(
                    self.config.base_url.rstrip("/") + "/token",
                    params=params,
                    headers={"Accept": "application/json"},
                )
            except httpx.HTTPError:
                return "", "retry", FailureKind.AVAILABILITY
            try:
                if response.status_code == 429:
                    return "", "retry", FailureKind.RATE_LIMIT
                if response.status_code == 408 or response.status_code >= 500:
                    return "", "retry", FailureKind.AVAILABILITY
                body = await response.aread()
                if len(body) > self.config.max_response_bytes:
                    return "", "invalidResponse", FailureKind.RESPONSE_TOO_LARGE
            except httpx.HTTPError:
                return "", "retry", FailureKind.AVAILABILITY
            finally:
                await response.aclose()
            if response.status_code != 200:
                return "", "providerRejected", FailureKind.AUTHENTICATION
            try:
                wire = json.loads(body)
            except (ValueError, UnicodeError):
                return "", "invalidResponse", FailureKind.PROTOCOL
            if not isinstance(wire, dict) or type(wire.get("code")) is not int:
                return "", "invalidResponse", FailureKind.PROTOCOL
            if wire["code"] != 0:
                branch, kind = _code_failure(wire["code"], is_category=False)
                return "", branch, kind
            data = wire.get("data")
            if (
                not isinstance(data, dict)
                or not _valid_secret(data.get("access_token"))
                or type(data.get("expires_in")) is not int
                or not 600 <= data["expires_in"] <= 86400
            ):
                return "", "invalidResponse", FailureKind.PROTOCOL
            self._token = data["access_token"]
            self._credential_fingerprint = fingerprint
            self._token_expiration = datetime.now(UTC) + timedelta(seconds=data["expires_in"])
            return self._token, "", FailureKind.VALIDATION


class ListRootGoodsCategoriesOperation:
    """Read the authorized store's root goods categories."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query definition."""
        return LIST_ROOT_GOODS_CATEGORIES_DEFINITION

    def _failure(self, branch: str, kind: FailureKind) -> QueryAttempt[ListRootGoodsCategoriesOutput]:
        failure = Failure(kind, "xiaoetech", "listRootGoodsCategories", "XiaoeTech category query failed")
        if branch == "retry":
            return QueryAttempt.retry(failure)
        return QueryAttempt.branch_result(branch, ListRootGoodsCategoriesOutput(), Receipt(), failure)

    async def invoke(
        self, call: Call, input: ListRootGoodsCategoriesInput
    ) -> QueryAttempt[ListRootGoodsCategoriesOutput]:
        """Fetch a token and one bounded category response."""
        try:
            credentials = await self.client.credentials.resolve(call)
        except (LookupError, ValueError, OSError, AttributeError):
            return self._failure("defect", FailureKind.AUTHENTICATION)
        if not all(
            _valid_secret(value)
            for value in (credentials.app_id, credentials.client_id, credentials.secret_key.reveal())
        ):
            return self._failure("defect", FailureKind.AUTHENTICATION)
        token, branch, kind = await self.client._access_token(credentials)
        if branch:
            return self._failure(branch, kind)
        try:
            response = await self.client.http_client.post(
                self.client.config.base_url.rstrip("/") + "/xe.goods.category.list/2.0.0",
                json={"access_token": token, "parent_id": 0},
                headers={"Accept": "application/json", "Content-Type": "application/json"},
            )
        except httpx.HTTPError:
            return self._failure("retry", FailureKind.AVAILABILITY)
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return QueryAttempt.retry(
                    Failure(
                        FailureKind.RATE_LIMIT,
                        "xiaoetech",
                        "listRootGoodsCategories",
                        "XiaoeTech rate limited the query",
                    ),
                    retry_after,
                )
            if status == 408 or status >= 500:
                return self._failure("retry", FailureKind.AVAILABILITY)
            body = await response.aread()
            if len(body) > self.client.config.max_response_bytes:
                return self._failure("invalidResponse", FailureKind.RESPONSE_TOO_LARGE)
        except httpx.HTTPError:
            return self._failure("retry", FailureKind.AVAILABILITY)
        finally:
            await response.aclose()
        if status != 200:
            kind = (
                FailureKind.AUTHENTICATION
                if status == 401
                else FailureKind.AUTHORIZATION
                if status == 403
                else FailureKind.PROVIDER_REJECTION
            )
            return self._failure("providerRejected", kind)
        try:
            wire = json.loads(body)
        except (ValueError, UnicodeError):
            return self._failure("invalidResponse", FailureKind.PROTOCOL)
        if not isinstance(wire, dict) or type(wire.get("code")) is not int:
            return self._failure("invalidResponse", FailureKind.PROTOCOL)
        if wire["code"] != 0:
            if wire["code"] == 2008:
                self.client._token = ""
            branch, kind = _code_failure(wire["code"], is_category=True)
            return self._failure(branch, kind)
        data = wire.get("data")
        if not isinstance(data, list) or len(data) > 1000:
            return self._failure("invalidResponse", FailureKind.PROTOCOL)
        categories: list[GoodsCategory] = []
        for item in data:
            if (
                not isinstance(item, dict)
                or type(item.get("id")) is not int
                or item["id"] <= 0
                or not isinstance(item.get("title"), str)
                or not item["title"].strip()
                or len(item["title"]) > 512
                or type(item.get("has_children")) is not int
                or item["has_children"] not in {0, 1}
                or type(item.get("parent_id")) is not int
                or item["parent_id"] != 0
            ):
                return self._failure("invalidResponse", FailureKind.PROTOCOL)
            categories.append(GoodsCategory(item["id"], item["title"], bool(item["has_children"])))
        return QueryAttempt.branch_result(
            "listed", ListRootGoodsCategoriesOutput(tuple(categories)), Receipt(), None
        )
