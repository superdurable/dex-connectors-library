# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""PagerDuty provider calls and Go-compatible branch classification."""

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
    FOUND as FOUND,
)
from ._generated.definitions import (
    GET_INCIDENT_DEFINITION as GET_INCIDENT_DEFINITION,
)
from ._generated.definitions import (
    GET_INCIDENT_OPERATION_ID as GET_INCIDENT_OPERATION_ID,
)
from ._generated.definitions import (
    INVALID_RESPONSE as INVALID_RESPONSE,
)
from ._generated.definitions import (
    LIST_INCIDENTS_DEFINITION as LIST_INCIDENTS_DEFINITION,
)
from ._generated.definitions import (
    LIST_INCIDENTS_OPERATION_ID as LIST_INCIDENTS_OPERATION_ID,
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
from .models import GetIncidentInput, Incident, IncidentPage, ListIncidentsInput

_DEFAULT_BASE_URL = "https://api.pagerduty.com"
_DEFAULT_TIMEOUT_SECONDS = 15
_DEFAULT_MAX_RESPONSE_BYTES = 2_097_152

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
            raise ValueError("PagerDuty configuration is required")
        if credentials is None:
            raise ValueError("PagerDuty credential source is required")
        self.base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL)
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("PagerDuty timeout and max response bytes must be positive")
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def list_incidents(self) -> "ListIncidentsOperation":
        """Bind the incident list Query to this client."""
        return ListIncidentsOperation(self)

    def get_incident(self) -> "GetIncidentOperation":
        """Bind the incident lookup Query to this client."""
        return GetIncidentOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def _run_query(
        self,
        call: Call,
        operation: str,
        path: str,
        empty: Output,
        success: str,
        decode: Callable[[dict[str, Any]], Output],
        has_not_found_branch: bool = False,
    ) -> QueryAttempt[Output]:
        """Send one GET and classify it exactly like the Go runQuery helper."""
        try:
            credentials = await self.credentials.resolve(call)
        except (ValueError, LookupError, OSError, ReauthorizationRequiredError):
            return _branch(
                operation, DEFECT, empty, FailureKind.AUTHENTICATION, "connection credentials are unavailable"
            )
        token = credentials.api_key.reveal()
        if not token.strip() or not is_header_safe_credential(token):
            return _branch(
                operation, DEFECT, empty, FailureKind.AUTHENTICATION, "connection credentials are invalid"
            )
        headers = {
            "Authorization": "Token token=" + token,
            "Accept": "application/vnd.pagerduty+json;version=2",
            "User-Agent": "superdurable-pagerduty/0.1",
        }
        try:
            request = self.http_client.build_request("GET", self.base_url + path, headers=headers)
        except (httpx.HTTPError, ValueError):
            return _branch(
                operation, DEFECT, empty, FailureKind.LOCAL_DEFECT, "PagerDuty request could not be built"
            )
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return QueryAttempt.retry(
                _failure(operation, FailureKind.AVAILABILITY, "provider request failed")
            )
        try:
            status = response.status_code
            if status == 429:
                return QueryAttempt.retry(
                    _failure(operation, FailureKind.RATE_LIMIT, "provider rate limited the request"),
                    _retry_after_seconds(response),
                )
            if status == 408 or status >= 500:
                return QueryAttempt.retry(
                    _failure(operation, FailureKind.AVAILABILITY, "provider is temporarily unavailable"),
                    _retry_after_seconds(response),
                )
            if status == 404 and has_not_found_branch:
                return _branch(
                    operation, NOT_FOUND, empty, FailureKind.NOT_FOUND, "provider resource was not found"
                )
            if status != 200:
                return _branch(
                    operation,
                    PROVIDER_REJECTED,
                    empty,
                    FailureKind.PROVIDER_REJECTION,
                    f"provider rejected the request with HTTP {status}",
                )
            try:
                content = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    operation,
                    INVALID_RESPONSE,
                    empty,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "provider response exceeded the configured limit",
                )
            except ProviderTransportError:
                return QueryAttempt.retry(
                    _failure(operation, FailureKind.AVAILABILITY, "provider response was interrupted")
                )
            try:
                document = json.loads(content)
                if not isinstance(document, dict):
                    raise ValueError
                value = decode(document)
            except (ValueError, TypeError, UnicodeDecodeError):
                return _branch(
                    operation,
                    INVALID_RESPONSE,
                    empty,
                    FailureKind.PROTOCOL,
                    "provider response is incomplete or malformed",
                )
            return QueryAttempt.branch_result(success, value, Receipt())
        finally:
            await response.aclose()


class ListIncidentsOperation:
    """List one page of PagerDuty incidents."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("PagerDuty client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return LIST_INCIDENTS_DEFINITION

    async def invoke(self, call: Call, input: ListIncidentsInput) -> QueryAttempt[IncidentPage]:
        """Validate the page window, read once, then select a branch or safe retry."""
        operation = LIST_INCIDENTS_OPERATION_ID
        if (
            not isinstance(input, ListIncidentsInput)
            or type(input.limit) is not int
            or type(input.offset) is not int
        ):
            return _branch(
                operation, DEFECT, IncidentPage(), FailureKind.VALIDATION, "search input is invalid"
            )
        limit = input.limit or 25
        if limit < 1 or limit > 100 or input.offset < 0:
            return _branch(
                operation,
                DEFECT,
                IncidentPage(),
                FailureKind.VALIDATION,
                "limit must be 1–100 and offset nonnegative",
            )
        path = f"/incidents?limit={limit}&offset={input.offset}"
        return await self.client._run_query(
            call, operation, path, IncidentPage(), LISTED, _decode_incident_page
        )


class GetIncidentOperation:
    """Read one PagerDuty incident by ID."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("PagerDuty client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return GET_INCIDENT_DEFINITION

    async def invoke(self, call: Call, input: GetIncidentInput) -> QueryAttempt[Incident]:
        """Validate the incident ID, read once, then select a branch or safe retry."""
        operation = GET_INCIDENT_OPERATION_ID
        if not isinstance(input, GetIncidentInput) or not isinstance(input.id, str) or not input.id:
            return _branch(operation, DEFECT, Incident(), FailureKind.VALIDATION, "incident ID is required")
        path = "/incidents/" + quote(input.id, safe="")
        return await self.client._run_query(
            call, operation, path, Incident(), FOUND, _decode_incident_envelope, has_not_found_branch=True
        )


def _retry_after_seconds(response: httpx.Response) -> float:
    return parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, CONNECTOR_ID, operation, message)


def _branch(
    operation: str, branch: str, value: Output, kind: FailureKind, message: str
) -> QueryAttempt[Output]:
    return QueryAttempt.branch_result(branch, value, Receipt(), _failure(operation, kind, message))


def _decode_incident_page(document: dict[str, Any]) -> IncidentPage:
    incidents = _field(document, "incidents")
    if not isinstance(incidents, list):
        raise ValueError
    return IncidentPage(
        tuple(_decode_incident(incident) for incident in incidents),
        _integer(document, "limit"),
        _integer(document, "offset"),
        _boolean(document, "more"),
    )


def _decode_incident_envelope(document: dict[str, Any]) -> Incident:
    wrapped = _field(document, "incident")
    incident = _decode_incident({} if wrapped is None else wrapped)
    if not incident.id:
        raise ValueError
    return incident


def _decode_incident(document: Any) -> Incident:
    if not isinstance(document, dict):
        raise ValueError
    return Incident(_string(document, "id"), _string(document, "title"), _string(document, "status"))


def _field(document: dict[str, Any], name: str) -> Any:
    """Match Go's case-insensitive JSON field lookup, preferring an exact key."""
    if name in document:
        return document[name]
    folded = name.casefold()
    for key, value in document.items():
        if key.casefold() == folded:
            return value
    return None


def _string(document: dict[str, Any], name: str) -> str:
    value = _field(document, name)
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError
    return value


def _integer(document: dict[str, Any], name: str) -> int:
    value = _field(document, name)
    if value is None:
        return 0
    if type(value) is not int:
        raise ValueError
    return value


def _boolean(document: dict[str, Any], name: str) -> bool:
    value = _field(document, name)
    if value is None:
        return False
    if not isinstance(value, bool):
        raise ValueError
    return value
