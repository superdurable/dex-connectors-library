# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider Query implementation with Go-compatible outcomes."""

import ipaddress
import json
from datetime import UTC, datetime
from typing import Any
from urllib.parse import urlsplit

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
from ._generated.definitions import LIST_TEAMS_DEFINITION
from .models import ListTeamsInput, ListTeamsOutput, Team


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListTeamsOutput]:
    return QueryAttempt.branch_result(
        branch, ListTeamsOutput(), Receipt(), Failure(kind, "ones", "listTeams", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListTeamsOutput]:
    return QueryAttempt.retry(Failure(kind, "ones", "listTeams", message), delay)


def _validated_base_url(value: str, *, origin_only: bool = False) -> str:
    if not value or any(ord(char) <= 0x20 or ord(char) > 0x7E for char in value):
        raise ValueError("provider base URL must be printable ASCII")
    try:
        parsed = urlsplit(value)
        host = parsed.hostname
    except ValueError as error:
        raise ValueError("provider base URL is invalid") from error
    if not host or parsed.username or parsed.password or parsed.query or parsed.fragment:
        raise ValueError("provider base URL is invalid")
    if origin_only and parsed.path not in ("", "/"):
        raise ValueError("provider base URL must be an origin")
    if parsed.scheme != "https":
        try:
            is_loopback = host == "localhost" or ipaddress.ip_address(host).is_loopback
        except ValueError:
            is_loopback = host == "localhost"
        if parsed.scheme != "http" or not is_loopback:
            raise ValueError("provider base URL must use HTTPS")
    return value.rstrip("/")


class Client:
    """Own ones settings and resolve credentials on each call."""

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
        self.base_url = _validated_base_url(config.base_url, origin_only=True)
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_teams(self) -> "ListTeamsOperation":
        """Bind the listTeams Query."""
        return ListTeamsOperation(self)

    async def aclose(self) -> None:
        """Close only an HTTP client this instance created."""
        if self._owns_client:
            await self.http_client.aclose()


class ListTeamsOperation:
    """Read the ones provider and classify each outcome."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("provider client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity and retry policy."""
        return LIST_TEAMS_DEFINITION

    async def invoke(self, call: Call, input: ListTeamsInput) -> QueryAttempt[ListTeamsOutput]:
        """Make one read-only provider request."""
        try:
            credentials = await self.client.credentials.resolve(call)
            key = credentials.api_key.reveal()
            if (
                not key
                or not (len(key) <= 512)
                or any(ord(char) <= 0x20 or ord(char) >= 0x7F for char in key)
            ):
                raise ValueError("invalid key")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch("defect", FailureKind.AUTHENTICATION, "ones credential is unavailable or invalid")
        try:
            request = self.client.http_client.build_request(
                "GET",
                self.client.base_url + "/openapi/v2/account/teams",
                headers={"Accept": "application/json", "Authorization": f"Bearer {key}"},
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "ones request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "ones request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "ones rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "ones is temporarily unavailable", delay)
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch("providerRejected", kind, f"ones rejected the request with HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "ones response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "ones response was interrupted")
        finally:
            await response.aclose()
        return _decode_response(body)


def _decode_response(body: bytes) -> QueryAttempt[ListTeamsOutput]:
    try:
        document: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "ONES returned invalid team-list data")
    if not isinstance(document, dict):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "ONES returned invalid team-list data")
    if document.get("result") == "FAIL":
        code = document.get("errorCode")
        kind = (
            FailureKind.AUTHENTICATION
            if code == "Unauthorized"
            else FailureKind.AUTHORIZATION
            if code == "Forbidden"
            else FailureKind.PROVIDER_REJECTION
        )
        return _branch("providerRejected", kind, "ONES rejected the team-list request")
    data = document.get("data")
    teams = data.get("teams") if isinstance(data, dict) else None
    if document.get("result") != "SUCCESS" or not isinstance(teams, list) or len(teams) > 10000:
        return _branch("invalidResponse", FailureKind.PROTOCOL, "ONES returned invalid team-list data")
    result = []
    for team in teams:
        if not isinstance(team, dict):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "ONES returned an invalid team")
        identifier, name, status = team.get("id"), team.get("name"), team.get("status")
        if (
            not isinstance(identifier, str)
            or not identifier
            or len(identifier.encode()) > 256
            or identifier.strip() != identifier
            or not isinstance(name, str)
            or not name
            or len(name.encode()) > 512
            or type(status) is not int
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "ONES returned an invalid team")
        result.append(Team(identifier, name, status))
    return QueryAttempt.branch_result("listed", ListTeamsOutput(tuple(result)), Receipt())
