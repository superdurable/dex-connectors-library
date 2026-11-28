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
from ._generated.definitions import LIST_GATEWAY_MODELS_DEFINITION
from .models import ListGatewayModelsInput, ListGatewayModelsOutput


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListGatewayModelsOutput]:
    return QueryAttempt.branch_result(
        branch, ListGatewayModelsOutput(), Receipt(), Failure(kind, "opencsg", "listGatewayModels", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListGatewayModelsOutput]:
    return QueryAttempt.retry(Failure(kind, "opencsg", "listGatewayModels", message), delay)


class Client:
    """Own opencsg settings and resolve credentials on each call."""

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

    def list_gateway_models(self) -> "ListGatewayModelsOperation":
        """Bind the listGatewayModels Query."""
        return ListGatewayModelsOperation(self)

    async def aclose(self) -> None:
        """Close only an HTTP client this instance created."""
        if self._owns_client:
            await self.http_client.aclose()


class ListGatewayModelsOperation:
    """Read the opencsg provider and classify each outcome."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("provider client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity and retry policy."""
        return LIST_GATEWAY_MODELS_DEFINITION

    async def invoke(
        self, call: Call, input: ListGatewayModelsInput
    ) -> QueryAttempt[ListGatewayModelsOutput]:
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
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "opencsg credential is unavailable or invalid"
            )
        try:
            request = self.client.http_client.build_request(
                "GET",
                "https://ai.space.opencsg.com/v1/models",
                headers={"Accept": "application/json", "Authorization": f"Bearer {key}"},
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "opencsg request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "opencsg request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "opencsg rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "opencsg is temporarily unavailable", delay)
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch("providerRejected", kind, f"opencsg rejected the request with HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "opencsg response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "opencsg response was interrupted")
        finally:
            await response.aclose()
        return _decode_response(body, key)


def _decode_response(body: bytes, key: str) -> QueryAttempt[ListGatewayModelsOutput]:
    try:
        document: Any = json.loads(
            body, object_pairs_hook=lambda pairs: {name.lower(): value for name, value in pairs}
        )
    except (ValueError, UnicodeDecodeError):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "OpenCSG returned invalid model data")
    models = document.get("data") if isinstance(document, dict) else None
    if not isinstance(models, list):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "OpenCSG returned invalid model data")
    ids: list[str] = []
    for model in models:
        identifier = model.get("id") if isinstance(model, dict) else None
        if (
            not isinstance(identifier, str)
            or key in identifier
            or not identifier
            or any(char in "\r\n\0" or "\ud800" <= char <= "\udfff" for char in identifier)
            or len(identifier.encode()) > 256
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "OpenCSG returned invalid model ID")
        if len(ids) < 50:
            ids.append(identifier)
    return QueryAttempt.branch_result("found", ListGatewayModelsOutput(len(models), tuple(ids)), Receipt())
