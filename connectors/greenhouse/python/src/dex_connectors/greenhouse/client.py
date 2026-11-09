# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Greenhouse Harvest v3 candidate lookup with Go-compatible outcomes."""

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
from ._generated.definitions import GET_CANDIDATE_DEFINITION
from .models import CandidateProfile, GetCandidateInput


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One provider response or sanitized request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "greenhouse", "getCandidate", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[CandidateProfile]:
    return QueryAttempt.branch_result(branch, CandidateProfile(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[CandidateProfile]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _decode_candidate(body: bytes, requested_id: int) -> tuple[CandidateProfile, bool]:
    raw: Any = json.loads(body)
    if not isinstance(raw, list) or len(raw) > 1:
        raise ValueError("invalid candidate list")
    if not raw:
        return CandidateProfile(), False
    record = raw[0]
    if not isinstance(record, dict) or type(record.get("id")) is not int or record["id"] != requested_id:
        raise ValueError("candidate ID mismatch")
    first_name = record.get("first_name")
    last_name = record.get("last_name")
    if first_name is not None and not isinstance(first_name, str):
        raise ValueError("invalid first name")
    if last_name is not None and not isinstance(last_name, str):
        raise ValueError("invalid last name")
    first_name = first_name or ""
    last_name = last_name or ""
    if not first_name.strip() and not last_name.strip():
        raise ValueError("candidate name missing")
    return CandidateProfile(requested_id, first_name, last_name), True


class Client:
    """Resolve access tokens per call and read selected Greenhouse candidate fields."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Greenhouse configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("Greenhouse request limits must be positive")
        base_url = validate_base_url(config.base_url).rstrip("/")
        if not urlsplit(base_url).path.endswith("/v3"):
            raise ValueError("Greenhouse Harvest URL must end in /v3")
        self.base_url = base_url
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_candidate(self) -> GetCandidateOperation:
        """Bind the getCandidate Query to this client."""
        return GetCandidateOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client only when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get(self, call: Call, candidate_id: int) -> _Exchange:
        try:
            credential = await self.credentials.resolve(call)
            token = credential.access_token.reveal()
        except ReauthorizationRequiredError:
            return _Exchange(error="credential")
        except Exception:
            return _Exchange(error="request")
        if not is_header_safe_credential(token):
            return _Exchange(error="credential")
        for attempt in range(2):
            exchange = await self._send(call, token, candidate_id)
            if (
                exchange.status != 401
                or attempt > 0
                or not hasattr(self.credentials, "resolve_after_rejection")
            ):
                return exchange
            try:
                replacement = await self.credentials.resolve_after_rejection(call)
                token = replacement.access_token.reveal()
            except ReauthorizationRequiredError:
                return exchange
            except Exception:
                return _Exchange(error="request")
            if not is_header_safe_credential(token):
                return exchange
        return _Exchange(error="request")

    async def _send(self, call: Call, token: str, candidate_id: int) -> _Exchange:
        query = urlencode({"fields": "id,first_name,last_name", "ids": str(candidate_id), "per_page": "1"})
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + "/candidates?" + query,
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


class GetCandidateOperation:
    """Read one candidate's ID and names from Greenhouse."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Greenhouse client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return GET_CANDIDATE_DEFINITION

    async def invoke(self, call: Call, input: GetCandidateInput) -> QueryAttempt[CandidateProfile]:
        """Classify candidate lookup outcomes consistently with Go."""
        if (
            not isinstance(input, GetCandidateInput)
            or type(input.candidate_id) is not int
            or input.candidate_id <= 0
        ):
            return _branch("defect", FailureKind.VALIDATION, "candidate ID must be positive")
        exchange = await self.client._get(call, input.candidate_id)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Greenhouse access token is unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Greenhouse request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "Greenhouse rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(
                FailureKind.AVAILABILITY, "Greenhouse is temporarily unavailable", exchange.retry_after
            )
        if exchange.status == 404:
            return _branch("notFound", FailureKind.NOT_FOUND, "Greenhouse did not expose the candidate")
        if exchange.status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"Greenhouse rejected HTTP {exchange.status}",
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Greenhouse response exceeded the configured limit",
            )
        if exchange.error == "interrupted":
            return _retry(FailureKind.TRANSPORT, "Greenhouse response was interrupted")
        try:
            profile, found = _decode_candidate(exchange.body, input.candidate_id)
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Greenhouse returned invalid candidate JSON"
            )
        if not found:
            return _branch("notFound", FailureKind.NOT_FOUND, "Greenhouse did not expose the candidate")
        return QueryAttempt.branch_result("found", profile, Receipt())
