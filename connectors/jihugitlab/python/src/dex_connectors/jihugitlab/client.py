# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""JiHu GitLab member-project transport and Go-compatible decisions."""

import json
from datetime import UTC, datetime
from typing import Any
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
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import LIST_MEMBER_PROJECTS_DEFINITION
from .models import ListMemberProjectsInput, ListMemberProjectsOutput, MemberProject


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListMemberProjectsOutput]:
    return QueryAttempt.branch_result(
        branch,
        ListMemberProjectsOutput(),
        Receipt(),
        Failure(kind, "jihugitlab", "listMemberProjects", message),
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListMemberProjectsOutput]:
    return QueryAttempt.retry(Failure(kind, "jihugitlab", "listMemberProjects", message), delay)


class Client:
    """Own a redirect-safe JiHu GitLab transport and resolve credentials per Query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("JiHu GitLab configuration and credential source are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("JiHu GitLab timeout or response limit is invalid")
        self.base_url = validate_base_url(config.base_url)
        if httpx.URL(self.base_url).path not in {"", "/"}:
            raise ValueError("JiHu GitLab base URL must contain only the HTTPS origin")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_member_projects(self) -> "ListMemberProjectsOperation":
        """Bind the member-project Query to this client."""
        return ListMemberProjectsOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP transport when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class ListMemberProjectsOperation:
    """Read one page of member projects without exposing token material."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("JiHu GitLab client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated member-project Query identity."""
        return LIST_MEMBER_PROJECTS_DEFINITION

    async def invoke(
        self, call: Call, input: ListMemberProjectsInput
    ) -> QueryAttempt[ListMemberProjectsOutput]:
        """List projects and classify retry, rejection, and malformed data."""
        try:
            credentials = await self.client.credentials.resolve(call)
            api_key = credentials.api_key.reveal()
            if not api_key or len(api_key) > 512 or any(not 0x20 < ord(c) < 0x7F for c in api_key):
                raise ValueError("invalid token")
        except (ValueError, LookupError, OSError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "JiHu GitLab token is unavailable or invalid"
            )
        page = input.page or 1
        per_page = input.per_page or 20
        if not 1 <= page <= 50000 or not 1 <= per_page <= 100:
            return _branch("defect", FailureKind.VALIDATION, "page or perPage is outside the supported range")
        try:
            query = urlencode({"membership": "true", "page": page, "per_page": per_page})
            request = self.client.http_client.build_request(
                "GET",
                self.client.base_url + "/api/v4/projects?" + query,
                headers={"Accept": "application/json", "PRIVATE-TOKEN": api_key},
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "project-list request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "JiHu GitLab request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "JiHu GitLab rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "JiHu GitLab temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "project response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "project response was interrupted")
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch(
                    "providerRejected", kind, f"JiHu GitLab rejected the request with HTTP {status}"
                )
            return _decode_projects(body, page, per_page, response.headers.get("X-Next-Page", ""))
        finally:
            await response.aclose()


def _decode_projects(
    body: bytes, page: int, per_page: int, next_page_header: str
) -> QueryAttempt[ListMemberProjectsOutput]:
    try:
        wire: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "JiHu GitLab returned invalid project data")
    if not isinstance(wire, list) or len(wire) > per_page:
        return _branch("invalidResponse", FailureKind.PROTOCOL, "JiHu GitLab returned invalid project data")
    projects: list[MemberProject] = []
    for item in wire:
        if (
            not isinstance(item, dict)
            or type(item.get("id")) is not int
            or item["id"] <= 0
            or not isinstance(item.get("name"), str)
            or not isinstance(item.get("path_with_namespace"), str)
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "JiHu GitLab returned an invalid project")
        name, path = item["name"], item["path_with_namespace"]
        if not name or len(name) > 512 or not path or len(path) > 1024 or path.strip() != path:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "JiHu GitLab returned an invalid project")
        projects.append(MemberProject(id=item["id"], name=name, path=path))
    next_page = 0
    if next_page_header:
        try:
            next_page = int(next_page_header)
        except ValueError:
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "JiHu GitLab returned invalid pagination data"
            )
        if not page < next_page <= 50000 or str(next_page) != next_page_header:
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "JiHu GitLab returned invalid pagination data"
            )
    return QueryAttempt.branch_result(
        "listed", ListMemberProjectsOutput(projects=tuple(projects), next_page=next_page), Receipt()
    )
