# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Rainbond bounded team directory Query."""

import json
from datetime import UTC, datetime
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
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import LIST_TEAMS_DEFINITION
from .models import ListTeamsInput, ListTeamsOutput, Team


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListTeamsOutput]:
    return QueryAttempt.branch_result(
        branch, ListTeamsOutput(), Receipt(), Failure(kind, "rainbond", "listTeams", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListTeamsOutput]:
    return QueryAttempt.retry(Failure(kind, "rainbond", "listTeams", message), delay)


def _valid_key(value: str) -> bool:
    return 0 < len(value) <= 512 and value.strip() == value and all(0x20 < ord(c) < 0x7F for c in value)


class Client:
    """Own Rainbond API origin and resolve the console access token per attempt."""

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
            raise ValueError("Rainbond configuration and credential source are required")
        base_url = validate_base_url(config.base_url)
        parsed = urlsplit(base_url)
        if parsed.path not in ("", "/") or parsed.query or parsed.fragment:
            raise ValueError("Rainbond base URL must be an HTTPS origin")
        self.base_url = base_url.rstrip("/")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_teams(self) -> "ListTeamsOperation":
        """Bind the team-list Query to this client."""
        return ListTeamsOperation(self)

    async def aclose(self) -> None:
        """Close only a transport this client created."""
        if self._owns_client:
            await self.http_client.aclose()


class ListTeamsOperation:
    """Read one page of team names and IDs without personal data."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Rainbond client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated team-list Query contract."""
        return LIST_TEAMS_DEFINITION

    async def invoke(self, call: Call, input: ListTeamsInput) -> QueryAttempt[ListTeamsOutput]:
        """Read one page, preserving Go's provider and retry decisions."""
        try:
            credentials = await self.client.credentials.resolve(call)
            key = credentials.api_key.reveal()
            if not _valid_key(key):
                raise ValueError("invalid console token")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "console access token is unavailable or invalid"
            )
        page, size = input.page or 1, input.page_size or 20
        if type(page) is not int or type(size) is not int or not 1 <= page <= 1000000 or not 1 <= size <= 100:
            return _branch(
                "defect", FailureKind.VALIDATION, "page or pageSize is outside the supported range"
            )
        try:
            request = self.client.http_client.build_request(
                "GET",
                self.client.base_url + "/openapi/v1/teams",
                params={"page": page, "page_size": size},
                headers={"Accept": "application/json", "Authorization": key},
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "team-list request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Rainbond request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if response.status_code == 429:
                return _retry(FailureKind.RATE_LIMIT, "Rainbond rate limited the request", delay)
            if response.status_code == 408 or response.status_code >= 500:
                return _retry(FailureKind.AVAILABILITY, "Rainbond is temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse", FailureKind.RESPONSE_TOO_LARGE, "Rainbond response exceeded limit"
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Rainbond response was interrupted")
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
                return _branch(
                    "providerRejected", kind, f"Rainbond rejected with HTTP {response.status_code}"
                )
            try:
                wire = json.loads(body, parse_constant=lambda _: (_ for _ in ()).throw(ValueError()))
                total, tenants = wire["total"], wire["tenants"]
                if (
                    type(total) is not int
                    or total < 0
                    or not isinstance(tenants, list)
                    or len(tenants) > size
                ):
                    raise ValueError("invalid team list")
                teams = []
                for tenant in tenants:
                    team_id, name = tenant["tenant_id"], tenant["tenant_name"]
                    active = tenant.get("is_active", False)
                    if (
                        not isinstance(team_id, str)
                        or not team_id
                        or len(team_id) > 256
                        or team_id.strip() != team_id
                        or not isinstance(name, str)
                        or not name
                        or len(name) > 512
                        or type(active) is not bool
                    ):
                        raise ValueError("invalid team")
                    teams.append(Team(team_id, name, active))
                output = ListTeamsOutput(tuple(teams), total)
            except (ValueError, KeyError, TypeError, UnicodeDecodeError):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "Rainbond returned invalid team-list data"
                )
            return QueryAttempt.branch_result("listed", output, Receipt())
        finally:
            await response.aclose()
