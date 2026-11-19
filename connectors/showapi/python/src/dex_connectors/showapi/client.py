# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Showapi provider Query with bounded HTTP responses."""

import json
from datetime import UTC, datetime
from urllib.parse import urlsplit

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
from ._generated.definitions import LOOKUP_AREA_CODES_DEFINITION
from .models import AreaCodeMatch, AreaCodeMatches, LookupAreaCodesInput


class ProviderRejection(Exception):
    """Represent a definite provider refusal inside a successful HTTP exchange."""

    def __init__(self, kind: FailureKind) -> None:
        self.kind = kind


class InputDefect(Exception):
    """Represent invalid application input before any provider request."""


def _valid_secret(value: str, limit: int) -> bool:
    return (
        bool(value)
        and len(value) <= limit
        and value.strip() == value
        and all(0x20 < ord(ch) < 0x7F for ch in value)
    )


def _valid_base_url(value: str) -> str:
    parsed = urlsplit(value)
    if (
        parsed.scheme != "https"
        or not parsed.hostname
        or parsed.username
        or parsed.password
        or parsed.query
        or parsed.fragment
        or parsed.path not in ("", "/")
    ):
        raise ValueError("invalid HTTPS provider base URL")
    return value.rstrip("/")


def _json_object(body: bytes) -> dict[str, object]:
    wire = json.loads(body, parse_constant=lambda _: (_ for _ in ()).throw(ValueError()))
    if not isinstance(wire, dict):
        raise ValueError("invalid JSON object")
    return wire


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[AreaCodeMatches]:
    return QueryAttempt.branch_result(
        branch, AreaCodeMatches(), Receipt(), Failure(kind, "showapi", "lookupAreaCodes", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[AreaCodeMatches]:
    return QueryAttempt.retry(Failure(kind, "showapi", "lookupAreaCodes", message), delay)


class Client:
    """Own provider HTTP limits and resolve credentials for each attempt."""

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
            raise ValueError("Showapi configuration and credential source are required")

        self.base_url = _valid_base_url(config.base_url)

        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def lookup_area_codes(self) -> "LookupAreaCodesOperation":
        """Bind the provider Query to this client."""
        return LookupAreaCodesOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP transport only when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class LookupAreaCodesOperation:
    """Read provider data without changing provider state."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Showapi client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated provider Query contract."""
        return LOOKUP_AREA_CODES_DEFINITION

    async def invoke(self, call: Call, input: LookupAreaCodesInput) -> QueryAttempt[AreaCodeMatches]:
        """Make one provider request and classify definite and unknown outcomes."""
        try:
            credentials = await self.client.credentials.resolve(call)
            request = self._request(credentials, input)
        except InputDefect:
            return _branch("defect", FailureKind.VALIDATION, "invalid Query input")
        except (ValueError, LookupError, OSError, AttributeError, httpx.HTTPError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
            )
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "provider request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if response.status_code == 429:
                return _retry(FailureKind.RATE_LIMIT, "provider rate limited the request", delay)
            if response.status_code == 408 or response.status_code >= 500:
                return _retry(FailureKind.AVAILABILITY, "provider is temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse", FailureKind.RESPONSE_TOO_LARGE, "provider response exceeded the limit"
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "provider response was interrupted")
            if response.status_code != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if response.status_code == 401
                    else FailureKind.AUTHORIZATION
                    if response.status_code == 403
                    else FailureKind.VALIDATION
                    if response.status_code in (400, 404)
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch(
                    "providerRejected", kind, "provider rejected HTTP " + str(response.status_code)
                )
            try:
                output = self._parse(body)
            except ProviderRejection as rejection:
                return _branch("providerRejected", rejection.kind, "provider rejected the query")
            except (ValueError, KeyError, TypeError, UnicodeDecodeError, OverflowError):
                return _branch("invalidResponse", FailureKind.PROTOCOL, "provider returned invalid data")
            return QueryAttempt.branch_result("found", output, Receipt())
        finally:
            await response.aclose()

    def _request(self, credentials: Credentials, input: LookupAreaCodesInput) -> httpx.Request:
        """Build the provider request after credential and input validation."""

        if (
            not isinstance(input, LookupAreaCodesInput)
            or not input.area.strip()
            or len(input.area.strip()) > 100
            or "\r" in input.area
            or "\n" in input.area
        ):
            raise InputDefect("invalid area")
        key = credentials.app_key.reveal()
        if not _valid_secret(key, 512):
            raise ValueError("invalid app key")
        return self.client.http_client.build_request(
            "POST",
            self.client.base_url + "/9-3",
            params={"appKey": key},
            data={"area": input.area.strip()},
            headers={"Accept": "application/json", "Content-Type": "application/x-www-form-urlencoded"},
        )

    def _parse(self, body: bytes) -> AreaCodeMatches:
        """Validate the provider envelope and return only declared fields."""

        envelope = _json_object(body)
        system_code = envelope["showapi_res_code"]
        if not isinstance(system_code, int):
            raise ValueError("invalid system code")
        if system_code != 0:
            raise ProviderRejection(
                FailureKind.AUTHENTICATION if system_code == -1004 else FailureKind.PROVIDER_REJECTION
            )
        response = envelope["showapi_res_body"]
        if not isinstance(response, dict) or not isinstance(response.get("ret_code"), int):
            raise ValueError("invalid response")
        if response["ret_code"] != 0:
            raise ProviderRejection(FailureKind.PROVIDER_REJECTION)
        items = response["list"]
        if not isinstance(items, list) or len(items) > 100:
            raise ValueError("invalid match list")
        matches: list[AreaCodeMatch] = []
        for item in items:
            if (
                not isinstance(item, dict)
                or not isinstance(item.get("area"), str)
                or not item["area"]
                or not isinstance(item.get("areaCode"), str)
                or not item["areaCode"]
                or not isinstance(item.get("prov", ""), str)
            ):
                raise ValueError("invalid match")
            matches.append(AreaCodeMatch(item.get("prov", ""), item["area"], item["areaCode"]))
        return AreaCodeMatches(tuple(matches))
