# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Jiandaoyun application page Query with Go-compatible pagination and outcomes."""

from __future__ import annotations

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
from ._generated.definitions import CONNECTOR_ID as CONNECTOR_ID
from ._generated.definitions import DEFECT as DEFECT
from ._generated.definitions import INVALID_RESPONSE as INVALID_RESPONSE
from ._generated.definitions import LIST_APPS_PAGE_DEFINITION as LIST_APPS_PAGE_DEFINITION
from ._generated.definitions import LIST_APPS_PAGE_OPERATION_ID as LIST_APPS_PAGE_OPERATION_ID
from ._generated.definitions import LISTED as LISTED
from ._generated.definitions import PROVIDER_REJECTED as PROVIDER_REJECTED
from .models import AppMetadata, ListAppsPageInput, ListAppsPageOutput

_APPS_ENDPOINT = "https://api.jiandaoyun.com/api/v5/app/list"
_DEFAULT_TIMEOUT_SECONDS = 10
_DEFAULT_MAX_RESPONSE_BYTES = 1_048_576
_MAX_RESPONSE_BYTES = 16 << 20
_MAX_SKIP = 2**63 - 1 - 100


def _valid_api_key(value: str) -> bool:
    return bool(value) and len(value.encode()) <= 1_024 and value.strip() == value and all(
        33 <= ord(character) <= 126 for character in value
    )


def _wire_field(document: dict[str, Any], name: str) -> Any:
    if name in document:
        return document[name]
    return next((value for key, value in document.items() if key.casefold() == name.casefold()), None)


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, CONNECTOR_ID, LIST_APPS_PAGE_OPERATION_ID, message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListAppsPageOutput]:
    return QueryAttempt.branch_result(branch, ListAppsPageOutput(), Receipt(), _failure(kind, message))


class Client:
    """Own fixed Jiandaoyun endpoint and resolve its API Key for every Query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        """Validate response limits and supply a no-redirect provider HTTP client."""
        if config is None or credentials is None:
            raise ValueError("Jiandaoyun settings and credential source are required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("Jiandaoyun timeout or response limit is invalid")
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        if self.max_response_bytes > _MAX_RESPONSE_BYTES:
            raise ValueError("Jiandaoyun timeout or response limit is invalid")
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def list_apps_page(self) -> ListAppsPageOperation:
        """Bind the read-only application page Query."""
        return ListAppsPageOperation(self)

    async def aclose(self) -> None:
        """Close the provider HTTP client after Worker shutdown."""
        await self.http_client.aclose()


class ListAppsPageOperation:
    """Read one page of authorized application IDs and names."""

    def __init__(self, client: Client) -> None:
        """Bind a validated Jiandaoyun client."""
        if client is None:
            raise ValueError("Jiandaoyun client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return generated Query identity and branches."""
        return LIST_APPS_PAGE_DEFINITION

    async def invoke(
        self, call: Call, input: ListAppsPageInput
    ) -> QueryAttempt[ListAppsPageOutput]:
        """POST the read-only page request and classify provider outcomes."""
        if (
            type(input.skip) is not int or not 0 <= input.skip <= _MAX_SKIP
            or type(input.limit) is not int or not 0 <= input.limit <= 100
        ):
            return _branch(DEFECT, FailureKind.VALIDATION, "Jiandaoyun pagination is invalid")
        limit = input.limit or 100
        try:
            credentials = await self.client.credentials.resolve(call)
            api_key = credentials.api_key.reveal()
        except Exception:  # noqa: BLE001
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "Jiandaoyun API Key is unavailable or invalid"
            )
        if not _valid_api_key(api_key):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "Jiandaoyun API Key is unavailable or invalid"
            )
        body = json.dumps({"limit": limit, "skip": input.skip}, separators=(",", ":")).encode()
        try:
            request = self.client.http_client.build_request(
                "POST", _APPS_ENDPOINT, content=body,
                headers={
                    "Authorization": "Bearer " + api_key,
                    "Content-Type": "application/json",
                    "Accept": "application/json",
                },
            )
        except (httpx.HTTPError, ValueError, TypeError):
            return _branch(DEFECT, FailureKind.VALIDATION, "Jiandaoyun app-list request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return QueryAttempt.retry(
                _failure(FailureKind.AVAILABILITY, "Jiandaoyun app-list request failed")
            )
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return QueryAttempt.retry(
                    _failure(FailureKind.RATE_LIMIT, "Jiandaoyun rate limited the app-list request"),
                    retry_after,
                )
            if status == 408 or status >= 500:
                return QueryAttempt.retry(
                    _failure(FailureKind.AVAILABILITY, "Jiandaoyun app list is temporarily unavailable"),
                    retry_after,
                )
            try:
                content = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE,
                    "Jiandaoyun response exceeded the configured limit",
                )
            except ProviderTransportError:
                return QueryAttempt.retry(
                    _failure(FailureKind.AVAILABILITY, "Jiandaoyun app-list response was interrupted")
                )
            if status == 400:
                try:
                    provider_error = json.loads(content)
                    code = _wire_field(provider_error, "code") if isinstance(provider_error, dict) else None
                except (ValueError, UnicodeDecodeError):
                    code = None
                if type(code) is int and code == 8303:
                    return QueryAttempt.retry(
                        _failure(FailureKind.RATE_LIMIT, "Jiandaoyun rate limited the app-list request"),
                        retry_after,
                    )
                return _branch(
                    PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION,
                    "Jiandaoyun rejected the app-list request",
                )
            if status != 200:
                kind = FailureKind.PROVIDER_REJECTION
                if status == 401:
                    kind = FailureKind.AUTHENTICATION
                if status == 403:
                    kind = FailureKind.AUTHORIZATION
                return _branch(
                    PROVIDER_REJECTED, kind,
                    f"Jiandaoyun rejected the app-list request with HTTP {status}",
                )
            try:
                document = json.loads(content)
                if not isinstance(document, dict):
                    raise ValueError("apps response is not an object")
            except (ValueError, UnicodeDecodeError):
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL,
                    "Jiandaoyun returned invalid app metadata",
                )
            apps = _wire_field(document, "apps")
            if not isinstance(apps, list):
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL,
                    "Jiandaoyun returned invalid app metadata",
                )
            page: list[AppMetadata] = []
            for app in apps:
                if not isinstance(app, dict):
                    return _branch(
                        INVALID_RESPONSE, FailureKind.PROTOCOL,
                        "Jiandaoyun returned invalid app metadata",
                    )
                app_id = _wire_field(app, "app_id")
                name = _wire_field(app, "name")
                if not isinstance(app_id, str) or not app_id or not isinstance(name, str) or not name:
                    return _branch(
                        INVALID_RESPONSE, FailureKind.PROTOCOL,
                        "Jiandaoyun returned incomplete app metadata",
                    )
                page.append(AppMetadata(id=app_id, name=name))
            next_skip = input.skip + limit if len(page) == limit else 0
            return QueryAttempt.branch_result(
                LISTED, ListAppsPageOutput(tuple(page), next_skip), Receipt()
            )
        finally:
            await response.aclose()


Config.__doc__ = "Jiandaoyun request timeout and response bounds; zero uses manifest defaults."
Credentials.__doc__ = "Private API Key resolved for each application page request."
