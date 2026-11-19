# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Worktile token and project transport with Go-compatible query decisions."""

import json
from datetime import UTC, datetime
from urllib.parse import urlencode

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
from ._generated.definitions import LIST_PROJECTS_DEFINITION
from .models import ListProjectsInput, ListProjectsOutput, Project

TOKEN_ENDPOINT = "https://dev.worktile.com/open-api/tenant-access-token"
PROJECTS_ENDPOINT = "https://dev.worktile.com/open-api/mission/get-projects-by-page"


def _credential(value: object) -> bool:
    return isinstance(value, str) and 0 < len(value) <= 1024 and all(0x20 < ord(c) < 0x7F for c in value)


class Client:
    """Resolve Worktile application credentials for each project query."""

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
            raise ValueError("Worktile configuration and credentials are required")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None

    def list_projects(self) -> "ListProjectsOperation":
        """Bind the project-list query."""
        return ListProjectsOperation(self)

    async def aclose(self) -> None:
        """Close the owned transport."""
        if self._owns_http_client:
            await self.http_client.aclose()


class ListProjectsOperation:
    """Acquire a tenant token and query one Worktile project page."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated query definition."""
        return LIST_PROJECTS_DEFINITION

    def _failure(self, kind: FailureKind) -> Failure:
        return Failure(kind, "worktile", "listProjects", "Worktile request failed")

    def _branch(self, branch: str, kind: FailureKind) -> QueryAttempt[ListProjectsOutput]:
        return QueryAttempt.branch_result(branch, ListProjectsOutput(), Receipt(), self._failure(kind))

    def _retry(self, kind: FailureKind, delay: float = 0) -> QueryAttempt[ListProjectsOutput]:
        return QueryAttempt.retry(self._failure(kind), delay)

    async def _response(self, request: httpx.Request) -> bytes | QueryAttempt[ListProjectsOutput]:
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
                return await read_bounded_body(response, self.client.config.max_response_bytes)
            except BodyTooLargeError:
                return self._branch("invalidResponse", FailureKind.RESPONSE_TOO_LARGE)
            except ProviderTransportError:
                return self._retry(FailureKind.AVAILABILITY)
        finally:
            await response.aclose()

    async def invoke(self, call: Call, input: ListProjectsInput) -> QueryAttempt[ListProjectsOutput]:
        """Read one page without persisting client credentials or tenant tokens."""
        if (
            not isinstance(input, ListProjectsInput)
            or input.page_index < 0
            or not 1 <= input.page_size <= 100
        ):
            return self._branch("defect", FailureKind.VALIDATION)
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError, AttributeError):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        client_id = credentials.client_id.reveal()
        client_secret = credentials.client_secret.reveal()
        if not _credential(client_id) or not _credential(client_secret):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        token_request = self.client.http_client.build_request(
            "POST",
            TOKEN_ENDPOINT,
            content=json.dumps(
                {"client_id": client_id, "client_secret": client_secret}, separators=(",", ":")
            ),
            headers={"Accept": "application/json", "Content-Type": "application/json"},
        )
        token_body = await self._response(token_request)
        if isinstance(token_body, QueryAttempt):
            return token_body
        try:
            token_wire = json.loads(token_body)
        except (ValueError, UnicodeDecodeError):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if (
            not isinstance(token_wire, dict)
            or not _credential(token_wire.get("tenant_access_token"))
            or type(token_wire.get("expires_in")) is not int
            or token_wire["expires_in"] <= 0
        ):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        query = urlencode(
            sorted(
                {
                    "access_token": token_wire["tenant_access_token"],
                    "page_index": input.page_index,
                    "page_size": input.page_size,
                }.items()
            )
        )
        request = self.client.http_client.build_request(
            "GET",
            PROJECTS_ENDPOINT + "?" + query,
            headers={"Accept": "application/json"},
        )
        body = await self._response(request)
        if isinstance(body, QueryAttempt):
            return body
        try:
            wire = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if (
            not isinstance(wire, dict)
            or type(wire.get("page_index")) is not int
            or wire["page_index"] != input.page_index
            or type(wire.get("page_size")) is not int
            or not 1 <= wire["page_size"] <= 100
            or type(wire.get("has_more")) is not bool
            or not isinstance(wire.get("items"), list)
        ):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        projects = []
        for item in wire["items"]:
            if (
                not isinstance(item, dict)
                or not isinstance(item.get("_id"), str)
                or not item["_id"]
                or not isinstance(item.get("name"), str)
                or not item["name"]
                or type(item.get("visibility")) is not int
                or item["visibility"] not in (1, 2)
                or not isinstance(item.get("identifier", ""), str)
            ):
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            projects.append(
                Project(
                    id=item["_id"],
                    name=item["name"],
                    identifier=item.get("identifier", ""),
                    visibility=item["visibility"],
                )
            )
        return QueryAttempt.branch_result(
            "found",
            ListProjectsOutput(
                page_index=wire["page_index"],
                page_size=wire["page_size"],
                has_more=wire["has_more"],
                projects=tuple(projects),
            ),
            Receipt(),
        )
