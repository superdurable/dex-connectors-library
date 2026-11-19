# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded FluRouter model catalog matching the Go provider contract."""

from __future__ import annotations

import json
from datetime import UTC, datetime

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
from ._generated.definitions import (
    CONNECTOR_ID,
    DEFECT,
    FOUND,
    INVALID_RESPONSE,
    LIST_MODELS_DEFINITION,
    LIST_MODELS_OPERATION_ID,
    PROVIDER_REJECTED,
)
from .models import ListModelsInput, ListModelsOutput

_MODELS_ENDPOINT = "https://flurouter.net/v1/models"


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, CONNECTOR_ID, LIST_MODELS_OPERATION_ID, message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListModelsOutput]:
    return QueryAttempt.branch_result(branch, ListModelsOutput(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListModelsOutput]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_api_key(value: object) -> bool:
    return (
        isinstance(value, str)
        and value.startswith("sk-il-")
        and len(value.encode()) <= 1024
        and all("!" <= character <= "~" for character in value)
    )


def _valid_model_id(value: object) -> bool:
    return (
        isinstance(value, str)
        and 0 < len(value.encode()) <= 256
        and all("!" <= character <= "~" for character in value)
    )


def _parse_models(body: bytes) -> ListModelsOutput:
    document = json.loads(body, parse_constant=lambda _: (_ for _ in ()).throw(ValueError()))
    if not isinstance(document, dict) or document.get("object") != "list":
        raise ValueError("model catalog is invalid")
    data = document.get("data")
    if not isinstance(data, list) or len(data) > 1000:
        raise ValueError("model list is invalid")
    model_ids: list[str] = []
    seen: set[str] = set()
    for entry in data:
        if not isinstance(entry, dict) or entry.get("object") != "model":
            raise ValueError("model item is invalid")
        model_id = entry.get("id")
        if not isinstance(model_id, str) or not _valid_model_id(model_id) or model_id in seen:
            raise ValueError("model ID is invalid or duplicated")
        seen.add(model_id)
        model_ids.append(model_id)
    return ListModelsOutput(tuple(model_ids))


class Client:
    """Own a bounded HTTP transport and resolve the current FluRouter API Key."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        """Validate request limits and the credential source."""
        if config is None or credentials is None:
            raise ValueError("FluRouter settings and credential source are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("FluRouter timeout and maxResponseBytes are invalid")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)

    def list_models(self) -> ListModelsOperation:
        """Bind the model-list Query to this client."""
        return ListModelsOperation(self)

    async def aclose(self) -> None:
        """Close the provider transport after Worker shutdown."""
        await self.http_client.aclose()


class ListModelsOperation:
    """Read model IDs visible to the connected FluRouter API Key."""

    def __init__(self, client: Client) -> None:
        """Bind a validated FluRouter client."""
        if client is None:
            raise ValueError("FluRouter client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated model-list Query contract."""
        return LIST_MODELS_DEFINITION

    async def invoke(self, call: Call, input: ListModelsInput) -> QueryAttempt[ListModelsOutput]:
        """Classify one model-list response and safe retry outcomes."""
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError):
            return _branch(DEFECT, FailureKind.AUTHENTICATION, "FluRouter API Key is unavailable or invalid")
        api_key = credentials.api_key.reveal()
        if not _valid_api_key(api_key):
            return _branch(DEFECT, FailureKind.AUTHENTICATION, "FluRouter API Key is unavailable or invalid")
        try:
            request = self.client.http_client.build_request(
                "GET",
                _MODELS_ENDPOINT,
                headers={"Authorization": "Bearer " + api_key, "Accept": "application/json"},
            )
        except (httpx.HTTPError, ValueError, TypeError):
            return _branch(DEFECT, FailureKind.VALIDATION, "FluRouter models request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "FluRouter models request failed")
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "FluRouter rate limited the request", retry_after)
            if status == 408 or status >= 500:
                return _retry(
                    FailureKind.AVAILABILITY,
                    "FluRouter models service is temporarily unavailable",
                    retry_after,
                )
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch(
                    PROVIDER_REJECTED, kind, f"FluRouter rejected models request with HTTP {status}"
                )
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "FluRouter response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "FluRouter response was interrupted")
            try:
                output = _parse_models(body)
            except (ValueError, TypeError, UnicodeDecodeError):
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL, "FluRouter returned invalid model data"
                )
            return QueryAttempt.branch_result(FOUND, output, Receipt())
        finally:
            await response.aclose()


Config.__doc__ = "FluRouter request timeout in seconds and response bound in bytes."
Credentials.__doc__ = "Secret sk-il- API Key, resolved afresh for each Query."
