# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider transport and Go-compatible Query decisions."""

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
from ._generated.definitions import LIST_SPACES_DEFINITION
from ._generated.models import Space
from .models import ListSpacesInput, SpacePage

_PROVIDER = "vika"
_OPERATION = "listSpaces"


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[SpacePage]:
    return QueryAttempt.branch_result(
        branch, SpacePage(), Receipt(), Failure(kind, _PROVIDER, _OPERATION, message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[SpacePage]:
    return QueryAttempt.retry(Failure(kind, _PROVIDER, _OPERATION, message), delay)


class Client:
    """Own the provider HTTP transport and resolve rotating credentials per Query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Vika configuration and credentials are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("Vika configuration is invalid")
        from dex_connectors.sdk.providerhttp import validate_base_url

        base_url = validate_base_url(config.base_url or "https://vika.cn")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self.base_url = base_url

    def list_spaces(self) -> "ListSpacesOperation":
        """Bind the listSpaces Query."""
        return ListSpacesOperation(self)

    async def aclose(self) -> None:
        """Close this client's own transport."""
        if self._owns_client:
            await self.http_client.aclose()


class ListSpacesOperation:
    """Run the listSpaces provider Query."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Vika client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity."""
        return LIST_SPACES_DEFINITION

    async def invoke(self, call: Call, input: ListSpacesInput) -> QueryAttempt[SpacePage]:
        """Send one request and classify its provider outcome."""
        prepared = await _prepare(self.client, call, input)
        if isinstance(prepared, QueryAttempt):
            return prepared
        try:
            response, _ = await send_traced(self.client.http_client, prepared)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Vika request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Vika rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Vika is temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Vika response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Vika response was interrupted")
            if status != 200:
                return _http_rejection(status)
        finally:
            await response.aclose()
        return _decode(body, input, delay)


def _valid_token(value: str) -> bool:
    return (
        bool(value)
        and len(value) <= 512
        and value.strip() == value
        and not value.lower().startswith("bearer ")
        and all(33 <= ord(c) <= 126 for c in value)
    )


async def _prepare(
    client: Client, call: Call, input: ListSpacesInput
) -> httpx.Request | QueryAttempt[SpacePage]:
    try:
        credentials = await client.credentials.resolve(call)
        token = credentials.api_token.reveal()
        if not _valid_token(token):
            raise ValueError("invalid token")
    except (ValueError, LookupError, OSError):
        return _branch(
            "defect", FailureKind.AUTHENTICATION, "Vika connection token is unavailable or invalid"
        )
    try:
        return client.http_client.build_request(
            "GET",
            client.base_url + "/fusion/v1/spaces",
            headers={"Accept": "application/json", "Authorization": "Bearer " + token},
        )
    except (httpx.HTTPError, ValueError):
        return _branch("defect", FailureKind.VALIDATION, "workspace request is invalid")


def _http_rejection(status: int) -> QueryAttempt[SpacePage]:
    kind = FailureKind.PROVIDER_REJECTION
    if status == 401:
        kind = FailureKind.AUTHENTICATION
    elif status == 403:
        kind = FailureKind.AUTHORIZATION
    elif status in (400, 404):
        kind = FailureKind.VALIDATION
    return _branch("providerRejected", kind, f"Vika rejected the query with HTTP {status}")


def _decode(body: bytes, input: ListSpacesInput, delay: float) -> QueryAttempt[SpacePage]:
    try:
        document: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        document = None
    if (
        not isinstance(document, dict)
        or type(document.get("success")) is not bool
        or type(document.get("code")) is not int
    ):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Vika returned an invalid workspace response")
    code = document["code"]
    if document["success"] is False:
        if code == 200:
            return QueryAttempt.branch_result("listed", SpacePage(()), Receipt())
        if code == 429:
            return _retry(FailureKind.RATE_LIMIT, "Vika rate limited the request", delay)
        if code >= 500:
            return _retry(FailureKind.AVAILABILITY, "Vika is temporarily unavailable", delay)
        return _branch(
            "providerRejected", FailureKind.PROVIDER_REJECTION, f"Vika rejected the query with code {code}"
        )
    data = document.get("data")
    if (
        code != 200
        or not isinstance(data, dict)
        or not isinstance(data.get("spaces"), list)
        or len(data["spaces"]) > 1000
    ):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Vika returned an invalid workspace response")
    spaces: list[Space] = []
    for item in data["spaces"]:
        if (
            not isinstance(item, dict)
            or not isinstance(item.get("id"), str)
            or not item["id"].startswith("spc")
            or not isinstance(item.get("name"), str)
            or not item["name"]
            or (item.get("isAdmin") is not None and type(item["isAdmin"]) is not bool)
        ):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Vika returned an invalid workspace response"
            )
        spaces.append(Space(item["id"], item["name"], item.get("isAdmin") is True))
    return QueryAttempt.branch_result("listed", SpacePage(tuple(spaces)), Receipt())
