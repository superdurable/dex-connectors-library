# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""ZhuanqiCloud provider transport and Go-compatible Query decisions."""

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


def _opaque(value: str, limit: int = 1024) -> bool:
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
            raise ValueError("ZhuanqiCloud configuration and credentials are required")
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
    """Query ZhuanqiCloud and classify the provider outcome."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query definition."""
        return LIST_MODELS_DEFINITION

    def _branch(self, branch: str, kind: FailureKind) -> QueryAttempt[ListModelsOutput]:
        return QueryAttempt.branch_result(
            branch,
            ListModelsOutput(),
            Receipt(),
            Failure(kind, "zhuanqicloud", "listModels", "ZhuanqiCloud request failed"),
        )

    def _retry(self, kind: FailureKind, delay: float = 0) -> QueryAttempt[ListModelsOutput]:
        return QueryAttempt.retry(
            Failure(kind, "zhuanqicloud", "listModels", "ZhuanqiCloud request failed"), delay
        )

    def _request(
        self, input: ListModelsInput, credentials: Credentials
    ) -> httpx.Request | QueryAttempt[ListModelsOutput]:
        token = credentials.api_key.reveal()
        if not _opaque(token):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        return self.client.http_client.build_request(
            "GET",
            "https://zhuanqicloud.com/v1/models",
            headers={"Accept": "application/json", "Authorization": "Bearer " + token},
        )

    def _decode(
        self, wire: object, input: ListModelsInput
    ) -> ListModelsOutput | QueryAttempt[ListModelsOutput] | None:
        if (
            not isinstance(wire, dict)
            or wire.get("object") != "list"
            or not isinstance(wire.get("data"), list)
        ):
            return None
        items = wire["data"]
        model_ids: list[str] = []
        for item in items:
            if (
                not isinstance(item, dict)
                or item.get("object") != "model"
                or not isinstance(item.get("id"), str)
                or not 0 < len(item["id"]) <= 256
                or any(ch in item["id"] for ch in "\r\n\0")
            ):
                return None
            if len(model_ids) < 50:
                model_ids.append(item["id"])
        return ListModelsOutput(total=len(items), model_ids=tuple(model_ids))

    async def invoke(self, call: Call, input: ListModelsInput) -> QueryAttempt[ListModelsOutput]:
        """Read one bounded provider response without persisting credentials."""
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
        result = self._decode(wire, input)
        if isinstance(result, QueryAttempt):
            return result
        if result is None:
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        return QueryAttempt.branch_result("found", result, Receipt())
