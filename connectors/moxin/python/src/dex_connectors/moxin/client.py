# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Moxin provider transport and Go-compatible Query decisions."""

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
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import LIST_MODELS_DEFINITION
from .models import ListModelsInput, Model, ModelPage


def _opaque(value: str, limit: int) -> bool:
    return (
        isinstance(value, str) and 0 < len(value) <= limit and all(0x20 < ord(char) < 0x7F for char in value)
    )


class Client:
    """Resolve credentials for each listModels request."""

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
            raise ValueError("Moxin configuration and credentials are required")
        validate_base_url(config.base_url)
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None

    def list_models(self) -> "ListModelsOperation":
        """Bind the listModels Query."""
        return ListModelsOperation(self)

    async def aclose(self) -> None:
        """Close the owned HTTP transport."""
        if self._owns_http_client:
            await self.http_client.aclose()


class ListModelsOperation:
    """Query Moxin and classify the provider outcome."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query definition."""
        return LIST_MODELS_DEFINITION

    def _branch(self, branch: str, kind: FailureKind) -> QueryAttempt[ModelPage]:
        return QueryAttempt.branch_result(
            branch, ModelPage(), Receipt(), Failure(kind, "moxin", "listModels", "Moxin request failed")
        )

    def _retry(self, kind: FailureKind, delay: float = 0) -> QueryAttempt[ModelPage]:
        return QueryAttempt.retry(Failure(kind, "moxin", "listModels", "Moxin request failed"), delay)

    def _request(
        self, input: ListModelsInput, credentials: Credentials
    ) -> httpx.Request | QueryAttempt[ModelPage]:
        token = credentials.platform_token.reveal()
        if not _opaque(token, 2048) or token.lower().startswith("bearer "):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        return self.client.http_client.build_request(
            "GET",
            self.client.config.base_url.rstrip("/") + "/v1/models",
            headers={
                "Accept": "application/json",
                "Content-Type": "application/json",
                "Authorization": "Bearer " + token,
            },
        )

    def _decode(
        self, wire: object, input: ListModelsInput, credentials: Credentials
    ) -> ModelPage | QueryAttempt[ModelPage] | None:
        if not isinstance(wire, dict):
            return None
        items = wire.get("data")
        if wire.get("object") != "list" or not isinstance(items, list) or len(items) > 1000:
            return None
        models = []
        for item in items:
            if (
                not isinstance(item, dict)
                or not isinstance(item.get("id"), str)
                or not item["id"]
                or item.get("object") != "model"
            ):
                return None
            models.append(Model(id=item["id"]))
        return ModelPage(models=tuple(models))

    async def invoke(self, call: Call, input: ListModelsInput) -> QueryAttempt[ModelPage]:
        """Read one bounded provider response without persisting secrets."""
        if not isinstance(input, ListModelsInput):
            return self._branch("defect", FailureKind.VALIDATION)
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError, AttributeError):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        request = self._request(input, credentials)
        if isinstance(request, QueryAttempt):
            return request
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return self._retry(FailureKind.AVAILABILITY)
        try:
            status = response.status_code
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return self._retry(FailureKind.RATE_LIMIT, delay)
            if status == 408 or status >= 500:
                return self._retry(FailureKind.AVAILABILITY, delay)
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return self._branch("providerRejected", kind)
            try:
                body = await read_bounded_body(response, self.client.config.max_response_bytes)
            except BodyTooLargeError:
                return self._branch("invalidResponse", FailureKind.RESPONSE_TOO_LARGE)
            except ProviderTransportError:
                return self._retry(FailureKind.AVAILABILITY)
        finally:
            await response.aclose()
        try:
            wire = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        result = self._decode(wire, input, credentials)
        if isinstance(result, QueryAttempt):
            return result
        if result is None:
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        return QueryAttempt.branch_result("listed", result, Receipt())
