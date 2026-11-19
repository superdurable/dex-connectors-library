# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Zadig project catalog Query with Go-compatible outcomes."""

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
from ._generated.definitions import LIST_PROJECTS_DEFINITION
from .models import ListProjectsInput, ListProjectsOutput, Project


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListProjectsOutput]:
    return QueryAttempt.branch_result(
        branch, ListProjectsOutput(), Receipt(), Failure(kind, "zadig", "listProjects", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListProjectsOutput]:
    return QueryAttempt.retry(Failure(kind, "zadig", "listProjects", message), delay)


def _validated_origin(value: str) -> str:
    if not value or any(ord(character) <= 0x20 or ord(character) > 0x7E for character in value):
        raise ValueError("Zadig base URL must be printable ASCII")
    try:
        parsed = urlsplit(value)
        host = parsed.hostname
    except ValueError as error:
        raise ValueError("Zadig base URL is invalid") from error
    if not host or parsed.username or parsed.password or parsed.query or parsed.fragment or parsed.path:
        raise ValueError("Zadig base URL must be an origin")
    if parsed.scheme != "https":
        try:
            is_loopback = host == "localhost" or ipaddress.ip_address(host).is_loopback
        except ValueError:
            is_loopback = host == "localhost"
        if parsed.scheme != "http" or not is_loopback:
            raise ValueError("Zadig base URL must use HTTPS")
    return value.rstrip("/")


class Client:
    """Own Zadig configuration and resolve credentials on each project Query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("configuration and credentials are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("Zadig timeout and maxResponseBytes must be positive")
        self.base_url = _validated_origin(config.base_url)
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_projects(self) -> "ListProjectsOperation":
        """Bind the listProjects Query."""
        return ListProjectsOperation(self)

    async def aclose(self) -> None:
        """Close only an HTTP client this instance created."""
        if self._owns_client:
            await self.http_client.aclose()


class ListProjectsOperation:
    """Read one page of Zadig project metadata."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("provider client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity and retry policy."""
        return LIST_PROJECTS_DEFINITION

    async def invoke(self, call: Call, input: ListProjectsInput) -> QueryAttempt[ListProjectsOutput]:
        """Make one read-only provider request."""
        try:
            credentials = await self.client.credentials.resolve(call)
            api_key = credentials.api_key.reveal()
            if (
                not api_key
                or len(api_key) > 512
                or api_key.strip() != api_key
                or any(ord(character) <= 0x20 or ord(character) >= 0x7F for character in api_key)
            ):
                raise ValueError("invalid key")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "connection API token is unavailable or invalid"
            )
        page_num = input.page_num or 1
        page_size = input.page_size or 20
        if not (1 <= page_num <= 1000000 and 1 <= page_size <= 100):
            return _branch(
                "defect", FailureKind.VALIDATION, "page number or size is outside the supported range"
            )
        try:
            request = self.client.http_client.build_request(
                "GET",
                self.client.base_url + "/openapi/projects/project",
                params={"pageNum": str(page_num), "pageSize": str(page_size)},
                headers={"Accept": "application/json", "Authorization": f"Bearer {api_key}"},
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "project-list request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Zadig request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Zadig rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Zadig is temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Zadig response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Zadig response was interrupted")
        finally:
            await response.aclose()
        if status != 200:
            kind = (
                FailureKind.AUTHENTICATION
                if status == 401
                else FailureKind.AUTHORIZATION
                if status == 403
                else FailureKind.PROVIDER_REJECTION
            )
            return _branch("providerRejected", kind, f"Zadig rejected the request with HTTP {status}")
        return _decode_projects(body, page_size)


def _decode_projects(body: bytes, page_size: int) -> QueryAttempt[ListProjectsOutput]:
    try:
        document: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Zadig returned invalid project-list data")
    if not isinstance(document, dict):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Zadig returned invalid project-list data")
    total, entries = document.get("total"), document.get("projects")
    if type(total) is not int or total < 0 or not isinstance(entries, list) or len(entries) > page_size:
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Zadig returned invalid project-list data")
    projects = []
    for entry in entries:
        if not isinstance(entry, dict):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Zadig returned an invalid project")
        key, name = entry.get("project_key"), entry.get("project_name")
        deploy_type = entry.get("deploy_type", "")
        if (
            not isinstance(key, str)
            or not key
            or len(key.encode()) > 256
            or key.strip() != key
            or not isinstance(name, str)
            or not name
            or len(name.encode()) > 512
            or not isinstance(deploy_type, str)
            or len(deploy_type.encode()) > 64
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Zadig returned an invalid project")
        projects.append(Project(key, name, deploy_type))
    return QueryAttempt.branch_result("listed", ListProjectsOutput(tuple(projects), total), Receipt())
