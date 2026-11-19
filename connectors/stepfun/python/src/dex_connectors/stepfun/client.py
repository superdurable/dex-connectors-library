# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded StepFun model catalog matching the Go provider contract."""

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
from ._generated.definitions import LIST_MODELS_DEFINITION
from .models import ListModelsInput, ListModelsOutput, Model

_ENDPOINT = "https://api.stepfun.com/v1/models"


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListModelsOutput]:
    return QueryAttempt.branch_result(
        branch, ListModelsOutput(), Receipt(), Failure(kind, "stepfun", "listModels", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListModelsOutput]:
    return QueryAttempt.retry(Failure(kind, "stepfun", "listModels", message), delay)


def _valid_api_key(value: str) -> bool:
    return (
        bool(value)
        and len(value.encode()) <= 1024
        and value.strip() == value
        and all(0x20 < ord(c) < 0x7F for c in value)
    )


class Client:
    """Own the HTTP transport and resolve the StepFun API Key per attempt."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or not 0 < config.max_response_bytes <= 16 << 20
        ):
            raise ValueError("StepFun configuration and credential source are required")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_models(self) -> "ListModelsOperation":
        """Bind the available-model Query."""
        return ListModelsOperation(self)

    async def aclose(self) -> None:
        """Close the provider transport only if this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class ListModelsOperation:
    """List at most fifty model IDs visible to the API Key."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("StepFun client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated model-list Query contract."""
        return LIST_MODELS_DEFINITION

    async def invoke(self, call: Call, input: ListModelsInput) -> QueryAttempt[ListModelsOutput]:
        """Classify one model-list response and safe retry outcomes."""
        try:
            credentials = await self.client.credentials.resolve(call)
            api_key = credentials.api_key.reveal()
            if not _valid_api_key(api_key):
                raise ValueError("invalid API Key")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch("defect", FailureKind.AUTHENTICATION, "StepFun API Key is unavailable or invalid")
        try:
            request = self.client.http_client.build_request(
                "GET", _ENDPOINT, headers={"Authorization": "Bearer " + api_key, "Accept": "application/json"}
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "StepFun model request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "StepFun model-list request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "StepFun rate limited the model-list request", delay)
            if status == 408 or status >= 500:
                return _retry(
                    FailureKind.AVAILABILITY, "StepFun model list is temporarily unavailable", delay
                )
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch("providerRejected", kind, f"StepFun rejected model request with HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "StepFun response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "StepFun model-list response was interrupted")
        finally:
            await response.aclose()
        try:
            wire = json.loads(body, parse_constant=lambda _: (_ for _ in ()).throw(ValueError()))
            if (
                not isinstance(wire, dict)
                or wire.get("object") != "list"
                or not isinstance(wire.get("data"), list)
            ):
                raise ValueError("invalid model list")
            models: list[Model] = []
            for item in wire["data"]:
                if not isinstance(item, dict) or item.get("object") != "model":
                    raise ValueError("invalid model")
                model_id, created, owned_by = item.get("id"), item.get("created"), item.get("owned_by")
                if (
                    not isinstance(model_id, str)
                    or not model_id
                    or type(created) is not int
                    or created < 0
                    or not isinstance(owned_by, str)
                    or not owned_by
                ):
                    raise ValueError("invalid model metadata")
                models.append(Model(id=model_id, created_at_unix=created, owned_by=owned_by))
            output = ListModelsOutput(models=tuple(models))
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "StepFun returned invalid model data")
        return QueryAttempt.branch_result("found", output, Receipt())
