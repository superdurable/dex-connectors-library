# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded RabbitMQ queue inspection with Go-compatible outcomes."""

from __future__ import annotations

import base64
import json
import unicodedata
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any
from urllib.parse import quote, urlsplit

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
from ._generated.definitions import GET_QUEUE_METRICS_DEFINITION
from .models import GetQueueMetricsInput, QueueMetrics


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One provider response or request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "rabbitmq", "getQueueMetrics", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[QueueMetrics]:
    return QueryAttempt.branch_result(branch, QueueMetrics(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[QueueMetrics]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_queue_identity(value: object) -> bool:
    if not isinstance(value, str) or not value:
        return False
    try:
        byte_count = len(value.encode("utf-8"))
    except UnicodeEncodeError:
        return False
    return byte_count <= 255 and not any(unicodedata.category(point) == "Cc" for point in value)


def _valid_basic_credential(value: object) -> bool:
    return isinstance(value, str) and is_header_safe_credential(value)


def _decode_queue(body: bytes, input: GetQueueMetricsInput) -> QueueMetrics:
    wire: Any = json.loads(body)
    if not isinstance(wire, dict):
        raise ValueError("queue metrics must be an object")
    messages, consumers = wire.get("messages"), wire.get("consumers")
    state = wire.get("state")
    if (
        wire.get("vhost") != input.virtual_host
        or wire.get("name") != input.name
        or type(messages) is not int
        or type(consumers) is not int
        or messages < 0
        or consumers < 0
        or not isinstance(state, str)
        or not state
    ):
        raise ValueError("queue metrics are missing or inconsistent")
    return QueueMetrics(input.virtual_host, input.name, messages, consumers, state)


class Client:
    """Resolve Basic Auth credentials per call and inspect one queue."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("RabbitMQ configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("RabbitMQ request limits must be positive")
        management_url = validate_base_url(config.management_url).rstrip("/")
        if urlsplit(management_url).path:
            raise ValueError("RabbitMQ Management API URL must be an origin without a path")
        self.management_url = management_url
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_queue_metrics(self) -> GetQueueMetricsOperation:
        """Bind the getQueueMetrics Query to this client."""
        return GetQueueMetricsOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client only when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get(self, call: Call, virtual_host: str, name: str) -> _Exchange:
        try:
            credential = await self.credentials.resolve(call)
            username, password = credential.username, credential.password.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _Exchange(error="credential")
        if not _valid_basic_credential(username) or ":" in username or not _valid_basic_credential(password):
            return _Exchange(error="credential")
        encoded_credential = base64.b64encode(f"{username}:{password}".encode("ascii")).decode("ascii")
        endpoint = (
            self.management_url + "/api/queues/" + quote(virtual_host, safe="") + "/" + quote(name, safe="")
        )
        try:
            request = self.http_client.build_request(
                "GET",
                endpoint,
                headers={"Accept": "application/json", "Authorization": "Basic " + encoded_credential},
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


class GetQueueMetricsOperation:
    """Read current counts for one RabbitMQ queue."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("RabbitMQ client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return GET_QUEUE_METRICS_DEFINITION

    async def invoke(self, call: Call, input: GetQueueMetricsInput) -> QueryAttempt[QueueMetrics]:
        """Read one queue and classify provider outcomes consistently with Go."""
        if (
            not isinstance(input, GetQueueMetricsInput)
            or not _valid_queue_identity(input.virtual_host)
            or not _valid_queue_identity(input.name)
        ):
            return _branch("defect", FailureKind.VALIDATION, "virtual host or queue name is invalid")
        exchange = await self.client._get(call, input.virtual_host, input.name)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "RabbitMQ credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "RabbitMQ request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "RabbitMQ rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(
                FailureKind.AVAILABILITY, "RabbitMQ is temporarily unavailable", exchange.retry_after
            )
        if exchange.status == 404:
            return _branch(
                "notFound", FailureKind.PROVIDER_REJECTION, "RabbitMQ queue or virtual host was not found"
            )
        if exchange.status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"RabbitMQ rejected HTTP {exchange.status}",
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "RabbitMQ response exceeded the configured limit",
            )
        if exchange.error == "interrupted":
            return _retry(FailureKind.TRANSPORT, "RabbitMQ response was interrupted")
        try:
            metrics = _decode_queue(exchange.body, input)
        except (ValueError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "RabbitMQ returned invalid queue metrics")
        return QueryAttempt.branch_result("found", metrics, Receipt())
