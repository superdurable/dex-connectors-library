# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Chinaz site TDK Query with Go-compatible provider decisions."""

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
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import GET_SITE_TDK_DEFINITION
from .models import GetSiteTDKInput, SiteTDK


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[SiteTDK]:
    return QueryAttempt.branch_result(
        branch, SiteTDK(), Receipt(), Failure(kind, "chinaz", "getSiteTDK", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[SiteTDK]:
    return QueryAttempt.retry(Failure(kind, "chinaz", "getSiteTDK", message), delay)


def _valid_domain(domain: str) -> bool:
    if len(domain) < 3 or len(domain) > 253 or domain.lower() != domain:
        return False
    labels = domain.split(".")
    return len(labels) >= 2 and all(
        0 < len(label) <= 63
        and label[0] != "-"
        and label[-1] != "-"
        and all(character in "abcdefghijklmnopqrstuvwxyz0123456789-" for character in label)
        for label in labels
    )


def _valid_api_key(api_key: str) -> bool:
    return (
        bool(api_key) and len(api_key) <= 2048 and all(0x20 < ord(character) < 0x7F for character in api_key)
    )


def _wire_string(value: object) -> str:
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError("Chinaz field is not a string")
    return value


class Client:
    """Own Chinaz settings and resolve the API Key for each TDK request."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Chinaz settings and credential source are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("Chinaz timeout and response limit must be positive")
        self.base_url = validate_base_url(config.base_url)
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_site_tdk(self) -> "GetSiteTDKOperation":
        """Bind the site TDK Query."""
        return GetSiteTDKOperation(self)

    async def aclose(self) -> None:
        """Close only an HTTP client this instance created."""
        if self._owns_client:
            await self.http_client.aclose()


class GetSiteTDKOperation:
    """Read public title, keywords, description, and IP for one domain."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Chinaz client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated site TDK Query identity."""
        return GET_SITE_TDK_DEFINITION

    async def invoke(self, call: Call, input: GetSiteTDKInput) -> QueryAttempt[SiteTDK]:
        """Make one provider request and classify the observed result."""
        domain = input.domain if isinstance(input, GetSiteTDKInput) else ""
        if not _valid_domain(domain):
            return _branch("defect", FailureKind.VALIDATION, "Chinaz domain must be a valid hostname")
        try:
            credentials = await self.client.credentials.resolve(call)
            api_key = credentials.api_key.reveal()
            if not _valid_api_key(api_key):
                raise ValueError("invalid Chinaz API Key")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch("defect", FailureKind.AUTHENTICATION, "Chinaz API key is unavailable or invalid")
        try:
            request = self.client.http_client.build_request(
                "POST",
                self.client.base_url + "/single/tdk",
                headers={"Accept": "application/json"},
                files=[("key", (None, api_key)), ("domain", (None, domain))],
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "site TDK request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Chinaz site TDK request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Chinaz rate limited site TDK requests", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Chinaz site TDK is temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Chinaz response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Chinaz response was interrupted")
            if status != 200:
                kind = FailureKind.PROVIDER_REJECTION
                if status == 401:
                    kind = FailureKind.AUTHENTICATION
                elif status == 403:
                    kind = FailureKind.AUTHORIZATION
                elif status in (400, 404):
                    kind = FailureKind.VALIDATION
                return _branch("providerRejected", kind, f"Chinaz rejected site TDK query with HTTP {status}")
        finally:
            await response.aclose()
        return _decode_response(domain, body, delay)


def _decode_response(domain: str, body: bytes, delay: float) -> QueryAttempt[SiteTDK]:
    try:
        document: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return _branch(
            "invalidResponse", FailureKind.PROTOCOL, "Chinaz returned an invalid site TDK response"
        )
    if not isinstance(document, dict) or type(document.get("StateCode")) is not int:
        return _branch(
            "invalidResponse", FailureKind.PROTOCOL, "Chinaz returned an invalid site TDK response"
        )
    state = document["StateCode"]
    if state == 1:
        result = document.get("Result")
        if not isinstance(result, dict):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Chinaz returned an invalid site TDK response"
            )
        try:
            site = SiteTDK(
                domain,
                _wire_string(result.get("Title")),
                _wire_string(result.get("Keywords")),
                _wire_string(result.get("Description")),
                _wire_string(result.get("IP")),
            )
        except ValueError:
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Chinaz returned an invalid site TDK response"
            )
        return QueryAttempt.branch_result("found", site, Receipt())
    if state == 10025:
        return _branch(
            "notFound", FailureKind.PROVIDER_REJECTION, "Chinaz has no site TDK data for this domain"
        )
    if state in (10011, 10012, 10013, 10026):
        return _retry(FailureKind.RATE_LIMIT, "Chinaz rate limited site TDK requests", delay)
    if state in (-1, 10020, 10024, 10029, 10034):
        return _retry(FailureKind.AVAILABILITY, "Chinaz site TDK is temporarily unavailable", delay)
    if state in (10001, 10003, 10004, 10009):
        return _branch("providerRejected", FailureKind.AUTHENTICATION, "Chinaz rejected the API key")
    if state in (10002, 10005, 10008, 10022, 10027, 10032):
        return _branch(
            "providerRejected", FailureKind.AUTHORIZATION, "Chinaz rejected the API plan or account"
        )
    return _branch(
        "providerRejected",
        FailureKind.PROVIDER_REJECTION,
        f"Chinaz rejected site TDK query with state {state}",
    )
