# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""SCNet model identifier directory Query."""

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
from .models import ListModelsInput, ListModelsOutput

_ENDPOINT = "https://api.scnet.cn/api/llm/v1/models"


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListModelsOutput]:
    return QueryAttempt.branch_result(
        branch, ListModelsOutput(), Receipt(), Failure(kind, "scnet", "listModels", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListModelsOutput]:
    return QueryAttempt.retry(Failure(kind, "scnet", "listModels", message), delay)


def _valid_key(value: str) -> bool:
    return 0 < len(value) <= 1024 and value.strip() == value and all(0x20 < ord(c) < 0x7F for c in value)


class Client:
    """Own SCNet HTTP limits and resolve the API Key for each attempt."""

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
            raise ValueError("SCNet configuration and credentials are required")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_models(self) -> "ListModelsOperation":
        """Bind the model-list Query to this client."""
        return ListModelsOperation(self)

    async def aclose(self) -> None:
        """Close only a transport this client created."""
        if self._owns_client:
            await self.http_client.aclose()


class ListModelsOperation:
    """Read up to fifty nonsecret model identifiers."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("SCNet client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated model-list Query contract."""
        return LIST_MODELS_DEFINITION

    async def invoke(self, call: Call, input: ListModelsInput) -> QueryAttempt[ListModelsOutput]:
        """Classify the provider response without returning model metadata."""
        try:
            credentials = await self.client.credentials.resolve(call)
            key = credentials.api_key.reveal()
            if not _valid_key(key):
                raise ValueError("invalid API key")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch("defect", FailureKind.AUTHENTICATION, "SCNet API Key is unavailable or invalid")
        try:
            request = self.client.http_client.build_request(
                "GET", _ENDPOINT, headers={"Authorization": "Bearer " + key, "Accept": "application/json"}
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "SCNet model request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "SCNet model request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if response.status_code == 429:
                return _retry(FailureKind.RATE_LIMIT, "SCNet rate limited the model request", delay)
            if response.status_code == 408 or response.status_code >= 500:
                return _retry(FailureKind.AVAILABILITY, "SCNet API is temporarily unavailable", delay)
            if response.status_code != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if response.status_code == 401
                    else (
                        FailureKind.AUTHORIZATION
                        if response.status_code == 403
                        else FailureKind.PROVIDER_REJECTION
                    )
                )
                return _branch("providerRejected", kind, f"SCNet rejected with HTTP {response.status_code}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse", FailureKind.RESPONSE_TOO_LARGE, "SCNet response exceeded limit"
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "SCNet response was interrupted")
            try:
                wire = json.loads(body, parse_constant=lambda _: (_ for _ in ()).throw(ValueError()))
                if wire["object"] != "list" or not isinstance(wire["data"], list):
                    raise ValueError("invalid list")
                ids: list[str] = []
                for model in wire["data"]:
                    value = model["id"]
                    if (
                        model["object"] != "model"
                        or not isinstance(value, str)
                        or not value
                        or len(value) > 256
                        or any(c in value for c in "\r\n\0")
                    ):
                        raise ValueError("invalid model")
                    if len(ids) < 50:
                        ids.append(value)
                output = ListModelsOutput(len(wire["data"]), tuple(ids))
            except (ValueError, KeyError, TypeError, UnicodeDecodeError):
                return _branch("invalidResponse", FailureKind.PROTOCOL, "SCNet returned invalid model data")
            return QueryAttempt.branch_result("found", output, Receipt())
        finally:
            await response.aclose()
