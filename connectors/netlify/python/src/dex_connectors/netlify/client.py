# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Netlify provider reads and Go-compatible branch classification."""

import json
from collections.abc import Callable
from dataclasses import dataclass as dataclass
from datetime import UTC, datetime
from datetime import timedelta as timedelta
from typing import Any, TypeVar
from urllib.parse import quote

import httpx
from dex_connectors.sdk import BranchDefinition as BranchDefinition
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    QueryAttempt,
    QueryDefinition,
    ReauthorizationRequiredError,
    Receipt,
)
from dex_connectors.sdk import OperationRef as OperationRef
from dex_connectors.sdk import SecretString as SecretString
from dex_connectors.sdk import StepDefaults as StepDefaults
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
from dex_connectors.sdk.retry_policy import (
    default_connector_step_execute_retry_policy as default_connector_step_execute_retry_policy,
)

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
    INVALID_RESPONSE as INVALID_RESPONSE,
)
from ._generated.definitions import (
    LIST_DEPLOYS_DEFINITION as LIST_DEPLOYS_DEFINITION,
)
from ._generated.definitions import (
    LIST_DEPLOYS_OPERATION_ID as LIST_DEPLOYS_OPERATION_ID,
)
from ._generated.definitions import (
    LIST_SITES_DEFINITION as LIST_SITES_DEFINITION,
)
from ._generated.definitions import (
    LIST_SITES_OPERATION_ID as LIST_SITES_OPERATION_ID,
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
from .models import Deploy, DeploysPage, ListDeploysInput, ListSitesInput, Site, SitesPage

PROVIDER_NAME = "Netlify"
_DEFAULT_BASE_URL = "https://api.netlify.com/api/v1"
_DEFAULT_TIMEOUT_SECONDS = 15
_DEFAULT_MAX_RESPONSE_BYTES = 2_097_152
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
            raise ValueError("Netlify configuration is required")
        if credentials is None:
            raise ValueError("Netlify credential source is required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("Netlify timeout and maxResponseBytes must be positive")
        self.base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL)
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def list_sites(self) -> "ListSitesOperation":
        """Bind the site page Query to this client."""
        return ListSitesOperation(self)

    def list_deploys(self) -> "ListDeploysOperation":
        """Bind the deploy page Query to this client."""
        return ListDeploysOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def execute_query(
        self,
        call: Call,
        operation: str,
        path: str,
        empty: Output,
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
            return QueryAttempt.branch_result(LISTED, value, Receipt())
        finally:
            await response.aclose()


class ListSitesOperation:
    """Read every site the token can see."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Netlify client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return LIST_SITES_DEFINITION

    async def invoke(self, call: Call, input: ListSitesInput) -> QueryAttempt[SitesPage]:
        """Read once, then select a branch or safe retry."""
        operation = LIST_SITES_OPERATION_ID
        if not isinstance(input, ListSitesInput):
            return _failed(operation, DEFECT, SitesPage(), FailureKind.VALIDATION, "input is invalid")
        return await self.client.execute_query(call, operation, "/sites", SitesPage(), False, _decode_sites)


class ListDeploysOperation:
    """Read the deploys of one Netlify site."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Netlify client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return LIST_DEPLOYS_DEFINITION

    async def invoke(self, call: Call, input: ListDeploysInput) -> QueryAttempt[DeploysPage]:
        """Validate the site ID, read once, then select a branch or safe retry."""
        operation = LIST_DEPLOYS_OPERATION_ID
        if (
            not isinstance(input, ListDeploysInput)
            or not isinstance(input.site_id, str)
            or not input.site_id.strip()
        ):
            return _failed(operation, DEFECT, DeploysPage(), FailureKind.VALIDATION, "siteId is required")
        path = "/sites/" + quote(input.site_id, safe="") + "/deploys"
        return await self.client.execute_query(call, operation, path, DeploysPage(), True, _decode_deploys)


def _failed(
    operation: str, branch: str, value: Output, kind: FailureKind, message: str
) -> QueryAttempt[Output]:
    return QueryAttempt.branch_result(
        branch, value, Receipt(), Failure(kind, CONNECTOR_ID, operation, message)
    )


def _retry(operation: str, kind: FailureKind, message: str, after_seconds: float) -> QueryAttempt[Any]:
    return QueryAttempt.retry(Failure(kind, CONNECTOR_ID, operation, message), after_seconds)


def _decode_sites(document: Any) -> SitesPage:
    return SitesPage(
        tuple(
            Site(_string(item, "id"), _string(item, "name"), _string(item, "url"), _string(item, "admin_url"))
            for item in map(_object, _array(document))
        )
    )


def _decode_deploys(document: Any) -> DeploysPage:
    return DeploysPage(
        tuple(
            Deploy(
                _string(item, "id"), _string(item, "site_id"), _string(item, "state"), _string(item, "url")
            )
            for item in map(_object, _array(document))
        )
    )


def _array(value: Any) -> list[Any]:
    if not isinstance(value, list):
        raise ValueError
    return value


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
