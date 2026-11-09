# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Rippling worker reads with Go-compatible outcomes."""

from __future__ import annotations

import json
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
from ._generated.definitions import GET_WORKER_DEFINITION
from .models import GetWorkerInput, WorkerSummary


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "rippling", "getWorker", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[WorkerSummary]:
    return QueryAttempt.branch_result(branch, WorkerSummary(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[WorkerSummary]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_worker_id(worker_id: object) -> bool:
    return (
        isinstance(worker_id, str)
        and 1 <= len(worker_id) <= 64
        and worker_id.isascii()
        and all(character.isalnum() or character in "-_" for character in worker_id)
    )


def _decode_worker(body: bytes, requested_worker_id: str) -> WorkerSummary:
    raw: Any = json.loads(body)
    if not isinstance(raw, dict):
        raise ValueError("worker data missing")
    if raw.get("id") != requested_worker_id:
        raise ValueError("worker identity does not match request")
    status = raw.get("status")
    title = raw.get("title")
    if status is not None and not isinstance(status, str):
        raise ValueError("worker status is invalid")
    if title is not None and not isinstance(title, str):
        raise ValueError("worker title is invalid")
    return WorkerSummary(requested_worker_id, title or "", status or "")


class Client:
    """Resolve an API token per call and read selected worker metadata."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Rippling configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("Rippling request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_worker(self) -> GetWorkerOperation:
        """Bind the getWorker Query to this client."""
        return GetWorkerOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get(self, call: Call, worker_id: str) -> _Exchange:
        try:
            credential = await self.credentials.resolve(call)
            api_token = credential.api_token.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _Exchange(error="credential")
        if not is_header_safe_credential(api_token):
            return _Exchange(error="credential")
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + "/workers/" + quote(worker_id, safe="") + "/",
                headers={"Accept": "application/json", "Authorization": "Bearer " + api_token},
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


class GetWorkerOperation:
    """Read one worker's ID, status, and title."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query worker."""
        return GET_WORKER_DEFINITION

    async def invoke(self, call: Call, input: GetWorkerInput) -> QueryAttempt[WorkerSummary]:
        """Classify worker lookup outcomes consistently with Go."""
        if not isinstance(input, GetWorkerInput) or not _valid_worker_id(input.worker_id):
            return _branch(
                "defect",
                FailureKind.VALIDATION,
                "worker ID must contain 1–64 URL-safe characters",
            )
        exchange = await self.client._get(call, input.worker_id)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Rippling credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Rippling request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "Rippling rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(
                FailureKind.AVAILABILITY, "Rippling is temporarily unavailable", exchange.retry_after
            )
        if exchange.status == 404:
            return _branch("notFound", FailureKind.NOT_FOUND, "Rippling did not expose the worker")
        if exchange.status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"Rippling rejected HTTP {exchange.status}",
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Rippling response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "Rippling response was interrupted")
        try:
            worker_summary = _decode_worker(exchange.body, input.worker_id)
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Rippling returned invalid worker JSON")
        return QueryAttempt.branch_result("found", worker_summary, Receipt())
