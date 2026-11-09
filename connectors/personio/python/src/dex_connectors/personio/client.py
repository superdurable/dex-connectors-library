# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Personio v2 person lookup with Go-compatible outcomes."""

from __future__ import annotations

import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any
from urllib.parse import urlsplit

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    QueryAttempt,
    QueryDefinition,
    ReauthorizationRequiredError,
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
from ._generated.definitions import GET_PERSON_DEFINITION
from .models import GetPersonInput, PersonProfile


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One provider response or sanitized request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "personio", "getPerson", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[PersonProfile]:
    return QueryAttempt.branch_result(branch, PersonProfile(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[PersonProfile]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _decode_person(body: bytes, requested_id: str) -> PersonProfile:
    raw: Any = json.loads(body)
    if not isinstance(raw, dict) or raw.get("id") != requested_id:
        raise ValueError("person ID mismatch")
    first_name = raw.get("first_name")
    last_name = raw.get("last_name")
    if first_name is not None and not isinstance(first_name, str):
        raise ValueError("invalid first name")
    if last_name is not None and not isinstance(last_name, str):
        raise ValueError("invalid last name")
    return PersonProfile(requested_id, first_name or "", last_name or "")


class Client:
    """Resolve access tokens per call and read selected Personio person fields."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Personio configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("Personio request limits must be positive")
        base_url = validate_base_url(config.base_url).rstrip("/")
        if urlsplit(base_url).path != "/v2":
            raise ValueError("Personio API URL must end in /v2")
        self.base_url = base_url
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_person(self) -> GetPersonOperation:
        """Bind the getPerson Query to this client."""
        return GetPersonOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client only when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get(self, call: Call, person_id: str) -> _Exchange:
        try:
            credential = await self.credentials.resolve(call)
            token = credential.access_token.reveal()
        except ReauthorizationRequiredError:
            return _Exchange(error="credential")
        except Exception:
            return _Exchange(error="request")
        if not is_header_safe_credential(token):
            return _Exchange(error="credential")
        return await self._send(call, token, person_id)

    async def _send(self, call: Call, token: str, person_id: str) -> _Exchange:
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + "/persons/" + person_id,
                headers={"Accept": "application/json", "Authorization": "Bearer " + token},
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


class GetPersonOperation:
    """Read one person's ID and names from Personio."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Personio client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return GET_PERSON_DEFINITION

    async def invoke(self, call: Call, input: GetPersonInput) -> QueryAttempt[PersonProfile]:
        """Classify person lookup outcomes consistently with Go."""
        if (
            not isinstance(input, GetPersonInput)
            or not isinstance(input.person_id, str)
            or re.fullmatch(r"[A-Za-z0-9_-]{1,128}", input.person_id) is None
        ):
            return _branch("defect", FailureKind.VALIDATION, "Personio person ID must be an API ID")
        exchange = await self.client._get(call, input.person_id)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Personio connection needs authorization"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Personio request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "Personio rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(
                FailureKind.AVAILABILITY, "Personio is temporarily unavailable", exchange.retry_after
            )
        if exchange.status == 404:
            return _branch("notFound", FailureKind.NOT_FOUND, "Personio person was not found")
        if exchange.status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"Personio rejected HTTP {exchange.status}",
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Personio response exceeded the configured limit",
            )
        if exchange.error == "interrupted":
            return _retry(FailureKind.TRANSPORT, "Personio response was interrupted")
        try:
            profile = _decode_person(exchange.body, input.person_id)
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Personio returned invalid person JSON"
            )
        return QueryAttempt.branch_result("found", profile, Receipt())
