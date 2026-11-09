# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Authenticated Campaign Monitor reads with Go-compatible decisions."""

import base64
import json
from dataclasses import dataclass
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
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import LIST_CLIENTS_DEFINITION
from .models import ClientMetadata, ClientsList, ListClientsInput


@dataclass(frozen=True, slots=True)
class _Outcome:
    """One classified provider failure or retry."""

    branch: str
    failure: Failure
    is_retry: bool = False
    retry_after_seconds: float = 0

    def with_empty(self) -> QueryAttempt[ClientsList]:
        """Apply this decision to an empty client list."""
        if self.is_retry:
            return QueryAttempt.retry(self.failure, self.retry_after_seconds)
        return QueryAttempt.branch_result(self.branch, ClientsList(), Receipt(), self.failure)


def _failure(branch: str, kind: FailureKind, message: str) -> _Outcome:
    return _Outcome(branch, Failure(kind, "campaignmonitor", "listClients", message))


class Client:
    """Resolve the account key per call and bound each provider response."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Campaign Monitor configuration and credential source are required")
        if type(config.max_response_bytes) is not int or config.max_response_bytes < 1:
            raise ValueError("Campaign Monitor response limit must be positive")
        if config.timeout_seconds <= 0:
            raise ValueError("Campaign Monitor timeout must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_clients(self) -> "ListClientsOperation":
        """Bind the client discovery Query."""
        return ListClientsOperation(self)

    async def aclose(self) -> None:
        """Close the transport when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def get(self, call: Call) -> Any:
        """Fetch client JSON or classify a provider failure."""
        try:
            credential = await self.credentials.resolve(call)
            api_key = credential.api_key.reveal()
            if not is_header_safe_credential(api_key):
                raise ValueError("invalid API key")
        except (ValueError, LookupError, OSError, AttributeError):
            return _failure(
                "defect", FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
            )
        basic = base64.b64encode((api_key + ":").encode("utf-8")).decode("ascii")
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + "/clients.json",
                headers={"Accept": "application/json", "Authorization": "Basic " + basic},
            )
        except (httpx.HTTPError, ValueError):
            return _Outcome(
                "",
                Failure(
                    FailureKind.TRANSPORT, "campaignmonitor", "listClients", "Campaign Monitor request failed"
                ),
                True,
            )
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return _Outcome(
                "",
                Failure(
                    FailureKind.TRANSPORT, "campaignmonitor", "listClients", "Campaign Monitor request failed"
                ),
                True,
            )
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _Outcome(
                    "",
                    Failure(
                        FailureKind.RATE_LIMIT,
                        "campaignmonitor",
                        "listClients",
                        "Campaign Monitor rate limited the request",
                    ),
                    True,
                    retry_after,
                )
            if status == 408 or status >= 500:
                return _Outcome(
                    "",
                    Failure(
                        FailureKind.AVAILABILITY,
                        "campaignmonitor",
                        "listClients",
                        "Campaign Monitor is temporarily unavailable",
                    ),
                    True,
                    retry_after,
                )
            if status != 200:
                return _failure(
                    "providerRejected",
                    FailureKind.PROVIDER_REJECTION,
                    f"Campaign Monitor rejected HTTP {status}",
                )
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _failure(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Campaign Monitor response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _Outcome(
                    "",
                    Failure(
                        FailureKind.TRANSPORT,
                        "campaignmonitor",
                        "listClients",
                        "Campaign Monitor response was interrupted",
                    ),
                    True,
                )
            try:
                return json.loads(body)
            except (ValueError, UnicodeDecodeError):
                return _failure(
                    "invalidResponse", FailureKind.PROTOCOL, "Campaign Monitor returned invalid JSON"
                )
        finally:
            await response.aclose()


class ListClientsOperation:
    """Read client IDs and names from Campaign Monitor."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return LIST_CLIENTS_DEFINITION

    async def invoke(self, call: Call, input: ListClientsInput) -> QueryAttempt[ClientsList]:
        """Read clients or select a Go-compatible optional branch."""
        if not isinstance(input, ListClientsInput):
            return _failure("defect", FailureKind.VALIDATION, "client list input is invalid").with_empty()
        response = await self.client.get(call)
        if isinstance(response, _Outcome):
            return response.with_empty()
        if not isinstance(response, list):
            return _failure(
                "invalidResponse", FailureKind.PROTOCOL, "Campaign Monitor returned invalid JSON"
            ).with_empty()
        clients = []
        for item in response:
            if (
                not isinstance(item, dict)
                or not isinstance(item.get("ClientID"), str)
                or not item["ClientID"]
                or not isinstance(item.get("Name"), str)
                or not item["Name"]
            ):
                return _failure(
                    "invalidResponse", FailureKind.PROTOCOL, "Campaign Monitor returned invalid JSON"
                ).with_empty()
            clients.append(ClientMetadata(item["ClientID"], item["Name"]))
        return QueryAttempt.branch_result("listed", ClientsList(tuple(clients)), Receipt())
