# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider Query implementation with Go-compatible classifications."""

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
from ._generated.definitions import DEFECT, FOUND, INVALID_RESPONSE, LIST_MODELS_DEFINITION, PROVIDER_REJECTED
from .models import ListModelsInput, ListModelsOutput


class Client:
    """Own a bounded transport and resolve private credentials per call."""

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
            raise ValueError("Miyang configuration and credentials are invalid")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)

    def list_models(self) -> "ListModelsOperation":
        """Bind the read-only provider Query."""
        return ListModelsOperation(self)

    async def aclose(self) -> None:
        """Close the provider transport after Worker shutdown."""
        await self.http_client.aclose()


class ListModelsOperation:
    """Read provider data with typed Go-compatible outcomes."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Miyang client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the stable Query and branch contract."""
        return LIST_MODELS_DEFINITION

    async def invoke(self, call: Call, input: ListModelsInput) -> QueryAttempt[ListModelsOutput]:
        """Classify one bounded provider request and discard secret response fields."""

        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "Miyang credentials are unavailable or invalid"
            )
        if not isinstance(credentials, Credentials) or not _valid_secret(credentials.api_key.reveal()):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "Miyang credentials are unavailable or invalid"
            )
        endpoint = "https://miyang.cn/api/v1/models"
        headers = {"Accept": "application/json", "Authorization": "Bearer " + credentials.api_key.reveal()}
        try:
            request = self.client.http_client.build_request("GET", endpoint, headers=headers)
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(FailureKind.AVAILABILITY, "Miyang request failed")
        try:
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Miyang rate limited the request", retry_after)
            if status == 408 or status >= 500:
                return _retry(
                    FailureKind.AVAILABILITY, "Miyang service is temporarily unavailable", retry_after
                )

            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch(PROVIDER_REJECTED, kind, f"Miyang rejected the request with HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Miyang response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Miyang response was interrupted")
            try:
                data: Any = json.loads(
                    body, parse_constant=lambda _: (_ for _ in ()).throw(ValueError("invalid JSON number"))
                )
                result = _decode(data, input)
            except (ValueError, TypeError, KeyError, OverflowError):
                return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "Miyang returned invalid data")
            if isinstance(result, QueryAttempt):
                return result
            return QueryAttempt.branch_result(FOUND, result, Receipt())
        finally:
            await response.aclose()


def _valid_secret(value: object) -> bool:
    return isinstance(value, str) and 0 < len(value) <= 1024 and all(32 < ord(c) < 127 for c in value)


def _decode(data: Any, input: ListModelsInput) -> ListModelsOutput:
    if not isinstance(data, dict) or data.get("object") != "list" or not isinstance(data.get("data"), list):
        raise ValueError("invalid model list")
    rows = data["data"]
    model_ids: list[str] = []
    for row in rows:
        if not isinstance(row, dict) or row.get("object") != "model" or not isinstance(row.get("id"), str):
            raise ValueError("invalid model")
        identifier = row["id"]
        if not identifier or len(identifier.encode()) > 256 or any(char in identifier for char in "\r\n\x00"):
            raise ValueError("invalid model ID")
        if len(model_ids) < 50:
            model_ids.append(identifier)
    return ListModelsOutput(len(rows), tuple(model_ids))


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListModelsOutput]:
    return QueryAttempt.branch_result(
        branch, ListModelsOutput(), Receipt(), Failure(kind, "miyang", "listModels", message)
    )


def _retry(kind: FailureKind, message: str, retry_after: float = 0) -> QueryAttempt[ListModelsOutput]:
    return QueryAttempt.retry(Failure(kind, "miyang", "listModels", message), retry_after)
