# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Marketo lead lookup with Go-compatible outcomes."""

from __future__ import annotations

import json
from dataclasses import dataclass
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
from ._generated.definitions import GET_LEAD_DEFINITION
from .models import GetLeadInput, LeadProfile


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One provider response or sanitized request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "adobe-marketo", "getLead", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[LeadProfile]:
    return QueryAttempt.branch_result(branch, LeadProfile(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[LeadProfile]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _subscription_url(value: str, suffix: str) -> str:
    target = validate_base_url(value)
    parsed = urlsplit(target)
    host = parsed.hostname or ""
    if (
        parsed.path != suffix
        or parsed.query
        or parsed.fragment
        or parsed.username
        or parsed.password
        or parsed.port is not None
        or not (parsed.scheme == "https" and host.lower().endswith(".mktorest.com"))
    ):
        raise ValueError(f"Marketo endpoint must end in {suffix} on a subscription host")
    return target.rstrip("/")


def _decode(body: bytes, requested_id: int) -> tuple[str, LeadProfile | None, FailureKind | None]:
    try:
        envelope: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return "invalidResponse", None, FailureKind.PROTOCOL
    if not isinstance(envelope, dict) or type(envelope.get("success")) is not bool:
        return "invalidResponse", None, FailureKind.PROTOCOL
    if not envelope["success"]:
        errors = envelope.get("errors")
        if not isinstance(errors, list) or not errors or not isinstance(errors[0], dict):
            return "invalidResponse", None, FailureKind.PROTOCOL
        code = errors[0].get("code")
        if not isinstance(code, str):
            return "invalidResponse", None, FailureKind.PROTOCOL
        if code in {"606", "607", "615"}:
            return "rateLimit", None, FailureKind.RATE_LIMIT
        if code in {"604", "608", "611", "614", "500", "502"}:
            return "availability", None, FailureKind.AVAILABILITY
        if code == "702":
            return "notFound", None, FailureKind.NOT_FOUND
        if code == "602":
            return "tokenExpired", None, FailureKind.AUTHENTICATION
        if code == "601":
            return "providerRejected", None, FailureKind.AUTHENTICATION
        return "providerRejected", None, FailureKind.PROVIDER_REJECTION
    records = envelope.get("result")
    if not isinstance(records, list) or len(records) > 1:
        return "invalidResponse", None, FailureKind.PROTOCOL
    if not records:
        return "notFound", None, FailureKind.NOT_FOUND
    record = records[0]
    if not isinstance(record, dict) or type(record.get("id")) is not int or record["id"] != requested_id:
        return "invalidResponse", None, FailureKind.PROTOCOL
    first_name = record.get("firstName")
    last_name = record.get("lastName")
    if first_name is not None and not isinstance(first_name, str):
        return "invalidResponse", None, FailureKind.PROTOCOL
    if last_name is not None and not isinstance(last_name, str):
        return "invalidResponse", None, FailureKind.PROTOCOL
    return "found", LeadProfile(requested_id, first_name or "", last_name or ""), None


class Client:
    """Resolve a renewable token per call and read selected lead fields."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Marketo configuration and credentials are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("Marketo request limits must be positive")
        self.rest_endpoint = _subscription_url(config.rest_endpoint, "/rest")
        token_endpoint = _subscription_url(config.token_endpoint, "/identity/oauth/token")
        if urlsplit(self.rest_endpoint).hostname != urlsplit(token_endpoint).hostname:
            raise ValueError("Marketo REST and Identity URLs must use the same subscription host")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_lead(self) -> GetLeadOperation:
        """Bind the getLead Query to this client."""
        return GetLeadOperation(self)

    async def aclose(self) -> None:
        """Close only the HTTP client created by this connector."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get(self, call: Call, lead_id: int) -> _Exchange:
        try:
            credential = await self.credentials.resolve(call)
            token = credential.access_token.reveal()
        except ReauthorizationRequiredError:
            return _Exchange(error="credential")
        except Exception:
            return _Exchange(error="request")
        if not is_header_safe_credential(token):
            return _Exchange(error="credential")
        query = urlencode({"fields": "id,firstName,lastName"})
        try:
            request = self.http_client.build_request(
                "GET",
                f"{self.rest_endpoint}/v1/lead/{lead_id}.json?{query}",
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


class GetLeadOperation:
    """Read one lead's ID and optional names from Marketo."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Marketo client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return GET_LEAD_DEFINITION

    async def invoke(self, call: Call, input: GetLeadInput) -> QueryAttempt[LeadProfile]:
        """Classify lead lookup outcomes consistently with Go."""
        if not isinstance(input, GetLeadInput) or type(input.lead_id) is not int or input.lead_id <= 0:
            return _branch("defect", FailureKind.VALIDATION, "Marketo lead ID must be positive")
        exchange = await self.client._get(call, input.lead_id)
        if exchange.error == "credential":
            return _branch("defect", FailureKind.AUTHENTICATION, "Marketo connection needs authorization")
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Marketo request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "Marketo rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(
                FailureKind.AVAILABILITY, "Marketo is temporarily unavailable", exchange.retry_after
            )
        if exchange.status != 200:
            return _branch(
                "providerRejected", FailureKind.PROVIDER_REJECTION, f"Marketo rejected HTTP {exchange.status}"
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Marketo response exceeded the configured limit",
            )
        if exchange.error == "interrupted":
            return _retry(FailureKind.TRANSPORT, "Marketo response was interrupted")
        result, profile, failure = _decode(exchange.body, input.lead_id)
        if result == "found" and profile is not None:
            return QueryAttempt.branch_result("found", profile, Receipt())
        if result == "rateLimit":
            return _retry(
                FailureKind.RATE_LIMIT, "Marketo quota or concurrency limit was reached", exchange.retry_after
            )
        if result == "availability":
            return _retry(
                FailureKind.AVAILABILITY, "Marketo is temporarily unavailable", exchange.retry_after
            )
        if result == "tokenExpired":
            # The token expired between resolution and use; the next attempt resolves the credential again.
            return _retry(FailureKind.AUTHENTICATION, "Marketo access token expired", exchange.retry_after)
        return _branch(
            result, failure or FailureKind.PROTOCOL, "Marketo lead lookup did not return a profile"
        )
