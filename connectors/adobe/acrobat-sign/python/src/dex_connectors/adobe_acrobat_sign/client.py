# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded AcrobatSign agreement reads with Go-compatible outcomes."""

from __future__ import annotations

import asyncio
import hashlib
import ipaddress
import json
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any
from urllib.parse import quote, urlparse

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
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import GET_AGREEMENT_DEFINITION
from .models import AgreementStatus, GetAgreementInput


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""
    discovery: bool = False


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "adobe-acrobat-sign", "getAgreement", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[AgreementStatus]:
    return QueryAttempt.branch_result(branch, AgreementStatus(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[AgreementStatus]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_agreement_id(agreement_id: object) -> bool:
    return (
        isinstance(agreement_id, str)
        and bool(agreement_id)
        and len(agreement_id.encode("utf-8")) <= 256
        and not any(ord(char) < 32 or 127 <= ord(char) <= 159 or char in "/?#" for char in agreement_id)
    )


def _decode_agreement(body: bytes, requested_agreement_id: str) -> AgreementStatus:
    raw: Any = json.loads(body)
    if (
        not isinstance(raw, dict)
        or raw.get("id") != requested_agreement_id
        or not isinstance(raw.get("name"), str)
        or not raw["name"].strip()
        or not isinstance(raw.get("status"), str)
        or not raw["status"].strip()
    ):
        raise ValueError("agreement identity or name missing")
    return AgreementStatus(raw["id"], raw["name"], raw["status"])


class Client:
    """Resolve an integration key and read selected agreement metadata."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("AcrobatSign configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("AcrobatSign request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self._access_point_lock = asyncio.Lock()
        self._cached_key_hash = b""
        self._cached_access_point = ""

    def get_agreement(self) -> GetAgreementOperation:
        """Bind the getAgreement Query to this client."""
        return GetAgreementOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get(self, call: Call, agreement_id: str) -> _Exchange:
        try:
            credential = await self.credentials.resolve(call)
            integration_key = credential.integration_key.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _Exchange(error="credential")
        if not is_header_safe_credential(integration_key):
            return _Exchange(error="credential")
        async with self._access_point_lock:
            key_hash = hashlib.sha256(integration_key.encode()).digest()
            if self._cached_access_point and self._cached_key_hash == key_hash:
                access_point = self._cached_access_point
            else:
                discovery = await self._request(self.base_url + "/baseUris", integration_key)
                if discovery.status != 200 or discovery.error:
                    return _Exchange(
                        discovery.status, discovery.body, discovery.retry_after, discovery.error, True
                    )
                try:
                    raw: Any = json.loads(discovery.body)
                    access_point = _validate_access_point(raw["apiAccessPoint"], self.base_url)
                except (ValueError, TypeError, KeyError):
                    return _Exchange(status=200, error="invalid_access_point", discovery=True)
                self._cached_access_point = access_point
                self._cached_key_hash = key_hash
        return await self._request(
            access_point + "/api/rest/v6/agreements/" + quote(agreement_id, safe=""), integration_key
        )

    async def _request(self, request_url: str, integration_key: str) -> _Exchange:
        try:
            request = self.http_client.build_request(
                "GET",
                request_url,
                headers={"Accept": "application/json", "Authorization": "Bearer " + integration_key},
            )
            response, _ = await send_traced(self.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _Exchange(error="request")
        try:
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _Exchange(status=response.status_code, retry_after=retry_after, error="too_large")
            except ProviderTransportError:
                return _Exchange(status=response.status_code, retry_after=retry_after, error="interrupted")
            return _Exchange(status=response.status_code, body=body, retry_after=retry_after)
        finally:
            await response.aclose()


def _validate_access_point(raw_access_point: object, discovery_url: str) -> str:
    if not isinstance(raw_access_point, str):
        raise ValueError("invalid Acrobat Sign access point")
    access_point = urlparse(raw_access_point)
    discovery = urlparse(discovery_url)
    if (
        not access_point.hostname
        or access_point.username
        or access_point.password
        or access_point.query
        or access_point.fragment
        or access_point.path not in ("", "/")
    ):
        raise ValueError("invalid Acrobat Sign access point")
    try:
        is_loopback = ipaddress.ip_address(discovery.hostname or "").is_loopback
    except ValueError:
        is_loopback = discovery.hostname == "localhost"
    if is_loopback:
        if access_point.scheme != discovery.scheme or access_point.netloc != discovery.netloc:
            raise ValueError("invalid local Acrobat Sign access point")
    elif (
        access_point.scheme != "https"
        or access_point.port
        or not access_point.hostname.startswith("api.")
        or not access_point.hostname.endswith((".adobesign.com", ".echosign.com"))
    ):
        raise ValueError("invalid Acrobat Sign host")
    return raw_access_point.rstrip("/")


class GetAgreementOperation:
    """Read one agreement's ID, name, and status."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return GET_AGREEMENT_DEFINITION

    async def invoke(self, call: Call, input: GetAgreementInput) -> QueryAttempt[AgreementStatus]:
        """Classify agreement lookup outcomes consistently with Go."""
        if not isinstance(input, GetAgreementInput) or not _valid_agreement_id(input.agreement_id):
            return _branch(
                "defect", FailureKind.VALIDATION, "agreement ID must be 1–256 printable characters"
            )
        exchange = await self.client._get(call, input.agreement_id)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "AcrobatSign credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "AcrobatSign request failed")
        if exchange.status == 429:
            return _retry(
                FailureKind.RATE_LIMIT, "AcrobatSign rate limited the request", exchange.retry_after
            )
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(
                FailureKind.AVAILABILITY, "AcrobatSign is temporarily unavailable", exchange.retry_after
            )
        if exchange.error == "invalid_access_point":
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Acrobat Sign returned an unsafe API access point"
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Acrobat Sign response exceeded the configured limit",
            )
        if exchange.discovery and exchange.status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"Acrobat Sign rejected discovery HTTP {exchange.status}",
            )
        if exchange.status == 404:
            return _branch("notFound", FailureKind.NOT_FOUND, "AcrobatSign did not expose the agreement")
        if exchange.status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"AcrobatSign rejected HTTP {exchange.status}",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "AcrobatSign response was interrupted")
        try:
            agreement = _decode_agreement(exchange.body, input.agreement_id)
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "AcrobatSign returned invalid agreement JSON"
            )
        return QueryAttempt.branch_result("found", agreement, Receipt())
