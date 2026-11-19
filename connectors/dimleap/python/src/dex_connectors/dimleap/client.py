# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Dimleap model catalog Query with Go-compatible outcome classification."""

from __future__ import annotations

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
from ._generated.definitions import CONNECTOR_ID as CONNECTOR_ID
from ._generated.definitions import DEFECT as DEFECT
from ._generated.definitions import FOUND as FOUND
from ._generated.definitions import INVALID_RESPONSE as INVALID_RESPONSE
from ._generated.definitions import LIST_MODELS_DEFINITION as LIST_MODELS_DEFINITION
from ._generated.definitions import LIST_MODELS_OPERATION_ID as LIST_MODELS_OPERATION_ID
from ._generated.definitions import PROVIDER_REJECTED as PROVIDER_REJECTED
from .models import ListModelsInput, ListModelsOutput

_MODELS_ENDPOINT = "https://api.dimleap.cn/v1/models"
_DEFAULT_TIMEOUT_SECONDS = 15
_DEFAULT_MAX_RESPONSE_BYTES = 1_048_576
_MAX_RESPONSE_BYTES = 16 << 20


def _valid_api_key(value: str) -> bool:
    return bool(value) and len(value.encode()) <= 1_024 and value.strip() == value and all(
        33 <= ord(character) <= 126 for character in value
    )


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, CONNECTOR_ID, LIST_MODELS_OPERATION_ID, message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListModelsOutput]:
    return QueryAttempt.branch_result(branch, ListModelsOutput(), Receipt(), _failure(kind, message))


def _wire_field(document: dict[str, Any], name: str) -> Any:
    if name in document:
        return document[name]
    return next((value for key, value in document.items() if key.casefold() == name.casefold()), None)


class Client:
    """Own fixed HTTPS provider endpoint and resolve an API Key for each list call."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        """Validate response bounds and supply a no-redirect provider HTTP client."""
        if config is None or credentials is None:
            raise ValueError("Dimleap settings and credential source are required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("Dimleap configuration is invalid")
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        if self.max_response_bytes > _MAX_RESPONSE_BYTES:
            raise ValueError("Dimleap configuration is invalid")
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def list_models(self) -> ListModelsOperation:
        """Bind the read-only model catalog Query."""
        return ListModelsOperation(self)

    async def aclose(self) -> None:
        """Close the provider HTTP client after Worker shutdown."""
        await self.http_client.aclose()


class ListModelsOperation:
    """Read the API Key's available model IDs without returning provider metadata."""

    def __init__(self, client: Client) -> None:
        """Bind a validated Dimleap client."""
        if client is None:
            raise ValueError("Dimleap client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return generated Query identity and branches."""
        return LIST_MODELS_DEFINITION

    async def invoke(self, call: Call, input: ListModelsInput) -> QueryAttempt[ListModelsOutput]:
        """GET the current dynamic catalog and classify provider outcomes."""
        try:
            credentials = await self.client.credentials.resolve(call)
            api_key = credentials.api_key.reveal()
        except Exception:  # noqa: BLE001
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "Dimleap API Key is unavailable or invalid"
            )
        if not _valid_api_key(api_key):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "Dimleap API Key is unavailable or invalid"
            )
        try:
            request = self.client.http_client.build_request(
                "GET", _MODELS_ENDPOINT,
                headers={"Authorization": "Bearer " + api_key, "Accept": "application/json"},
            )
        except (httpx.HTTPError, ValueError, TypeError):
            return _branch(DEFECT, FailureKind.VALIDATION, "Dimleap model request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return QueryAttempt.retry(
                _failure(FailureKind.AVAILABILITY, "Dimleap model request failed")
            )
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return QueryAttempt.retry(
                    _failure(FailureKind.RATE_LIMIT, "Dimleap rate limited the model request"),
                    retry_after,
                )
            if status == 408 or status >= 500:
                return QueryAttempt.retry(
                    _failure(FailureKind.AVAILABILITY, "Dimleap API is temporarily unavailable"),
                    retry_after,
                )
            if status != 200:
                kind = FailureKind.PROVIDER_REJECTION
                if status == 401:
                    kind = FailureKind.AUTHENTICATION
                if status == 403:
                    kind = FailureKind.AUTHORIZATION
                return _branch(
                    PROVIDER_REJECTED, kind, f"Dimleap rejected model request with HTTP {status}"
                )
            try:
                content = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE,
                    "Dimleap response exceeded the configured limit",
                )
            except ProviderTransportError:
                return QueryAttempt.retry(
                    _failure(FailureKind.AVAILABILITY, "Dimleap response was interrupted")
                )
            try:
                document = json.loads(content)
                if not isinstance(document, dict):
                    raise ValueError("model response is not an object")
            except (ValueError, UnicodeDecodeError):
                return _branch(
                        INVALID_RESPONSE, FailureKind.PROTOCOL, "Dimleap returned invalid model data"
                    )
            models = _wire_field(document, "data")
            if _wire_field(document, "object") != "list" or not isinstance(models, list):
                return _branch(
                        INVALID_RESPONSE, FailureKind.PROTOCOL, "Dimleap returned invalid model data"
                    )
            model_ids: list[str] = []
            for model in models:
                if not isinstance(model, dict):
                    return _branch(
                        INVALID_RESPONSE, FailureKind.PROTOCOL, "Dimleap returned invalid model data"
                    )
                model_id = _wire_field(model, "id")
                if (
                    _wire_field(model, "object") != "model"
                    or not isinstance(model_id, str)
                    or not model_id
                    or len(model_id.encode()) > 256
                    or any(character in model_id for character in ("\r", "\n", "\x00"))
                ):
                    return _branch(
                        INVALID_RESPONSE, FailureKind.PROTOCOL, "Dimleap returned invalid model ID"
                    )
                if len(model_ids) < 50:
                    model_ids.append(model_id)
            return QueryAttempt.branch_result(
                FOUND, ListModelsOutput(len(models), tuple(model_ids)), Receipt()
            )
        finally:
            await response.aclose()


Config.__doc__ = "Dimleap request timeout and response bounds; zero uses manifest defaults."
Credentials.__doc__ = "Private API Key resolved for each catalog request."
