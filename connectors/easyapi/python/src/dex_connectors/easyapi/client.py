# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Private-safe EasyAPI invoicing company Query and outcome classification."""

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
    encode_query_values,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import CONNECTOR_ID as CONNECTOR_ID
from ._generated.definitions import DEFECT as DEFECT
from ._generated.definitions import FOUND as FOUND
from ._generated.definitions import INVALID_RESPONSE as INVALID_RESPONSE
from ._generated.definitions import PROVIDER_REJECTED as PROVIDER_REJECTED
from ._generated.definitions import QUERY_SHOP_DEFINITION as QUERY_SHOP_DEFINITION
from ._generated.definitions import QUERY_SHOP_OPERATION_ID as QUERY_SHOP_OPERATION_ID
from .models import QueryShopInput, ShopResult

_DEFAULT_BASE_URL = "https://fapiao-api.easyapi.com"
_DEFAULT_TIMEOUT_SECONDS = 15
_DEFAULT_MAX_RESPONSE_BYTES = 262_144
_INT64_MAX = 2**63 - 1


def _valid_tax_number(value: str) -> bool:
    return len(value.encode()) in (15, 17, 18, 19, 20) and all(
        character in "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ" for character in value
    )


def _valid_credential(value: str) -> bool:
    return bool(value) and len(value.encode()) <= 2_048 and value.strip() == value


def _wire_field(document: dict[str, Any], name: str) -> Any:
    if name in document:
        return document[name]
    return next((value for key, value in document.items() if key.casefold() == name.casefold()), None)


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, CONNECTOR_ID, QUERY_SHOP_OPERATION_ID, message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ShopResult]:
    return QueryAttempt.branch_result(branch, ShopResult(), Receipt(), _failure(kind, message))


def _http_rejection_kind(status: int) -> FailureKind:
    if status == 401:
        return FailureKind.AUTHENTICATION
    if status == 403:
        return FailureKind.AUTHORIZATION
    if status in (400, 404, 412):
        return FailureKind.VALIDATION
    return FailureKind.PROVIDER_REJECTION


class Client:
    """Own the EasyAPI HTTPS endpoint and resolve credentials for each shop Query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        """Validate the provider origin and disable redirects for query-string secrets."""
        if config is None or credentials is None:
            raise ValueError("EasyAPI settings and credential source are required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("EasyAPI timeout and response limit must be positive")
        self.base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL)
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def query_shop(self) -> QueryShopOperation:
        """Bind the read-only invoicing company Query."""
        return QueryShopOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after Worker shutdown."""
        await self.http_client.aclose()


class QueryShopOperation:
    """Read one authorized company's invoicing service status."""

    def __init__(self, client: Client) -> None:
        """Bind a validated EasyAPI client."""
        if client is None:
            raise ValueError("EasyAPI client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return generated Query identity and branches."""
        return QUERY_SHOP_DEFINITION

    async def invoke(self, call: Call, input: QueryShopInput) -> QueryAttempt[ShopResult]:
        """GET the shop record while excluding credentials and tax number from outcomes."""
        if not _valid_tax_number(input.tax_number):
            return _branch(DEFECT, FailureKind.VALIDATION, "EasyAPI tax number is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            app_key = credentials.app_key.reveal()
            app_secret = credentials.app_secret.reveal()
        except Exception:  # noqa: BLE001
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "EasyAPI credentials are unavailable or invalid"
            )
        if not _valid_credential(app_key) or not _valid_credential(app_secret):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "EasyAPI credentials are unavailable or invalid"
            )
        query = encode_query_values({
            "appKey": [app_key], "appSecret": [app_secret], "taxNumber": [input.tax_number]
        })
        try:
            request = self.client.http_client.build_request(
                "GET", self.client.base_url + "/shop/query?" + query,
                headers={"Accept": "application/json"},
            )
        except (httpx.HTTPError, ValueError, TypeError):
            return _branch(DEFECT, FailureKind.VALIDATION, "EasyAPI company request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return QueryAttempt.retry(
                _failure(FailureKind.AVAILABILITY, "EasyAPI company request failed")
            )
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return QueryAttempt.retry(
                    _failure(FailureKind.RATE_LIMIT, "EasyAPI rate limited company query"),
                    retry_after,
                )
            if status == 408 or status >= 500:
                return QueryAttempt.retry(
                    _failure(FailureKind.AVAILABILITY, "EasyAPI company API is temporarily unavailable"),
                    retry_after,
                )
            try:
                content = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE,
                    "EasyAPI response exceeded the configured limit",
                )
            except ProviderTransportError:
                return QueryAttempt.retry(
                    _failure(FailureKind.AVAILABILITY, "EasyAPI company response was interrupted")
                )
            if status != 200:
                return _branch(
                    PROVIDER_REJECTED, _http_rejection_kind(status),
                    f"EasyAPI rejected company query with HTTP {status}",
                )
            try:
                document = json.loads(content)
                if not isinstance(document, dict):
                    raise ValueError("company response is not an object")
            except (ValueError, UnicodeDecodeError):
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL,
                    "EasyAPI returned an invalid company response",
                )
            code = _wire_field(document, "code")
            if type(code) is not int or not -_INT64_MAX - 1 <= code <= _INT64_MAX:
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL,
                    "EasyAPI returned an invalid company response",
                )
            if code != 1:
                return _branch(
                    PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION,
                    "EasyAPI rejected company query",
                )
            shop = _wire_field(document, "content")
            if not isinstance(shop, dict):
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL,
                    "EasyAPI returned an invalid company response",
                )
            shop_id = _wire_field(shop, "shopId")
            name = _wire_field(shop, "name")
            api_state = _wire_field(shop, "apiState")
            end_time = _wire_field(shop, "endTime")
            if end_time is None:
                end_time = ""
            if not (
                type(shop_id) is int and 0 < shop_id <= _INT64_MAX
                and isinstance(name, str) and bool(name) and len(name.encode()) <= 256
                and type(api_state) is int and api_state in (-1, 0, 1)
                and isinstance(end_time, str) and len(end_time.encode()) <= 32
            ):
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL,
                    "EasyAPI returned an invalid company response",
                )
            return QueryAttempt.branch_result(
                FOUND, ShopResult(shop_id, name, api_state, end_time), Receipt()
            )
        finally:
            await response.aclose()


Config.__doc__ = "EasyAPI HTTPS origin and response bounds; zero uses manifest defaults."
Credentials.__doc__ = "Private EasyAPI service Key and Secret resolved for each shop Query."
