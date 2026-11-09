# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded MeisterTask task reads with Go-compatible outcomes."""

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
from ._generated.definitions import GET_TASK_DEFINITION
from .models import GetTaskInput, TaskSummary


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "meistertask", "getTask", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[TaskSummary]:
    return QueryAttempt.branch_result(branch, TaskSummary(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[TaskSummary]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_task_id(task_id: object) -> bool:
    return (
        isinstance(task_id, str)
        and 1 <= len(task_id) <= 10
        and task_id.isascii()
        and task_id[0] != "0"
        and task_id.isdecimal()
        and int(task_id) <= 2147483647
    )


def _decode_task(body: bytes, requested_task_id: str) -> TaskSummary:
    raw: Any = json.loads(body)
    if not isinstance(raw, dict):
        raise ValueError("task data missing")
    if type(raw.get("id")) is not int or str(raw["id"]) != requested_task_id:
        raise ValueError("task identity does not match request")
    status = raw.get("status")
    name = raw.get("name")
    if type(status) is not int or status < 0:
        raise ValueError("task status is invalid")
    if not isinstance(name, str) or not name:
        raise ValueError("task name is invalid")
    return TaskSummary(requested_task_id, name, str(status))


class Client:
    """Resolve an API token per call and read selected task metadata."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("MeisterTask configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("MeisterTask request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_task(self) -> GetTaskOperation:
        """Bind the getTask Query to this client."""
        return GetTaskOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get(self, call: Call, task_id: str) -> _Exchange:
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
                self.base_url + "/tasks/" + quote(task_id, safe=""),
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


class GetTaskOperation:
    """Read one task's ID, status, and name."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query task."""
        return GET_TASK_DEFINITION

    async def invoke(self, call: Call, input: GetTaskInput) -> QueryAttempt[TaskSummary]:
        """Classify task lookup outcomes consistently with Go."""
        if not isinstance(input, GetTaskInput) or not _valid_task_id(input.task_id):
            return _branch(
                "defect",
                FailureKind.VALIDATION,
                "task ID must be a positive 32-bit decimal integer",
            )
        exchange = await self.client._get(call, input.task_id)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "MeisterTask credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "MeisterTask request failed")
        if exchange.status == 429:
            return _retry(
                FailureKind.RATE_LIMIT, "MeisterTask rate limited the request", exchange.retry_after
            )
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(
                FailureKind.AVAILABILITY, "MeisterTask is temporarily unavailable", exchange.retry_after
            )
        if exchange.status == 404:
            return _branch("notFound", FailureKind.NOT_FOUND, "MeisterTask did not expose the task")
        if exchange.status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"MeisterTask rejected HTTP {exchange.status}",
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "MeisterTask response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "MeisterTask response was interrupted")
        try:
            task_summary = _decode_task(exchange.body, input.task_id)
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "MeisterTask returned invalid task JSON")
        return QueryAttempt.branch_result("found", task_summary, Receipt())
