# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Infini AI model catalog transport and Go-compatible decisions."""

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
from .models import ListModelsInput, ListModelsOutput, ModelInfo

_ENDPOINT = "https://cloud.infini-ai.com/maas/v1/models"


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListModelsOutput]:
    return QueryAttempt.branch_result(
        branch, ListModelsOutput(), Receipt(), Failure(kind, "infiniai", "listModels", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListModelsOutput]:
    return QueryAttempt.retry(Failure(kind, "infiniai", "listModels", message), delay)


def _valid_api_key(value: str) -> bool:
    return (
        value.startswith("sk-")
        and len(value) <= 1024
        and all(0x20 < ord(character) < 0x7F for character in value)
    )


class Client:
    """Own the HTTP transport and resolve the Infini AI Key on every attempt."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Infini AI configuration and credential source are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("Infini AI configuration is invalid")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_models(self) -> "ListModelsOperation":
        """Bind the available-model Query."""
        return ListModelsOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP transport if this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class ListModelsOperation:
    """List at most fifty model IDs visible to the API Key."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Infini AI client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated model-list Query identity."""
        return LIST_MODELS_DEFINITION

    async def invoke(self, call: Call, input: ListModelsInput) -> QueryAttempt[ListModelsOutput]:
        """Make one model-list request and classify its provider outcome."""
        try:
            credentials = await self.client.credentials.resolve(call)
            api_key = credentials.api_key.reveal()
            if not _valid_api_key(api_key):
                raise ValueError("invalid API Key")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Infini AI API Key is unavailable or invalid"
            )
        try:
            request = self.client.http_client.build_request(
                "GET", _ENDPOINT, headers={"Authorization": "Bearer " + api_key, "Accept": "application/json"}
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "Infini AI model request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Infini AI model request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Infini AI rate limited the model request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Infini AI API is temporarily unavailable", delay)
            if status != 200:
                kind = FailureKind.PROVIDER_REJECTION
                if status == 401:
                    kind = FailureKind.AUTHENTICATION
                elif status == 403:
                    kind = FailureKind.AUTHORIZATION
                return _branch(
                    "providerRejected", kind, f"Infini AI rejected model request with HTTP {status}"
                )
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Infini AI response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Infini AI response was interrupted")
        finally:
            await response.aclose()
        return _decode_response(body, api_key)


def _decode_response(body: bytes, api_key: str) -> QueryAttempt[ListModelsOutput]:
    try:
        document: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "GenStudio returned invalid model list")
    if (
        not isinstance(document, dict)
        or document.get("object") != "list"
        or not isinstance(document.get("data"), list)
    ):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "GenStudio returned invalid model list")
    models: list[ModelInfo] = []
    for item in document["data"]:
        if not isinstance(item, dict):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "GenStudio returned invalid model data")
        model_id = item.get("id")
        model_type = item.get("model_type")
        context_length = item.get("context_length")
        max_output_length = item.get("max_output_length")
        if (
            not isinstance(model_id, str)
            or not model_id
            or len(model_id.encode()) > 512
            or item.get("object") != "model"
            or type(item.get("created")) is not int
            or not isinstance(item.get("owned_by"), str)
            or not isinstance(model_type, str)
            or not model_type
            or len(model_type.encode()) > 128
            or type(context_length) is not int
            or context_length < 0
            or type(max_output_length) is not int
            or max_output_length < 0
            or api_key in model_id
            or api_key in model_type
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "GenStudio returned invalid model data")
        models.append(
            ModelInfo(
                id=model_id,
                model_type=model_type,
                context_length=context_length,
                max_output_length=max_output_length,
            )
        )
    return QueryAttempt.branch_result("listed", ListModelsOutput(models=tuple(models)), Receipt())
