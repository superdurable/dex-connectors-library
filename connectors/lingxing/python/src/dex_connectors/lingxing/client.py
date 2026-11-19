# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Lingxing token and signed store-list Query with Go-compatible outcomes."""

import base64
import hashlib
import json
from collections.abc import Callable
from datetime import UTC, datetime
from typing import Any

import httpx
from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes
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
from ._generated.definitions import LIST_STORES_DEFINITION
from .models import ListStoresInput, Store, StoreList


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[StoreList]:
    return QueryAttempt.branch_result(
        branch, StoreList(), Receipt(), Failure(kind, "lingxing", "listStores", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[StoreList]:
    return QueryAttempt.retry(Failure(kind, "lingxing", "listStores", message), delay)


def _valid_secret(value: str) -> bool:
    return bool(value) and len(value) <= 2048 and all(0x20 < ord(character) < 0x7F for character in value)


def _code(value: object) -> int | None:
    if type(value) is int:
        return value
    if isinstance(value, str):
        try:
            return int(value)
        except ValueError:
            return None
    return None


def _http_failure(status: int) -> FailureKind:
    return {
        401: FailureKind.AUTHENTICATION,
        403: FailureKind.AUTHORIZATION,
        400: FailureKind.VALIDATION,
        404: FailureKind.VALIDATION,
    }.get(status, FailureKind.PROVIDER_REJECTION)


def _sign_store_request(token: str, app_id: str, timestamp: str) -> str:
    values = {"access_token": token, "app_key": app_id, "timestamp": timestamp}
    canonical = "&".join(f"{name}={values[name]}" for name in sorted(values) if values[name])
    digest = hashlib.md5(canonical.encode()).hexdigest().upper().encode()
    pad = 16 - len(digest) % 16
    cipher = Cipher(algorithms.AES(app_id.encode()), modes.ECB()).encryptor()
    return base64.b64encode(cipher.update(digest + bytes([pad]) * pad) + cipher.finalize()).decode()


class Client:
    """Resolve rotated Lingxing credentials and cache their access token in memory."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        clock: Callable[[], datetime] | None = None,
        multipart_boundary: str | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or not 0 < config.max_response_bytes <= 16 << 20
        ):
            raise ValueError("Lingxing configuration and credentials are invalid")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.base_url = config.base_url.rstrip("/")
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self.clock = clock or (lambda: datetime.now(UTC))
        self.multipart_boundary = multipart_boundary
        self._token = ""
        self._token_credentials = ""
        self._token_expiry = datetime.min.replace(tzinfo=UTC)

    def list_stores(self) -> "ListStoresOperation":
        """Bind the authorized-store Query."""
        return ListStoresOperation(self)

    async def aclose(self) -> None:
        """Close the transport if this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def access_token(self, app_id: str, app_secret: str) -> str | QueryAttempt[StoreList]:
        """Get and cache a provider token for the current credential pair."""
        fingerprint = hashlib.sha256((app_id + "\x00" + app_secret).encode()).hexdigest()
        if self._token and self._token_credentials == fingerprint and self.clock() < self._token_expiry:
            return self._token
        try:
            request = self.http_client.build_request(
                "POST",
                self.base_url + "/api/auth-server/oauth/access-token",
                files={"appId": (None, app_id), "appSecret": (None, app_secret)},
                headers={"Accept": "application/json"},
            )
            if self.multipart_boundary:
                body = (await request.aread()).replace(
                    request.headers["content-type"].split("boundary=")[1].encode(),
                    self.multipart_boundary.encode(),
                )
                request = self.http_client.build_request(
                    "POST",
                    self.base_url + "/api/auth-server/oauth/access-token",
                    content=body,
                    headers={
                        "Accept": "application/json",
                        "Content-Type": "multipart/form-data; boundary=" + self.multipart_boundary,
                    },
                )
            response, _ = await send_traced(self.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(FailureKind.AVAILABILITY, "Lingxing token request failed")
        try:
            if response.status_code == 429:
                delay = parse_retry_after(response.headers.get("Retry-After", ""), self.clock())
                return _retry(FailureKind.RATE_LIMIT, "Lingxing token request was rate limited", delay)
            if response.status_code == 408 or response.status_code >= 500:
                return _retry(FailureKind.AVAILABILITY, "Lingxing token service is unavailable")
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Lingxing token response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Lingxing token response was interrupted")
            if response.status_code != 200:
                return _branch(
                    "providerRejected", _http_failure(response.status_code), "Lingxing rejected token request"
                )
            try:
                data: Any = json.loads(body)
            except (UnicodeDecodeError, ValueError):
                return _branch("invalidResponse", FailureKind.PROTOCOL, "Lingxing token response was invalid")
            if not isinstance(data, dict) or _code(data.get("code")) is None:
                return _branch("invalidResponse", FailureKind.PROTOCOL, "Lingxing token status was invalid")
            if _code(data["code"]) != 200:
                return _branch(
                    "providerRejected", FailureKind.AUTHENTICATION, "Lingxing rejected AppID or AppSecret"
                )
            token_data = data.get("data")
            if not isinstance(token_data, dict) or not _valid_secret(token_data.get("access_token", "")):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "Lingxing token response was incomplete"
                )
            expiry = _code(token_data.get("expires_in"))
            if expiry is None or not 60 <= expiry <= 86400:
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "Lingxing token response was incomplete"
                )
            self._token = token_data["access_token"]
            self._token_credentials = fingerprint
            from datetime import timedelta

            self._token_expiry = self.clock() + timedelta(seconds=expiry - 30)
            return self._token
        finally:
            await response.aclose()


class ListStoresOperation:
    """Read authorized Amazon stores through Lingxing ERP."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Lingxing client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the stable generated Query definition."""
        return LIST_STORES_DEFINITION

    async def invoke(self, call: Call, input: ListStoresInput) -> QueryAttempt[StoreList]:
        """Obtain a token, sign the store request, and classify its result."""
        if not isinstance(input, ListStoresInput):
            return _branch("defect", FailureKind.VALIDATION, "Lingxing input is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            app_id = credentials.app_id.reveal()
            app_secret = credentials.app_secret.reveal()
            if len(app_id) not in {16, 24, 32} or not _valid_secret(app_id) or not _valid_secret(app_secret):
                raise ValueError("invalid credential")
        except (ValueError, LookupError, OSError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Lingxing AppID or AppSecret is unavailable or invalid"
            )
        token = await self.client.access_token(app_id, app_secret)
        if not isinstance(token, str):
            return token
        timestamp = str(int(self.client.clock().timestamp()))
        try:
            signature = _sign_store_request(token, app_id, timestamp)
            request = self.client.http_client.build_request(
                "GET",
                self.client.base_url + "/erp/sc/data/seller/lists",
                params={"app_key": app_id, "timestamp": timestamp, "sign": signature},
                headers={"Authorization": "Bearer " + token, "Accept": "application/json"},
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(FailureKind.AVAILABILITY, "Lingxing store request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), self.client.clock())
            if response.status_code == 429:
                return _retry(FailureKind.RATE_LIMIT, "Lingxing store request was rate limited", delay)
            if response.status_code == 408 or response.status_code >= 500:
                return _retry(FailureKind.AVAILABILITY, "Lingxing store service is unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Lingxing store response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Lingxing store response was interrupted")
            if response.status_code != 200:
                return _branch(
                    "providerRejected",
                    _http_failure(response.status_code),
                    f"Lingxing rejected store request with HTTP {response.status_code}",
                )
            try:
                data: Any = json.loads(body)
            except (UnicodeDecodeError, ValueError):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "Lingxing returned invalid store data"
                )
            if not isinstance(data, dict) or _code(data.get("code")) is None:
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "Lingxing returned invalid store data"
                )
            code = _code(data["code"])
            if code == 3001008:
                return _retry(FailureKind.RATE_LIMIT, "Lingxing store request was rate limited")
            if code != 0:
                return _branch(
                    "providerRejected", FailureKind.PROVIDER_REJECTION, "Lingxing rejected store request"
                )
            items = data.get("data")
            if not isinstance(items, list):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "Lingxing returned invalid store data"
                )
            stores: list[Store] = []
            for item in items:
                if (
                    not isinstance(item, dict)
                    or type(item.get("sid")) is not int
                    or item["sid"] <= 0
                    or not isinstance(item.get("name"), str)
                    or not item["name"]
                    or type(item.get("status")) is not int
                    or not 0 <= item["status"] <= 3
                ):
                    return _branch(
                        "invalidResponse", FailureKind.PROTOCOL, "Lingxing returned invalid store data"
                    )
                if len(stores) < 200:
                    stores.append(
                        Store(
                            item["sid"],
                            item["name"],
                            item.get("region", ""),
                            item.get("country", ""),
                            item["status"],
                        )
                    )
            return QueryAttempt.branch_result("listed", StoreList(tuple(stores), len(items) > 200), Receipt())
        finally:
            await response.aclose()
