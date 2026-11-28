# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Greenhouse Harvest v3 candidate lookup with Go-compatible outcomes."""

from __future__ import annotations

import json
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any
from urllib.parse import parse_qs, urlencode, urlsplit

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
from ._generated.definitions import GET_CANDIDATE_DEFINITION, LIST_CANDIDATES_DEFINITION
from .models import CandidateProfile, CandidateProfilesPage, GetCandidateInput, ListCandidatesInput


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One provider response or sanitized request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""
    link: str = ""


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

    def list_candidates(self) -> ListCandidatesOperation:
        """Read a page of candidate profiles from the authorized Harvest connection."""
        return ListCandidatesOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client only when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get(self, call: Call, query: dict[str, str]) -> _Exchange:
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
            exchange = await self._send(call, token, query)
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

    async def _send(self, call: Call, token: str, query: dict[str, str]) -> _Exchange:
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + "/candidates?" + urlencode(query),
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
            return _Exchange(
                status=response.status_code,
                body=body,
                retry_after=retry_after,
                link=response.headers.get("Link", ""),
            )
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
        exchange = await self.client._get(
            call, {"fields": "id,first_name,last_name", "ids": str(input.candidate_id), "per_page": "1"}
        )
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


class ListCandidatesOperation:
    """Read candidate IDs and names for one Harvest page."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest contract for candidate pagination."""
        return LIST_CANDIDATES_DEFINITION

    async def invoke(self, call: Call, input: ListCandidatesInput) -> QueryAttempt[CandidateProfilesPage]:
        """Use the preceding cursor or request a first page of up to 500 profiles."""

        def branch(name: str, kind: FailureKind, message: str) -> QueryAttempt[CandidateProfilesPage]:
            return QueryAttempt.branch_result(
                name,
                CandidateProfilesPage(),
                Receipt(),
                Failure(kind, "greenhouse", "listCandidates", message),
            )

        def retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[CandidateProfilesPage]:
            return QueryAttempt.retry(Failure(kind, "greenhouse", "listCandidates", message), delay)

        if (
            not isinstance(input, ListCandidatesInput)
            or not isinstance(input.cursor, str)
            or type(input.page_size) is not int
            or not 0 <= input.page_size <= 500
            or (
                input.cursor
                and (input.page_size != 0 or len(input.cursor.encode()) > 4096 or not input.cursor.strip())
            )
        ):
            return branch("defect", FailureKind.VALIDATION, "page size or cursor is invalid")
        if input.cursor:
            query = {"cursor": input.cursor}
        else:
            query = {"fields": "id,first_name,last_name"}
            if input.page_size:
                query["per_page"] = str(input.page_size)
        exchange = await self.client._get(call, query)
        if exchange.error == "credential":
            return branch(
                "defect", FailureKind.AUTHENTICATION, "Greenhouse access token is unavailable or invalid"
            )
        if exchange.error == "request":
            return retry(FailureKind.TRANSPORT, "Greenhouse request failed")
        if exchange.status == 429:
            return retry(FailureKind.RATE_LIMIT, "Greenhouse rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return retry(
                FailureKind.AVAILABILITY, "Greenhouse is temporarily unavailable", exchange.retry_after
            )
        if exchange.status != 200:
            return branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"Greenhouse rejected HTTP {exchange.status}",
            )
        if exchange.error == "too_large":
            return branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Greenhouse response exceeded the configured limit",
            )
        if exchange.error:
            return retry(FailureKind.TRANSPORT, "Greenhouse response was interrupted")
        try:
            page = _candidate_page(exchange, self.client.base_url)
        except (ValueError, UnicodeDecodeError):
            return branch(
                "invalidResponse", FailureKind.PROTOCOL, "Greenhouse returned an invalid candidate page"
            )
        return QueryAttempt.branch_result("listed", page, Receipt())


def _candidate_page(exchange: _Exchange, base_url: str) -> CandidateProfilesPage:
    rows = json.loads(exchange.body)
    if not isinstance(rows, list) or len(rows) > 500:
        raise ValueError("invalid candidate list")
    profiles = []
    for row in rows:
        if not isinstance(row, dict) or type(row.get("id")) is not int or not 0 < row["id"] < 1 << 63:
            raise ValueError("invalid candidate ID")
        first, last = row.get("first_name"), row.get("last_name")
        if (first is not None and not isinstance(first, str)) or (
            last is not None and not isinstance(last, str)
        ):
            raise ValueError("invalid candidate name")
        profiles.append(CandidateProfile(row["id"], first or "", last or ""))
    for link in exchange.link.split(","):
        parts = link.strip().split(";")
        if len(parts) != 2 or parts[1].strip() != 'rel="next"':
            continue
        target = parts[0].strip()
        if not target.startswith("<") or not target.endswith(">"):
            raise ValueError("invalid next link")
        parsed = urlsplit(target[1:-1])
        query = parse_qs(parsed.query, keep_blank_values=True)
        if (
            f"{parsed.scheme}://{parsed.netloc}{parsed.path}" != base_url + "/candidates"
            or set(query) != {"cursor"}
            or not query["cursor"][0]
        ):
            raise ValueError("invalid next cursor")
        return CandidateProfilesPage(tuple(profiles), query["cursor"][0])
    return CandidateProfilesPage(tuple(profiles))
