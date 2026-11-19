# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""tingyun provider Query and Go-compatible outcome classification."""

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
from ._generated.definitions import LIST_APPLICATIONS_DEFINITION
from ._generated.models import ApplicationMetadata
from .models import ListApplicationsInput, ListApplicationsOutput


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListApplicationsOutput]:
    return QueryAttempt.branch_result(
        branch, ListApplicationsOutput(), Receipt(), Failure(kind, "tingyun", "listApplications", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListApplicationsOutput]:
    return QueryAttempt.retry(Failure(kind, "tingyun", "listApplications", message), delay)


class Client:
    """Own a bounded provider transport and resolve rotated credentials per call."""

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
            raise ValueError("tingyun configuration and credentials are invalid")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.endpoint = config.api_host.rstrip("/") + "/app-api/app/list"
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_applications(self) -> "ListApplicationsOperation":
        """Bind the read-only provider Query."""
        return ListApplicationsOperation(self)

    async def aclose(self) -> None:
        """Close the transport if this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class ListApplicationsOperation:
    """Read the provider endpoint without changing provider state."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("tingyun client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the stable generated Query definition."""
        return LIST_APPLICATIONS_DEFINITION

    async def invoke(self, call: Call, input: ListApplicationsInput) -> QueryAttempt[ListApplicationsOutput]:
        """Make one bounded request and classify its provider outcome."""
        if not isinstance(input, ListApplicationsInput):
            return _branch("defect", FailureKind.VALIDATION, "provider input is invalid")

        params = None
        try:
            credentials = await self.client.credentials.resolve(call)
            key = credentials.access_token.reveal()
            if (
                not key
                or len(key) > 2048
                or key != key.strip()
                or any(ord(c) <= 0x20 or ord(c) >= 0x7F for c in key)
            ):
                raise ValueError("invalid credential")
        except (ValueError, LookupError, OSError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "provider credentials are unavailable or invalid"
            )
        try:
            request = self.client.http_client.build_request(
                "GET",
                self.client.endpoint,
                params=params,
                headers={"Accept": "application/json", "Authorization": "token " + key},
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(FailureKind.AVAILABILITY, "provider request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if response.status_code == 429:
                return _retry(FailureKind.RATE_LIMIT, "provider rate limited the request", delay)
            if response.status_code == 408 or response.status_code >= 500:
                return _retry(FailureKind.AVAILABILITY, "provider temporarily unavailable", delay)
            if response.status_code != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if response.status_code == 401
                    else FailureKind.AUTHORIZATION
                    if response.status_code == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch(
                    "providerRejected",
                    kind,
                    f"provider rejected the request with HTTP {response.status_code}",
                )
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "provider response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "provider response was interrupted")
            try:
                data: Any = json.loads(
                    body, parse_constant=lambda _: (_ for _ in ()).throw(ValueError("invalid JSON number"))
                )
            except (ValueError, UnicodeDecodeError):
                return _branch("invalidResponse", FailureKind.PROTOCOL, "provider returned invalid JSON")
            try:
                return _decode(data, delay)
            except (ValueError, TypeError, KeyError, OverflowError):
                return _branch("invalidResponse", FailureKind.PROTOCOL, "provider returned invalid data")
        finally:
            await response.aclose()


def _decode(data: Any, delay: float) -> QueryAttempt[ListApplicationsOutput]:
    """Validate and reduce the provider response to safe result fields."""
    if not isinstance(data, list):
        raise ValueError("invalid provider response")

    if not isinstance(data, list):
        raise ValueError("invalid applications")
    apps = []
    for item in data:
        if (
            not isinstance(item, dict)
            or type(item.get("id")) is not int
            or item["id"] <= 0
            or not isinstance(item.get("name"), str)
            or not item["name"]
        ):
            raise ValueError("invalid application")
        apps.append(ApplicationMetadata(id=item["id"], name=item["name"]))
    return QueryAttempt.branch_result("listed", ListApplicationsOutput(applications=tuple(apps)), Receipt())
