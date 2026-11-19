# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider Query implementation with Go-compatible outcomes."""

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
from .models import ListModelsInput, ListModelsOutput, Model


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListModelsOutput]:
    return QueryAttempt.branch_result(
        branch, ListModelsOutput(), Receipt(), Failure(kind, "ppio", "listModels", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListModelsOutput]:
    return QueryAttempt.retry(Failure(kind, "ppio", "listModels", message), delay)


class Client:
    """Own ppio settings and resolve credentials on each call."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("configuration and credentials are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("provider configuration is invalid")

        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_models(self) -> "ListModelsOperation":
        """Bind the listModels Query."""
        return ListModelsOperation(self)

    async def aclose(self) -> None:
        """Close only an HTTP client this instance created."""
        if self._owns_client:
            await self.http_client.aclose()


class ListModelsOperation:
    """Read the ppio provider and classify each outcome."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("provider client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity and retry policy."""
        return LIST_MODELS_DEFINITION

    async def invoke(self, call: Call, input: ListModelsInput) -> QueryAttempt[ListModelsOutput]:
        """Make one read-only provider request."""
        try:
            credentials = await self.client.credentials.resolve(call)
            key = credentials.api_key.reveal()
            if (
                not key
                or not (len(key) <= 1024)
                or any(ord(char) <= 0x20 or ord(char) >= 0x7F for char in key)
            ):
                raise ValueError("invalid key")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch("defect", FailureKind.AUTHENTICATION, "ppio credential is unavailable or invalid")
        try:
            request = self.client.http_client.build_request(
                "GET",
                "https://api.ppio.com/openai/v1/models",
                headers={
                    "Accept": "application/json",
                    "Content-Type": "application/json",
                    "Authorization": f"Bearer {key}",
                },
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "ppio request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "ppio request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "ppio rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "ppio is temporarily unavailable", delay)
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch("providerRejected", kind, f"ppio rejected the request with HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "ppio response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "ppio response was interrupted")
        finally:
            await response.aclose()
        return _decode_response(body)


def _decode_response(body: bytes) -> QueryAttempt[ListModelsOutput]:
    try:
        document: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "PPIO returned invalid model data")
    models = document.get("data") if isinstance(document, dict) else None
    if not isinstance(models, list):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "PPIO returned invalid model data")
    result = []
    for item in models:
        if not isinstance(item, dict):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "PPIO returned invalid model metadata")
        identifier, created, owned, display = (
            item.get("id"),
            item.get("created"),
            item.get("owned_by"),
            item.get("display_name"),
        )
        status, context, endpoints = item.get("status"), item.get("context_size"), item.get("endpoints")
        if (
            not isinstance(identifier, str)
            or not identifier
            or item.get("object") != "model"
            or type(created) is not int
            or created < 0
            or owned != "ppio"
            or not isinstance(display, str)
            or not display
            or type(status) is not int
            or status not in (1, 3, 4)
            or type(context) is not int
            or context <= 0
            or not isinstance(endpoints, list)
            or any(not isinstance(value, str) for value in endpoints)
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "PPIO returned invalid model metadata")
        result.append(Model(identifier, created, owned, display, status, context, tuple(endpoints)))
    return QueryAttempt.branch_result("found", ListModelsOutput(tuple(result)), Receipt())
