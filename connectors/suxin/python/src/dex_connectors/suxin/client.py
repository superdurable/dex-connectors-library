# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""suxin provider transport and Go-compatible Query decisions."""

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
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import LIST_ORGANIZATIONS_DEFINITION
from ._generated.models import OrganizationSummary
from .models import ListOrganizationsInput, ListOrganizationsOutput


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListOrganizationsOutput]:
    return QueryAttempt.branch_result(
        branch, ListOrganizationsOutput(), Receipt(), Failure(kind, "suxin", "listOrganizations", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListOrganizationsOutput]:
    return QueryAttempt.retry(Failure(kind, "suxin", "listOrganizations", message), delay)


class Client:
    """Resolve credentials per listOrganizations request and own a redirect-safe HTTP client."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("suxin configuration and credentials are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("suxin timeout or response limit is invalid")
        self.config = config
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_organizations(self) -> "ListOrganizationsOperation":
        """Bind the listOrganizations Query to this client."""
        return ListOrganizationsOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client if this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class ListOrganizationsOperation:
    """Perform the listOrganizations read and return bounded provider data."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("suxin client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Go-derived Query identity."""
        return LIST_ORGANIZATIONS_DEFINITION

    async def invoke(
        self, call: Call, input: ListOrganizationsInput
    ) -> QueryAttempt[ListOrganizationsOutput]:
        """Exchange app credentials for an ephemeral token, then list organizations."""
        try:
            credentials = await self.client.credentials.resolve(call)
            app_id, key = credentials.app_id.reveal(), credentials.api_key.reveal()
            if not app_id or not key:
                raise ValueError("missing credentials")
            base_url = validate_base_url(self.client.config.base_url)
            request = self.client.http_client.build_request(
                "POST",
                base_url + "/api/v1/auth/token",
                json={"appId": app_id, "apiKey": key},
                headers={"Content-Type": "application/json", "Accept": "application/json"},
            )
        except (ValueError, LookupError, OSError, httpx.HTTPError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
            )
        authorization = await self._read_stage(request)
        if isinstance(authorization, QueryAttempt):
            return authorization
        if not isinstance(authorization.get("success"), bool):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "invalid authorization result")
        if not authorization["success"]:
            return self._provider_failure(authorization.get("code"))
        data = authorization.get("data")
        if (
            not isinstance(data, dict)
            or not isinstance(data.get("accessToken"), str)
            or not data["accessToken"]
            or not isinstance(data.get("userId"), str)
            or not data["userId"]
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "invalid authorization data")
        request = self.client.http_client.build_request(
            "GET",
            base_url + "/api/v1/org/list",
            params={"userId": data["userId"]},
            headers={"Accept": "application/json", "Authorization": "Bearer " + data["accessToken"]},
        )
        organizations = await self._read_stage(request)
        if isinstance(organizations, QueryAttempt):
            return organizations
        if not isinstance(organizations.get("success"), bool):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "invalid organization result")
        if not organizations["success"]:
            return self._provider_failure(organizations.get("code"))
        items = organizations.get("data")
        if not isinstance(items, list):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "missing organizations")
        output = []
        for item in items:
            if (
                not isinstance(item, dict)
                or not isinstance(item.get("orgId"), str)
                or not item["orgId"]
                or not isinstance(item.get("name"), str)
                or not item["name"]
                or item.get("entityType") not in ("COMPANY", "SELF_EMPLOYED")
                or item.get("certifyStatus") not in ("VERIFIED", "UNVERIFIED")
            ):
                return _branch("invalidResponse", FailureKind.PROTOCOL, "invalid organization metadata")
            output.append(
                OrganizationSummary(
                    id=item["orgId"],
                    name=item["name"],
                    entity_type=item["entityType"],
                    certify_status=item["certifyStatus"],
                )
            )
        return QueryAttempt.branch_result(
            "listed", ListOrganizationsOutput(organizations=tuple(output)), Receipt()
        )

    async def _read_stage(
        self, request: httpx.Request
    ) -> dict[str, Any] | QueryAttempt[ListOrganizationsOutput]:
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Suxin transport failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Suxin rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Suxin temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch("invalidResponse", FailureKind.RESPONSE_TOO_LARGE, "Suxin response too large")
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Suxin response interrupted")
        finally:
            await response.aclose()
        if status != 200:
            kind = FailureKind.AUTHENTICATION if status in (401, 403) else FailureKind.PROVIDER_REJECTION
            return _branch("providerRejected", kind, "Suxin rejected request")
        try:
            document: Any = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Suxin invalid JSON")
        if not isinstance(document, dict):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Suxin invalid envelope")
        return document

    def _provider_failure(self, code: Any) -> QueryAttempt[ListOrganizationsOutput]:
        value = str(code)
        if value == "5000":
            return _retry(FailureKind.AVAILABILITY, "Suxin temporarily unavailable")
        kind = FailureKind.AUTHENTICATION if value == "4010" else FailureKind.PROVIDER_REJECTION
        return _branch("providerRejected", kind, "Suxin rejected request")
