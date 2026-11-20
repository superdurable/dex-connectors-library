# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Weimob token exchange and authorized store Query."""

import asyncio
import hashlib
import json
from datetime import UTC, datetime, timedelta
from urllib.parse import urlparse

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
from ._generated.definitions import GET_STORE_BASIC_DEFINITION
from .models import GetStoreBasicInput, StoreBasic


def _opaque(value: str) -> bool:
    return isinstance(value, str) and 0 < len(value) <= 4096 and all("!" <= char <= "~" for char in value)


class Client:
    """Resolve Weimob credentials per call and cache the short-lived access token."""

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
            raise ValueError("Weimob configuration and credentials are required")
        parsed = urlparse(config.base_url)
        if parsed.scheme not in {"https", "http"} or not parsed.netloc or parsed.query or parsed.fragment:
            raise ValueError("Weimob base URL is invalid")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None
        self._token_lock = asyncio.Lock()
        self._token = ""
        self._token_expires_at = datetime.min.replace(tzinfo=UTC)
        self._token_fingerprint = b""

    def get_store_basic(self) -> "GetStoreBasicOperation":
        """Bind the generated getStoreBasic Query definition."""
        return GetStoreBasicOperation(self)

    async def aclose(self) -> None:
        """Close the owned HTTP transport."""
        if self._owns_http_client:
            await self.http_client.aclose()

    async def _token_for(self, credentials: Credentials) -> tuple[str, str, FailureKind]:
        fingerprint = hashlib.sha256(
            (
                credentials.client_id
                + "\x00"
                + credentials.client_secret.reveal()
                + "\x00"
                + credentials.shop_id
            ).encode()
        ).digest()
        async with self._token_lock:
            if (
                self._token
                and self._token_fingerprint == fingerprint
                and datetime.now(UTC) + timedelta(minutes=1) < self._token_expires_at
            ):
                return self._token, "", FailureKind.VALIDATION
            params = {
                "grant_type": "client_credentials",
                "client_id": credentials.client_id,
                "client_secret": credentials.client_secret.reveal(),
            }
            if credentials.shop_id:
                params.update({"shop_id": credentials.shop_id, "shop_type": "business_operation_system_id"})
            try:
                response = await self.http_client.post(
                    self.config.base_url.rstrip("/") + "/fuwu/b/oauth2/token",
                    params=params,
                    content=b"{}",
                    headers={"Content-Type": "application/json; charset=utf-8"},
                )
            except httpx.HTTPError:
                return "", "retry", FailureKind.TRANSPORT
            try:
                if response.status_code == 429:
                    return "", "retry", FailureKind.RATE_LIMIT
                if response.status_code == 408 or response.status_code >= 500:
                    return "", "retry", FailureKind.AVAILABILITY
                if response.status_code != 200:
                    return "", "providerRejected", FailureKind.PROVIDER_REJECTION
                body = await response.aread()
                if len(body) > 8192:
                    return "", "invalidResponse", FailureKind.PROTOCOL
            except httpx.HTTPError:
                return "", "invalidResponse", FailureKind.PROTOCOL
            finally:
                await response.aclose()
            try:
                wire = json.loads(body)
            except (ValueError, UnicodeDecodeError):
                return "", "invalidResponse", FailureKind.PROTOCOL
            if not isinstance(wire, dict):
                return "", "invalidResponse", FailureKind.PROTOCOL
            if wire.get("error"):
                return "", "providerRejected", FailureKind.PROVIDER_REJECTION
            token, expires_in = wire.get("access_token"), wire.get("expires_in")
            if (
                not isinstance(token, str)
                or not _opaque(token)
                or type(expires_in) is not int
                or not 120 <= expires_in <= 604800
            ):
                return "", "invalidResponse", FailureKind.PROTOCOL
            self._token = token
            self._token_fingerprint = fingerprint
            self._token_expires_at = datetime.now(UTC) + timedelta(seconds=expires_in)
            return token, "", FailureKind.VALIDATION


class GetStoreBasicOperation:
    """Read the public store profile associated with the connection."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated operation definition."""
        return GET_STORE_BASIC_DEFINITION

    def _failure(self, branch: str, kind: FailureKind) -> QueryAttempt[StoreBasic]:
        failure = Failure(kind, "weimob", "getStoreBasic", "Weimob store query failed")
        if branch == "retry":
            return QueryAttempt.retry(failure)
        return QueryAttempt.branch_result(branch, StoreBasic(), Receipt(), failure)

    async def invoke(self, call: Call, input: GetStoreBasicInput) -> QueryAttempt[StoreBasic]:
        """Get one bounded provider response with no credentials in the result."""
        if not isinstance(input, GetStoreBasicInput):
            return self._failure("defect", FailureKind.VALIDATION)
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError, AttributeError):
            return self._failure("defect", FailureKind.AUTHENTICATION)
        if (
            not _opaque(credentials.client_id)
            or not _opaque(credentials.client_secret.reveal())
            or (
                credentials.shop_id != ""
                and (
                    not credentials.shop_id.isascii()
                    or not credentials.shop_id.isdecimal()
                    or len(credentials.shop_id) > 20
                )
            )
        ):
            return self._failure("defect", FailureKind.AUTHENTICATION)
        token, branch, kind = await self.client._token_for(credentials)
        if branch:
            return self._failure(branch, kind)
        try:
            response = await self.client.http_client.post(
                self.client.config.base_url.rstrip("/") + "/apigw/bos/v2.0/info/get",
                params={"accesstoken": token},
                content=b"{}",
                headers={"Accept": "application/json", "Content-Type": "application/json; charset=utf-8"},
            )
        except httpx.HTTPError:
            return self._failure("retry", FailureKind.TRANSPORT)
        try:
            status = response.status_code
            if status == 429:
                return QueryAttempt.retry(
                    Failure(
                        FailureKind.RATE_LIMIT, "weimob", "getStoreBasic", "Weimob rate limited the request"
                    ),
                    parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC)),
                )
            if status == 408 or status >= 500:
                return self._failure("retry", FailureKind.AVAILABILITY)
            body = await response.aread()
            if len(body) > self.client.config.max_response_bytes:
                return self._failure("invalidResponse", FailureKind.RESPONSE_TOO_LARGE)
        except httpx.HTTPError:
            return self._failure("retry", FailureKind.TRANSPORT)
        finally:
            await response.aclose()
        if status != 200:
            return self._failure("providerRejected", FailureKind.PROVIDER_REJECTION)
        try:
            wire = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return self._failure("invalidResponse", FailureKind.PROTOCOL)
        if (
            not isinstance(wire, dict)
            or not isinstance(wire.get("code"), dict)
            or not isinstance(wire["code"].get("errcode"), str)
            or not wire["code"]["errcode"]
        ):
            return self._failure("invalidResponse", FailureKind.PROTOCOL)
        if wire["code"]["errcode"] != "0":
            return self._failure("providerRejected", FailureKind.PROVIDER_REJECTION)
        data = wire.get("data")
        if (
            not isinstance(data, dict)
            or type(data.get("bosId")) is not int
            or data["bosId"] <= 0
            or not isinstance(data.get("bosName"), str)
            or not data["bosName"]
        ):
            return self._failure("invalidResponse", FailureKind.PROTOCOL)
        if not isinstance(data.get("bosLogo", ""), str) or not isinstance(data.get("desc", ""), str):
            return self._failure("invalidResponse", FailureKind.PROTOCOL)
        return QueryAttempt.branch_result(
            "found",
            StoreBasic(data["bosId"], data["bosName"], data.get("bosLogo", ""), data.get("desc", "")),
            Receipt(),
            None,
        )
