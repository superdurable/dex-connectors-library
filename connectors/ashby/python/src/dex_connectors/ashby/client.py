# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Ashby candidate reads with Go-compatible outcomes."""

from __future__ import annotations

import base64
import json
import re
from dataclasses import dataclass
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
    """One bounded provider response or request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "ashby", "getCandidate", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[CandidateProfile]:
    return QueryAttempt.branch_result(branch, CandidateProfile(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[CandidateProfile]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_candidate_id(candidate_id: object) -> bool:
    return isinstance(candidate_id, str) and re.fullmatch(
        r"[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}",
        candidate_id,
    ) is not None


def _decode_candidate(body: bytes, requested_candidate_id: str) -> tuple[CandidateProfile, str]:
    raw: Any = json.loads(body)
    if not isinstance(raw, dict) or not isinstance(raw.get("success"), bool):
        raise ValueError("success missing")
    if not raw["success"]:
        error_info = raw.get("errorInfo")
        if (
            not isinstance(error_info, dict)
            or not isinstance(error_info.get("code"), str)
            or not error_info["code"].strip()
        ):
            raise ValueError("error code missing")
        return CandidateProfile(), error_info["code"]
    data = raw.get("results")
    # UUID hex digits compare without case, matching Go strings.EqualFold.
    if (
        not isinstance(data, dict)
        or not isinstance(data.get("id"), str)
        or data["id"].lower() != requested_candidate_id.lower()
    ) or any(
        not isinstance(data.get(field), str) or not data[field].strip()
        for field in ("name", "createdAt", "updatedAt")
    ):
        raise ValueError("candidate identity or required profile fields missing")
    return CandidateProfile(data["id"], data["name"], data["createdAt"], data["updatedAt"]), ""


class Client:
    """Resolve an API key per call and read selected candidate metadata."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Ashby configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("Ashby request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_candidate(self) -> GetCandidateOperation:
        """Bind the getCandidate Query to this client."""
        return GetCandidateOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get(self, call: Call, candidate_id: str) -> _Exchange:
        try:
            credential = await self.credentials.resolve(call)
            api_key = credential.api_key.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _Exchange(error="credential")
        if not is_header_safe_credential(api_key) or ":" in api_key:
            return _Exchange(error="credential")
        try:
            request = self.http_client.build_request(
                "POST",
                self.base_url + "/candidate.info",
                json={"id": candidate_id},
                headers={
                    "Accept": "application/json; version=1",
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


class GetCandidateOperation:
    """Read one candidate's ID, name, and timestamps."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query candidate."""
        return GET_CANDIDATE_DEFINITION

    async def invoke(self, call: Call, input: GetCandidateInput) -> QueryAttempt[CandidateProfile]:
        """Classify candidate lookup outcomes consistently with Go."""
        if not isinstance(input, GetCandidateInput) or not _valid_candidate_id(input.candidate_id):
            return _branch(
                "defect",
                FailureKind.VALIDATION,
                "candidate ID must be a UUID",
            )
        exchange = await self.client._get(call, input.candidate_id)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Ashby credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Ashby request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "Ashby rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(FailureKind.AVAILABILITY, "Ashby is temporarily unavailable", exchange.retry_after)
        if exchange.status == 404:
            return _branch("notFound", FailureKind.NOT_FOUND, "Ashby did not expose the candidate")
        if exchange.status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"Ashby rejected HTTP {exchange.status}",
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Ashby response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "Ashby response was interrupted")
        try:
            candidate_profile, rejection_code = _decode_candidate(exchange.body, input.candidate_id)
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Ashby returned invalid candidate JSON"
            )
        if rejection_code == "candidate_not_found":
            return _branch("notFound", FailureKind.NOT_FOUND, "Ashby did not expose the candidate")
        if rejection_code:
            return _branch(
                "providerRejected", FailureKind.PROVIDER_REJECTION, "Ashby rejected the candidate lookup"
            )
        return QueryAttempt.branch_result("found", candidate_profile, Receipt())
