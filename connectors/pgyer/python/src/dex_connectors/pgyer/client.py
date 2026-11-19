# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Pgyer uploaded-app page with Go-compatible provider decisions."""

import ipaddress
import json
from datetime import UTC, datetime
from typing import Any
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
from ._generated.definitions import LIST_UPLOADED_APPS_DEFINITION
from .models import ListUploadedAppsInput, UploadedApp, UploadedAppPage


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[UploadedAppPage]:
    return QueryAttempt.branch_result(
        branch, UploadedAppPage(), Receipt(), Failure(kind, "pgyer", "listUploadedApps", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[UploadedAppPage]:
    return QueryAttempt.retry(Failure(kind, "pgyer", "listUploadedApps", message), delay)


def _validated_base_url(value: str, *, origin_only: bool = False) -> str:
    if not value or any(ord(char) <= 0x20 or ord(char) > 0x7E for char in value):
        raise ValueError("provider base URL must be printable ASCII")
    try:
        parsed = urlsplit(value)
        host = parsed.hostname
    except ValueError as error:
        raise ValueError("provider base URL is invalid") from error
    if not host or parsed.username or parsed.password or parsed.query or parsed.fragment:
        raise ValueError("provider base URL is invalid")
    if origin_only and parsed.path not in ("", "/"):
        raise ValueError("provider base URL must be an origin")
    if parsed.scheme != "https":
        try:
            is_loopback = host == "localhost" or ipaddress.ip_address(host).is_loopback
        except ValueError:
            is_loopback = host == "localhost"
        if parsed.scheme != "http" or not is_loopback:
            raise ValueError("provider base URL must use HTTPS")
    return value.rstrip("/")


class Client:
    """Own Pgyer settings and resolve credentials for each app-list Query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Pgyer configuration and credentials are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("Pgyer configuration is invalid")
        self.base_url = _validated_base_url(config.base_url, origin_only=False)
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_uploaded_apps(self) -> "ListUploadedAppsOperation":
        """Bind the generated app-list Query."""
        return ListUploadedAppsOperation(self)

    async def aclose(self) -> None:
        """Close only an HTTP client this instance created."""
        if self._owns_client:
            await self.http_client.aclose()


class ListUploadedAppsOperation:
    """Read at most one page of uploaded app metadata."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Pgyer client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated app-list contract."""
        return LIST_UPLOADED_APPS_DEFINITION

    async def invoke(self, call: Call, input: ListUploadedAppsInput) -> QueryAttempt[UploadedAppPage]:
        """Read a provider page and classify its outcome."""
        page = input.page if isinstance(input, ListUploadedAppsInput) else -1
        if type(page) is not int or page < 0 or page > 10000:
            return _branch("defect", FailureKind.VALIDATION, "Pgyer page number is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            key = credentials.api_key.reveal()
            if not key or len(key) > 512 or any(ord(c) <= 0x20 or ord(c) >= 0x7F for c in key):
                raise ValueError("invalid key")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch("defect", FailureKind.AUTHENTICATION, "Pgyer API Key is unavailable or invalid")
        form = {"_api_key": key}
        if page:
            form["page"] = str(page)
        try:
            request = self.client.http_client.build_request(
                "POST",
                self.client.base_url + "/apiv2/app/listMy",
                headers={"Accept": "application/json", "Content-Type": "application/x-www-form-urlencoded"},
                content=urlencode(form).encode(),
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "Pgyer app-list request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Pgyer app-list request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Pgyer hourly API limit reached", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Pgyer is temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Pgyer response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Pgyer response was interrupted")
        finally:
            await response.aclose()
        if status != 200:
            kind = (
                FailureKind.AUTHENTICATION
                if status == 401
                else FailureKind.AUTHORIZATION
                if status == 403
                else FailureKind.VALIDATION
                if status in (400, 404)
                else FailureKind.PROVIDER_REJECTION
            )
            return _branch("providerRejected", kind, f"Pgyer rejected the query with HTTP {status}")
        try:
            document: Any = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Pgyer returned an invalid app-list response"
            )
        code = document.get("code") if isinstance(document, dict) else None
        if type(code) is not int:
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Pgyer returned an invalid app-list response"
            )
        if code == 1098:
            return _retry(FailureKind.RATE_LIMIT, "Pgyer hourly API limit reached", delay)
        if code in (1003, 1188):
            return _retry(FailureKind.AVAILABILITY, "Pgyer is temporarily unavailable", delay)
        if code != 0:
            kind = (
                FailureKind.AUTHENTICATION
                if code in (1001, 1002, 1055)
                else FailureKind.AUTHORIZATION
                if code in (1122, 1124)
                else FailureKind.PROVIDER_REJECTION
            )
            return _branch("providerRejected", kind, f"Pgyer rejected the query with code {code}")
        apps = document.get("data")
        if not isinstance(apps, list) or len(apps) > 100:
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Pgyer returned an invalid app-list response"
            )
        result = []
        for app in apps:
            if (
                not isinstance(app, dict)
                or any(
                    not isinstance(app.get(name), str) or not app[name]
                    for name in ("appKey", "buildKey", "buildName")
                )
                or type(app.get("buildType")) is not int
                or app["buildType"] not in (1, 2)
            ):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "Pgyer returned an invalid app-list response"
                )
            result.append(
                UploadedApp(
                    app["appKey"],
                    app["buildKey"],
                    app["buildName"],
                    app["buildType"],
                    app.get("buildVersion", ""),
                    app.get("buildCreated", ""),
                )
            )
        return QueryAttempt.branch_result("listed", UploadedAppPage(tuple(result)), Receipt())
