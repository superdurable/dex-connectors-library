# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Signed Wapi vehicle-brand price transport and Go-compatible Query decisions."""

import hashlib
import json
from collections.abc import Callable
from datetime import UTC, datetime

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
from ._generated.definitions import LIST_VEHICLE_BRANDS_DEFINITION
from .models import ListVehicleBrandsInput, ListVehicleBrandsOutput, VehicleBrandPrice

BRAND_PRICE_ENDPOINT = "https://uaqy.api.storeapi.net/pyi/170/324"


def _valid_app_id(value: object) -> bool:
    return isinstance(value, str) and 0 < len(value) <= 20 and value.isascii() and value.isdigit()


def _valid_app_secret(value: object) -> bool:
    return isinstance(value, str) and len(value) == 32 and value.isascii() and value.isalnum()


class Client:
    """Own Wapi credentials and an optional injected HTTP transport and clock."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        request_clock: Callable[[], datetime] | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or config.max_response_bytes <= 0
            or config.max_response_bytes > 16 << 20
        ):
            raise ValueError("Wapi configuration and credential source are required")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None
        self.request_clock = request_clock or (lambda: datetime.now(UTC))

    def list_vehicle_brands(self) -> "ListVehicleBrandsOperation":
        """Bind the listVehicleBrands Query to this client."""
        return ListVehicleBrandsOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client only when this connector created it."""
        if self._owns_http_client:
            await self.http_client.aclose()


class ListVehicleBrandsOperation:
    """Read the provider's vehicle brands and quoted prices in Chinese fen."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated listVehicleBrands definition."""
        return LIST_VEHICLE_BRANDS_DEFINITION

    def _failure(self, kind: FailureKind) -> Failure:
        return Failure(kind, "wapi", "listVehicleBrands", "Wapi brand query failed")

    def _branch(self, branch: str, kind: FailureKind) -> QueryAttempt[ListVehicleBrandsOutput]:
        return QueryAttempt.branch_result(branch, ListVehicleBrandsOutput(), Receipt(), self._failure(kind))

    def _retry(self, kind: FailureKind, delay: float = 0) -> QueryAttempt[ListVehicleBrandsOutput]:
        return QueryAttempt.retry(self._failure(kind), delay)

    async def invoke(
        self, call: Call, input: ListVehicleBrandsInput
    ) -> QueryAttempt[ListVehicleBrandsOutput]:
        """Sign and read the fixed read-only brand-price endpoint."""
        if not isinstance(input, ListVehicleBrandsInput):
            return self._branch("defect", FailureKind.VALIDATION)
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError, AttributeError):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        app_id = credentials.app_id
        app_secret = credentials.app_secret.reveal()
        if not _valid_app_id(app_id) or not _valid_app_secret(app_secret):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        timestamp = str(int(self.client.request_clock().timestamp()))
        signing_text = "appid" + app_id + "formatjson" + "time" + timestamp + app_secret
        signature = hashlib.md5(signing_text.encode("utf-8")).hexdigest()  # noqa: S324 - provider protocol
        request = self.client.http_client.build_request(
            "GET",
            BRAND_PRICE_ENDPOINT,
            params={"appid": app_id, "format": "json", "time": timestamp, "sign": signature},
            headers={"Accept": "application/json"},
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
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return self._branch("providerRejected", kind)
            try:
                body = await read_bounded_body(response, self.client.config.max_response_bytes)
            except BodyTooLargeError:
                return self._branch("invalidResponse", FailureKind.RESPONSE_TOO_LARGE)
            except ProviderTransportError:
                return self._retry(FailureKind.AVAILABILITY)
            try:
                payload = json.loads(body)
            except (json.JSONDecodeError, UnicodeDecodeError, TypeError):
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            if (
                not isinstance(payload, dict)
                or not isinstance(payload.get("codeid"), str)
                or not payload["codeid"]
            ):
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            code = payload["codeid"]
            if code in {"10021", "10014"}:
                return self._retry(FailureKind.AVAILABILITY)
            if code != "10000":
                return self._branch("providerRejected", FailureKind.PROVIDER_REJECTION)
            raw_brands = payload.get("retdata")
            if not isinstance(raw_brands, list) or len(raw_brands) > 5000:
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            brands: list[VehicleBrandPrice] = []
            for item in raw_brands:
                if not isinstance(item, dict):
                    return self._branch("invalidResponse", FailureKind.PROTOCOL)
                identifier, name, price = item.get("ppid"), item.get("pp_name"), item.get("pp_price")
                if price is None:
                    price = 0
                if (
                    not isinstance(identifier, str)
                    or not identifier
                    or len(identifier.encode("utf-8", "replace")) > 50
                    or not isinstance(name, str)
                    or not name
                    or len(name.encode("utf-8", "replace")) > 256
                    or not isinstance(price, int)
                    or isinstance(price, bool)
                    or price < 0
                ):
                    return self._branch("invalidResponse", FailureKind.PROTOCOL)
                if len(brands) < 200:
                    brands.append(VehicleBrandPrice(identifier, name, price))
            return QueryAttempt.branch_result(
                "found", ListVehicleBrandsOutput(len(raw_brands), tuple(brands)), Receipt(), None
            )
        finally:
            await response.aclose()
