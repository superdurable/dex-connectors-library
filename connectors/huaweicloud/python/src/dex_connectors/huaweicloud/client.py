# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read accessible Huawei Cloud IAM projects through a domain token."""

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
from ._generated.definitions import LIST_ACCESSIBLE_PROJECTS_DEFINITION
from ._generated.models import AccessibleProject
from .models import ListAccessibleProjectsInput, ListAccessibleProjectsOutput

_ENDPOINT = "https://iam.myhuaweicloud.com"


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "huaweicloud", "listAccessibleProjects", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListAccessibleProjectsOutput]:
    return QueryAttempt.branch_result(
        branch, ListAccessibleProjectsOutput(), Receipt(), _failure(kind, message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListAccessibleProjectsOutput]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_credential(value: str) -> bool:
    return (
        bool(value)
        and len(value) <= 512
        and value.strip() == value
        and all(ord(c) >= 32 and ord(c) != 127 for c in value)
    )


def _valid_token(value: str) -> bool:
    return bool(value) and len(value) < 32768 and all(32 < ord(c) < 127 for c in value)


class Client:
    """Resolve IAM user credentials per Query and keep tokens in process memory."""

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
            raise ValueError("Huawei Cloud configuration and credentials are required")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None

    def list_accessible_projects(self) -> "ListAccessibleProjectsOperation":
        """Bind the two-request IAM project Query."""
        return ListAccessibleProjectsOperation(self)

    async def aclose(self) -> None:
        """Close a connector-owned transport."""
        if self._owns_http_client:
            await self.http_client.aclose()

    async def _request(
        self, method: str, path: str, body: bytes | None = None, token: str = ""
    ) -> tuple[int, bytes, str] | QueryAttempt[ListAccessibleProjectsOutput]:
        headers = {"Content-Type": "application/json;charset=utf8", "Accept": "application/json"}
        if token:
            headers["X-Auth-Token"] = token
        try:
            request = self.http_client.build_request(method, _ENDPOINT + path, content=body, headers=headers)
            response, _ = await send_traced(self.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(FailureKind.AVAILABILITY, "Huawei Cloud IAM request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Huawei Cloud IAM rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Huawei Cloud IAM is temporarily unavailable", delay)
            try:
                response_body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Huawei Cloud IAM response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Huawei Cloud IAM response was interrupted")
            return status, response_body, response.headers.get("X-Subject-Token", "")
        finally:
            await response.aclose()


class ListAccessibleProjectsOperation:
    """Obtain a domain-scoped token and read accessible project metadata."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query definition."""
        return LIST_ACCESSIBLE_PROJECTS_DEFINITION

    async def invoke(
        self, call: Call, input: ListAccessibleProjectsInput
    ) -> QueryAttempt[ListAccessibleProjectsOutput]:
        """Keep password and token out of the Flow result and failure."""
        try:
            credentials = await self.client.credentials.resolve(call)
            account = credentials.account_name.reveal()
            username = credentials.iam_username.reveal()
            password = credentials.iam_password.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch("defect", FailureKind.AUTHENTICATION, "IAM credentials are unavailable")
        if not all(_valid_credential(value) for value in (account, username, password)):
            return _branch("defect", FailureKind.AUTHENTICATION, "IAM credentials are invalid")
        body = json.dumps(
            {
                "auth": {
                    "identity": {
                        "methods": ["password"],
                        "password": {
                            "user": {"domain": {"name": account}, "name": username, "password": password}
                        },
                    },
                    "scope": {"domain": {"name": account}},
                }
            },
            separators=(",", ":"),
        ).encode()
        first = await self.client._request("POST", "/v3/auth/tokens", body)
        if isinstance(first, QueryAttempt):
            return first
        status, _, token = first
        if status != 201:
            return self._rejected(status, "token")
        if not _valid_token(token):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "IAM returned an invalid token header")
        second = await self.client._request("GET", "/v3/auth/projects", token=token)
        if isinstance(second, QueryAttempt):
            return second
        status, response_body, _ = second
        if status != 200:
            return self._rejected(status, "projects")
        try:
            wire: Any = json.loads(response_body)
            rows = wire["projects"]
            if not isinstance(rows, list):
                raise ValueError("projects missing")
            projects = []
            for row in rows:
                if (
                    not isinstance(row, dict)
                    or not isinstance(row.get("id"), str)
                    or not row["id"]
                    or type(row.get("enabled")) is not bool
                    or not isinstance(row.get("domain_id"), str)
                    or not row["domain_id"]
                ):
                    raise ValueError("invalid project")
                projects.append(
                    AccessibleProject(row["id"], row.get("name", ""), row["enabled"], row["domain_id"])
                )
        except (KeyError, TypeError, ValueError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "IAM returned invalid project data")
        return QueryAttempt.branch_result("found", ListAccessibleProjectsOutput(tuple(projects)), Receipt())

    @staticmethod
    def _rejected(status: int, phase: str) -> QueryAttempt[ListAccessibleProjectsOutput]:
        kind = (
            FailureKind.AUTHENTICATION
            if status == 401
            else FailureKind.AUTHORIZATION
            if status == 403
            else FailureKind.PROVIDER_REJECTION
        )
        return _branch(
            "providerRejected", kind, f"Huawei Cloud IAM rejected the {phase} request with HTTP {status}"
        )
