# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Qingflow application directory Query."""

import json
from datetime import UTC, datetime

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
from .models import ApplicationMetadata, ListApplicationsInput, ListApplicationsOutput

_ENDPOINT = "https://api.qingflow.com/app"


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListApplicationsOutput]:
    return QueryAttempt.branch_result(
        branch, ListApplicationsOutput(), Receipt(), Failure(kind, "qingflow", "listApplications", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListApplicationsOutput]:
    return QueryAttempt.retry(Failure(kind, "qingflow", "listApplications", message), delay)


def _valid_token(value: str) -> bool:
    return 0 < len(value) <= 1024 and value.strip() == value and all(0x20 < ord(c) < 0x7F for c in value)


class Client:
    """Own Qingflow HTTP limits and resolve the current access token per attempt."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Qingflow configuration and credentials are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("Qingflow timeout or response limit is invalid")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_applications(self) -> "ListApplicationsOperation":
        """Bind the application-list Query to this client."""
        return ListApplicationsOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP transport only when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class ListApplicationsOperation:
    """Read names and keys of applications visible to the super-admin token."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Qingflow client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated application-list Query contract."""
        return LIST_APPLICATIONS_DEFINITION

    async def invoke(self, call: Call, input: ListApplicationsInput) -> QueryAttempt[ListApplicationsOutput]:
        """Make one bounded request and classify the provider response."""
        try:
            credentials = await self.client.credentials.resolve(call)
            access_token = credentials.access_token.reveal()
            if not _valid_token(access_token):
                raise ValueError("invalid access token")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Qingflow accessToken is unavailable or invalid"
            )
        try:
            request = self.client.http_client.build_request(
                "GET", _ENDPOINT, headers={"accessToken": access_token, "Accept": "application/json"}
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "Qingflow application-list request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Qingflow application-list request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if response.status_code == 429:
                return _retry(FailureKind.RATE_LIMIT, "Qingflow rate limited the request", delay)
            if response.status_code == 408 or response.status_code >= 500:
                return _retry(
                    FailureKind.AVAILABILITY, "Qingflow application list is temporarily unavailable", delay
                )
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
                    "providerRejected", kind, f"Qingflow rejected with HTTP {response.status_code}"
                )
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse", FailureKind.RESPONSE_TOO_LARGE, "Qingflow response exceeded limit"
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Qingflow response was interrupted")
            try:
                wire = json.loads(body, parse_constant=lambda _: (_ for _ in ()).throw(ValueError()))
                code = wire["errCode"]
                if type(code) is not int:
                    raise ValueError("invalid errCode")
                if code != 0:
                    return _branch(
                        "providerRejected",
                        FailureKind.PROVIDER_REJECTION,
                        f"Qingflow rejected with code {code}",
                    )
                items = wire["result"]["appList"]
                if not isinstance(items, list):
                    raise ValueError("invalid appList")
                applications = []
                for item in items:
                    key, name = item["appKey"], item["appName"]
                    if not isinstance(key, str) or not key or not isinstance(name, str) or not name:
                        raise ValueError("invalid application")
                    applications.append(ApplicationMetadata(key, name))
            except (ValueError, KeyError, TypeError, UnicodeDecodeError):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "Qingflow returned invalid application metadata"
                )
            return QueryAttempt.branch_result(
                "listed", ListApplicationsOutput(tuple(applications)), Receipt()
            )
        finally:
            await response.aclose()
