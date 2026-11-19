# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Kokoni World provider transport and Go-compatible Query decisions."""

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
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import LIST_MODELS_DEFINITION
from .models import ListModelsInput, ListModelsOutput


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListModelsOutput]:
    return QueryAttempt.branch_result(
        branch, ListModelsOutput(), Receipt(), Failure(kind, "kokoniworld", "listModels", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListModelsOutput]:
    return QueryAttempt.retry(Failure(kind, "kokoniworld", "listModels", message), delay)


def _ascii_value(value: str, limit: int, allow_empty: bool = False) -> bool:
    return (
        isinstance(value, str)
        and (allow_empty or bool(value))
        and len(value.encode()) <= limit
        and all(0x20 < ord(c) < 0x7F for c in value)
    )


class Client:
    """Resolve credentials per listModels request and own a redirect-safe HTTP client."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Kokoni World configuration and credentials are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("Kokoni World timeout or response limit is invalid")

        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_models(self) -> "ListModelsOperation":
        """Bind the listModels Query to this client."""
        return ListModelsOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client if this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class ListModelsOperation:
    """Perform one listModels read and filter the provider response."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Kokoni World client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Go-derived Query identity."""
        return LIST_MODELS_DEFINITION

    async def invoke(self, call: Call, input: ListModelsInput) -> QueryAttempt[ListModelsOutput]:
        """Classify transport and provider outcomes without exposing secrets."""
        try:
            credentials = await self.client.credentials.resolve(call)
            key = credentials.api_key.reveal()
            if not key.startswith("sk-") or not _ascii_value(key, 1024):
                return _branch(
                    "defect", FailureKind.AUTHENTICATION, "Kokoni World API Key is unavailable or invalid"
                )
            request = self.client.http_client.build_request(
                "GET",
                "https://api.kokoniworld.com/v1/models",
                headers={"Accept": "application/json", "Authorization": f"Bearer {key}"},
            )
        except (ValueError, LookupError, OSError, httpx.HTTPError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Kokoni World credentials or request are invalid"
            )
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Kokoni World request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Kokoni World rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Kokoni World is temporarily unavailable", delay)
            if status != 200:
                kind = FailureKind.PROVIDER_REJECTION
                if status == 401:
                    kind = FailureKind.AUTHENTICATION
                elif status == 403:
                    kind = FailureKind.AUTHORIZATION
                return _branch("providerRejected", kind, f"Kokoni World rejected request with HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Kokoni World response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Kokoni World response was interrupted")
        finally:
            await response.aclose()

        try:
            document: Any = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Kokoni World returned invalid JSON")
        data = document.get("data") if isinstance(document, dict) else None
        if not isinstance(document, dict) or document.get("object") != "list" or not isinstance(data, list):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Kokoni World returned invalid model data"
            )
        model_ids: list[str] = []
        for model in data:
            if (
                not isinstance(model, dict)
                or model.get("object") != "model"
                or not isinstance(model.get("id"), str)
                or not model["id"]
                or len(model["id"].encode()) > 256
                or any(c in model["id"] for c in "\r\n\0")
            ):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "Kokoni World returned invalid model ID"
                )
            if len(model_ids) < 50:
                model_ids.append(model["id"])
        return QueryAttempt.branch_result(
            "found", ListModelsOutput(total=len(data), model_ids=tuple(model_ids)), Receipt()
        )
