# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Lever opportunity reads with Go-compatible outcomes."""

from __future__ import annotations

import base64
import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any
from urllib.parse import quote

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
from ._generated.definitions import GET_OPPORTUNITY_DEFINITION
from .models import GetOpportunityInput, OpportunityStage


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "lever", "getOpportunity", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[OpportunityStage]:
    return QueryAttempt.branch_result(branch, OpportunityStage(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[OpportunityStage]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_opportunity_id(opportunity_id: object) -> bool:
    return isinstance(opportunity_id, str) and re.fullmatch(
        r"[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}",
        opportunity_id,
    ) is not None


def _decode_opportunity(body: bytes, requested_opportunity_id: str) -> OpportunityStage:
    raw: Any = json.loads(body)
    if not isinstance(raw, dict) or not isinstance(raw.get("data"), dict):
        raise ValueError("opportunity data missing")
    data = raw["data"]
    if data.get("id") != requested_opportunity_id or any(
        not isinstance(data.get(field), str) or not data[field].strip()
        for field in ("name", "stage")
    ):
        raise ValueError("opportunity identity, name, or stage missing")
    return OpportunityStage(data["id"], data["name"], data["stage"])


class Client:
    """Resolve an API key per call and read selected opportunity metadata."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Lever configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("Lever request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_opportunity(self) -> GetOpportunityOperation:
        """Bind the getOpportunity Query to this client."""
        return GetOpportunityOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get(self, call: Call, opportunity_id: str) -> _Exchange:
        try:
            credential = await self.credentials.resolve(call)
            api_key = credential.api_key.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _Exchange(error="credential")
        if not is_header_safe_credential(api_key) or ":" in api_key:
            return _Exchange(error="credential")
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + "/opportunities/" + quote(opportunity_id, safe=""),
                headers={
                    "Accept": "application/json",
                    "Authorization": "Basic " + base64.b64encode((api_key + ":").encode()).decode(),
                },
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


class GetOpportunityOperation:
    """Read one opportunity's ID, name, and stage."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query opportunity."""
        return GET_OPPORTUNITY_DEFINITION

    async def invoke(self, call: Call, input: GetOpportunityInput) -> QueryAttempt[OpportunityStage]:
        """Classify opportunity lookup outcomes consistently with Go."""
        if not isinstance(input, GetOpportunityInput) or not _valid_opportunity_id(input.opportunity_id):
            return _branch(
                "defect",
                FailureKind.VALIDATION,
                "opportunity ID must be a UUID",
            )
        exchange = await self.client._get(call, input.opportunity_id)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Lever credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Lever request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "Lever rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(FailureKind.AVAILABILITY, "Lever is temporarily unavailable", exchange.retry_after)
        if exchange.status == 404:
            return _branch("notFound", FailureKind.NOT_FOUND, "Lever did not expose the opportunity")
        if exchange.status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"Lever rejected HTTP {exchange.status}",
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Lever response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "Lever response was interrupted")
        try:
            opportunity_stage = _decode_opportunity(exchange.body, input.opportunity_id)
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Lever returned invalid opportunity JSON"
            )
        return QueryAttempt.branch_result("found", opportunity_stage, Receipt())
