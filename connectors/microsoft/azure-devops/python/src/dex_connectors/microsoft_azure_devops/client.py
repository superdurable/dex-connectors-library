# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Authenticated Azure DevOps reads with Go-compatible decisions."""

import base64
import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any, TypeVar
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
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import GET_WORK_ITEM_DEFINITION, LIST_PROJECTS_DEFINITION
from .models import GetWorkItemInput, ListProjectsInput, ProjectMetadata, Projects, WorkItemMetadata

Output = TypeVar("Output")
USER_AGENT = "Dex Connector (https://superdurable.io)"


@dataclass(frozen=True, slots=True)
class _Outcome:
    """One classified provider failure or retry."""

    branch: str
    failure: Failure
    is_retry: bool = False
    retry_after_seconds: float = 0

    def with_empty(self, empty: Output) -> QueryAttempt[Output]:
        """Apply a provider decision to the operation's empty output."""
        if self.is_retry:
            return QueryAttempt.retry(self.failure, self.retry_after_seconds)
        return QueryAttempt.branch_result(self.branch, empty, Receipt(), self.failure)


def _failure(operation: str, branch: str, kind: FailureKind, message: str) -> _Outcome:
    return _Outcome(branch, Failure(kind, "azure-devops", operation, message))


def _invalid(operation: str, empty: Output) -> QueryAttempt[Output]:
    return _failure(
        operation, "invalidResponse", FailureKind.PROTOCOL, "Azure DevOps returned invalid JSON"
    ).with_empty(empty)


class Client:
    """Resolve the Azure DevOps PAT per call and bound provider responses."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Azure DevOps configuration and credential source are required")
        if type(config.max_response_bytes) is not int or config.max_response_bytes < 1:
            raise ValueError("Azure DevOps response limit must be positive")
        if config.timeout_seconds <= 0:
            raise ValueError("Azure DevOps timeout must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_projects(self) -> "ListProjectsOperation":
        """Bind the project page Query."""
        return ListProjectsOperation(self)

    def get_work_item(self) -> "GetWorkItemOperation":
        """Bind the work item Query."""
        return GetWorkItemOperation(self)

    async def aclose(self) -> None:
        """Close the transport when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def get(self, call: Call, operation: str, path: str, has_not_found: bool) -> Any:
        """Fetch JSON or classify a provider failure without retaining its body."""
        try:
            credential = await self.credentials.resolve(call)
            token = credential.personal_access_token.reveal()
            if not is_header_safe_credential(token):
                raise ValueError("invalid Azure DevOps PAT")
        except (ValueError, LookupError, OSError, AttributeError):
            return _failure(
                operation, "defect", FailureKind.AUTHENTICATION,
                "connection credentials are unavailable or invalid",
            )
        authorization = base64.b64encode((":" + token).encode()).decode("ascii")
        try:
            request = self.http_client.build_request(
                "GET", self.base_url + path,
                headers={
                    "Accept": "application/json",
                    "Authorization": "Basic " + authorization,
                    "User-Agent": USER_AGENT,
                },
            )
        except (httpx.HTTPError, ValueError):
            return _Outcome("", Failure(FailureKind.TRANSPORT, "azure-devops", operation,
                                        "Azure DevOps request failed"), True)
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return _Outcome("", Failure(FailureKind.TRANSPORT, "azure-devops", operation,
                                        "Azure DevOps request failed"), True)
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _Outcome("", Failure(FailureKind.RATE_LIMIT, "azure-devops", operation,
                                            "Azure DevOps rate limited the request"), True, retry_after)
            if status == 408 or status >= 500:
                return _Outcome("", Failure(FailureKind.AVAILABILITY, "azure-devops", operation,
                                            "Azure DevOps is temporarily unavailable"), True, retry_after)
            if status == 404 and has_not_found:
                return _failure(operation, "notFound", FailureKind.NOT_FOUND,
                                "Azure DevOps did not find the resource")
            if status != 200:
                return _failure(operation, "providerRejected", FailureKind.PROVIDER_REJECTION,
                                f"Azure DevOps rejected HTTP {status}")
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _failure(operation, "invalidResponse", FailureKind.RESPONSE_TOO_LARGE,
                                "Azure DevOps response exceeded the configured limit")
            except ProviderTransportError:
                return _Outcome("", Failure(FailureKind.TRANSPORT, "azure-devops", operation,
                                            "Azure DevOps response was interrupted"), True)
            try:
                return json.loads(body)
            except (ValueError, UnicodeDecodeError):
                return _failure(operation, "invalidResponse", FailureKind.PROTOCOL,
                                "Azure DevOps returned invalid JSON")
        finally:
            await response.aclose()


class ListProjectsOperation:
    """List one bounded page of accessible Azure DevOps projects."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query contract."""
        return LIST_PROJECTS_DEFINITION

    async def invoke(self, call: Call, input: ListProjectsInput) -> QueryAttempt[Projects]:
        """Read a project page or select a Go-compatible optional branch."""
        empty = Projects()
        if (
            not isinstance(input, ListProjectsInput)
            or type(input.top) is not int or not 1 <= input.top <= 100
            or type(input.skip) is not int or input.skip < 0
        ):
            return _failure("listProjects", "defect", FailureKind.VALIDATION,
                            "project page size or offset is invalid").with_empty(empty)
        query = urlencode({"$top": input.top, "$skip": input.skip, "api-version": "7.1"})
        response = await self.client.get(call, "listProjects", f"/_apis/projects?{query}", False)
        if isinstance(response, _Outcome):
            return response.with_empty(empty)
        values = response.get("value") if isinstance(response, dict) else None
        if not isinstance(values, list):
            return _invalid("listProjects", empty)
        projects = []
        for value in values:
            if not isinstance(value, dict) or not isinstance(value.get("id"), str) or not value["id"]:
                return _invalid("listProjects", empty)
            if not isinstance(value.get("name"), str) or not value["name"]:
                return _invalid("listProjects", empty)
            projects.append(ProjectMetadata(value["id"], value["name"], value.get("state") or ""))
        return QueryAttempt.branch_result(
            "listed", Projects(tuple(projects), input.top, input.skip), Receipt()
        )


class GetWorkItemOperation:
    """Read one Azure DevOps work item's basic identity."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query contract."""
        return GET_WORK_ITEM_DEFINITION

    async def invoke(self, call: Call, input: GetWorkItemInput) -> QueryAttempt[WorkItemMetadata]:
        """Read a work item after validating its positive decimal ID."""
        empty = WorkItemMetadata()
        if not isinstance(input, GetWorkItemInput) or not _valid_work_item_id(input.work_item_id):
            return _failure("getWorkItem", "defect", FailureKind.VALIDATION,
                            "work item ID must be a positive decimal identifier").with_empty(empty)
        response = await self.client.get(
            call, "getWorkItem", f"/_apis/wit/workitems/{input.work_item_id}?api-version=7.1", True
        )
        if isinstance(response, _Outcome):
            return response.with_empty(empty)
        fields = response.get("fields") if isinstance(response, dict) else None
        if not isinstance(fields, dict) or type(response.get("id")) is not int:
            return _invalid("getWorkItem", empty)
        work_item_id = response["id"]
        title = fields.get("System.Title")
        if not 0 < work_item_id <= 2147483647 or not isinstance(title, str) or not title:
            return _invalid("getWorkItem", empty)
        work_item = WorkItemMetadata(
            work_item_id, title, fields.get("System.State") or "",
            fields.get("System.WorkItemType") or "",
        )
        return QueryAttempt.branch_result("found", work_item, Receipt())


def _valid_work_item_id(work_item_id: str) -> bool:
    return (
        isinstance(work_item_id, str)
        and re.fullmatch(r"[0-9]+", work_item_id) is not None
        and 0 < int(work_item_id) <= 2147483647
    )
