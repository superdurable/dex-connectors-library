# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Vercel provider reads and Go-compatible branch classification."""

import json
from collections.abc import Callable
from datetime import UTC, datetime, timedelta
from typing import Any, TypeVar
from urllib.parse import quote, urlencode

import httpx
from dex_connectors.sdk import (
    BranchDefinition,
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    QueryAttempt,
    QueryDefinition,
    ReauthorizationRequiredError,
    Receipt,
    StepDefaults,
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
from dex_connectors.sdk.retry_policy import default_connector_step_execute_retry_policy

from ._generated.config import (
    Config as Config,
)
from ._generated.config import (
    Credentials as Credentials,
)
from ._generated.definitions import (
    CONNECTOR_ID as CONNECTOR_ID,
)
from ._generated.definitions import (
    DEFECT as DEFECT,
)
from ._generated.definitions import (
    FOUND as FOUND,
)
from ._generated.definitions import (
    GET_DEPLOYMENT_DEFINITION as GET_DEPLOYMENT_DEFINITION,
)
from ._generated.definitions import (
    GET_DEPLOYMENT_OPERATION_ID as GET_DEPLOYMENT_OPERATION_ID,
)
from ._generated.definitions import (
    INVALID_RESPONSE as INVALID_RESPONSE,
)
from ._generated.definitions import (
    LIST_DEPLOYMENTS_DEFINITION as LIST_DEPLOYMENTS_DEFINITION,
)
from ._generated.definitions import (
    LIST_DEPLOYMENTS_OPERATION_ID as LIST_DEPLOYMENTS_OPERATION_ID,
)
from ._generated.definitions import (
    LISTED as LISTED,
)
from ._generated.definitions import (
    NOT_FOUND as NOT_FOUND,
)
from ._generated.definitions import (
    PROVIDER_REJECTED as PROVIDER_REJECTED,
)
from .models import Deployment, DeploymentsPage, GetDeploymentInput, ListDeploymentsInput, Pagination

PROVIDER_NAME = "Vercel"
_DEFAULT_BASE_URL = "https://api.vercel.com"
_DEFAULT_TIMEOUT_SECONDS = 15
_DEFAULT_MAX_RESPONSE_BYTES = 2_097_152
_STEP_DEFAULTS = StepDefaults(
    timedelta(seconds=30), 2, execute_retry=default_connector_step_execute_retry_policy()
)
_SHARED_BRANCHES = (
    BranchDefinition(PROVIDER_REJECTED, "The provider rejected the request.", True),
    BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
    BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
)
_STATUS_KINDS = {
    401: FailureKind.AUTHENTICATION,
    403: FailureKind.AUTHORIZATION,
    404: FailureKind.NOT_FOUND,
    400: FailureKind.VALIDATION,
    422: FailureKind.VALIDATION,
}

Output = TypeVar("Output")






class Client:
    """One redirect-safe HTTP client and call-time credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None:
            raise ValueError("Vercel configuration is required")
        if credentials is None:
            raise ValueError("Vercel credential source is required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("Vercel timeout and maxResponseBytes must be positive")
        self.base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL)
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def list_deployments(self) -> "ListDeploymentsOperation":
        """Bind the deployment page Query to this client."""
        return ListDeploymentsOperation(self)

    def get_deployment(self) -> "GetDeploymentOperation":
        """Bind the deployment lookup Query to this client."""
        return GetDeploymentOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def execute_query(
        self,
        call: Call,
        operation: str,
        path: str,
        empty: Output,
        success: str,
        has_not_found_branch: bool,
        decode: Callable[[Any], Output],
    ) -> QueryAttempt[Output]:
        """Send one GET and classify it exactly like the Go executeQuery helper."""
        try:
            credentials = await self.credentials.resolve(call)
        except (ValueError, LookupError, OSError, ReauthorizationRequiredError):
            return _failed(
                operation, DEFECT, empty, FailureKind.AUTHENTICATION, "connection credentials are unavailable"
            )
        token = credentials.access_token.reveal()
        if not token or not is_header_safe_credential(token):
            return _failed(
                operation, DEFECT, empty, FailureKind.AUTHENTICATION, "connection credentials are invalid"
            )
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + path,
                headers={"Authorization": "Bearer " + token, "Accept": "application/json"},
            )
        except (httpx.HTTPError, ValueError):
            return _failed(operation, DEFECT, empty, FailureKind.LOCAL_DEFECT, "request could not be built")
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return _retry(operation, FailureKind.AVAILABILITY, f"{PROVIDER_NAME} request failed", 0)
        try:
            status = response.status_code
            if status in (408, 409, 429) or status >= 500:
                kind = FailureKind.RATE_LIMIT if status == 429 else FailureKind.AVAILABILITY
                retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
                return _retry(operation, kind, f"{PROVIDER_NAME} returned HTTP {status}", retry_after)
            if status != 200:
                branch = NOT_FOUND if status == 404 and has_not_found_branch else PROVIDER_REJECTED
                kind = _STATUS_KINDS.get(status, FailureKind.PROVIDER_REJECTION)
                return _failed(operation, branch, empty, kind, f"{PROVIDER_NAME} returned HTTP {status}")
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _failed(
                    operation,
                    INVALID_RESPONSE,
                    empty,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(operation, FailureKind.AVAILABILITY, "response was interrupted", 0)
            try:
                value = decode(json.loads(body))
            except (ValueError, TypeError, UnicodeDecodeError):
                return _failed(
                    operation, INVALID_RESPONSE, empty, FailureKind.PROTOCOL, "response was malformed"
                )
            return QueryAttempt.branch_result(success, value, Receipt())
        finally:
            await response.aclose()


class ListDeploymentsOperation:
    """Read the first page of deployments visible to the account or team."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Vercel client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return LIST_DEPLOYMENTS_DEFINITION

    async def invoke(self, call: Call, input: ListDeploymentsInput) -> QueryAttempt[DeploymentsPage]:
        """Read once, then select a branch or safe retry."""
        operation = LIST_DEPLOYMENTS_OPERATION_ID
        if not isinstance(input, ListDeploymentsInput) or not isinstance(input.team_id, str):
            return _failed(
                operation, DEFECT, DeploymentsPage(), FailureKind.VALIDATION, "teamId must be text"
            )
        return await self.client.execute_query(
            call,
            operation,
            "/v6/deployments" + _team_query(input.team_id),
            DeploymentsPage(),
            LISTED,
            False,
            _decode_deployments,
        )


class GetDeploymentOperation:
    """Read one deployment by Vercel deployment ID or hostname."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Vercel client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return GET_DEPLOYMENT_DEFINITION

    async def invoke(self, call: Call, input: GetDeploymentInput) -> QueryAttempt[Deployment]:
        """Validate the identifier, read once, then select a branch or safe retry."""
        operation = GET_DEPLOYMENT_OPERATION_ID
        if (
            not isinstance(input, GetDeploymentInput)
            or not isinstance(input.id_or_url, str)
            or not isinstance(input.team_id, str)
            or not input.id_or_url.strip()
        ):
            return _failed(operation, DEFECT, Deployment(), FailureKind.VALIDATION, "idOrUrl is required")
        path = "/v13/deployments/" + quote(input.id_or_url, safe="") + _team_query(input.team_id)
        return await self.client.execute_query(
            call, operation, path, Deployment(), FOUND, True, _decode_identified_deployment
        )


def _team_query(team_id: str) -> str:
    return "?" + urlencode({"teamId": team_id}) if team_id else ""


def _failed(
    operation: str, branch: str, value: Output, kind: FailureKind, message: str
) -> QueryAttempt[Output]:
    return QueryAttempt.branch_result(
        branch, value, Receipt(), Failure(kind, CONNECTOR_ID, operation, message)
    )


def _retry(operation: str, kind: FailureKind, message: str, after_seconds: float) -> QueryAttempt[Any]:
    return QueryAttempt.retry(Failure(kind, CONNECTOR_ID, operation, message), after_seconds)


def _decode_deployments(document: Any) -> DeploymentsPage:
    document = _object(document)
    deployments = document.get("deployments")
    if not isinstance(deployments, list):
        raise ValueError
    pagination = _object(document.get("pagination") or {})
    next_cursor = pagination.get("next")
    if next_cursor is not None and type(next_cursor) is not int:
        raise ValueError
    return DeploymentsPage(tuple(_decode_deployment(item) for item in deployments), Pagination(next_cursor))


def _decode_identified_deployment(document: Any) -> Deployment:
    deployment = _decode_deployment(document)
    if not deployment.uid and not deployment.id:
        raise ValueError
    return deployment


def _decode_deployment(document: Any) -> Deployment:
    document = _object(document)
    created = document.get("created")
    if created is None:
        created = 0
    if type(created) is not int:
        raise ValueError
    return Deployment(
        _string(document, "uid"),
        _string(document, "id"),
        _string(document, "name"),
        _string(document, "url"),
        _string(document, "state"),
        _string(document, "readyState"),
        created,
    )


def _object(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError
    return value


def _string(document: dict[str, Any], name: str) -> str:
    value = document.get(name)
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError
    return value
