# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""365 API model catalog transport and Go-compatible decisions."""

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

_ENDPOINT = "https://www.hby365.studio/v1/models"


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListModelsOutput]:
    return QueryAttempt.branch_result(
        branch, ListModelsOutput(), Receipt(), Failure(kind, "hby365", "listModels", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListModelsOutput]:
    return QueryAttempt.retry(Failure(kind, "hby365", "listModels", message), delay)


def _valid_api_key(value: str) -> bool:
    return bool(value) and len(value) <= 1024 and all(0x20 < ord(character) < 0x7F for character in value)


class Client:
    """Own the HTTP transport and resolve the 365 API Key on every attempt."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("365 API configuration and credential source are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("365 API configuration is invalid")
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
            raise ValueError("365 API client is required")
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
            return _branch("defect", FailureKind.AUTHENTICATION, "365 API API Key is unavailable or invalid")
        try:
            request = self.client.http_client.build_request(
                "GET", _ENDPOINT, headers={"Authorization": "Bearer " + api_key, "Accept": "application/json"}
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "365 API model request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "365 API model request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "365 API rate limited the model request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "365 API API is temporarily unavailable", delay)
            if status != 200:
                kind = FailureKind.PROVIDER_REJECTION
                if status == 401:
                    kind = FailureKind.AUTHENTICATION
                elif status == 403:
                    kind = FailureKind.AUTHORIZATION
                return _branch("providerRejected", kind, f"365 API rejected model request with HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse", FailureKind.RESPONSE_TOO_LARGE,
                    "365 API response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "365 API response was interrupted")
        finally:
            await response.aclose()
        return _decode_response(body)


def _decode_response(body: bytes) -> QueryAttempt[ListModelsOutput]:
    try:
        document: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "365 API returned invalid model data")
    if not isinstance(document, dict) or document.get("object") != "list" or not isinstance(
        document.get("data"), list
    ):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "365 API returned invalid model data")
    model_ids: list[str] = []
    for model in document["data"]:
        if not isinstance(model, dict):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "365 API returned invalid model ID")
        model_id = model.get("id")
        if (
            model.get("object") != "model"
            or not isinstance(model_id, str)
            or not model_id
            or len(model_id.encode("utf-8")) > 256
            or any(character in model_id for character in "\r\n\x00")
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "365 API returned invalid model ID")
        if len(model_ids) < 50:
            model_ids.append(model_id)
    return QueryAttempt.branch_result(
        "found", ListModelsOutput(len(document["data"]), tuple(model_ids)), Receipt()
    )
