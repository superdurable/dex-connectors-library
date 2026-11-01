# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Mailgun provider reads and Go-compatible branch classification."""

import base64
import json
import re
from collections.abc import Callable
from dataclasses import dataclass as dataclass
from datetime import UTC, datetime
from datetime import timedelta as timedelta
from typing import Any, TypeVar
from urllib.parse import urlencode

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
    GET_EVENTS_DEFINITION as GET_EVENTS_DEFINITION,
)
from ._generated.definitions import (
    GET_EVENTS_OPERATION_ID as GET_EVENTS_OPERATION_ID,
)
from ._generated.definitions import (
    INVALID_RESPONSE as INVALID_RESPONSE,
)
from ._generated.definitions import (
    LIST_DOMAINS_DEFINITION as LIST_DOMAINS_DEFINITION,
)
from ._generated.definitions import (
    LIST_DOMAINS_OPERATION_ID as LIST_DOMAINS_OPERATION_ID,
)
from ._generated.definitions import (
    LISTED as LISTED,
)
from ._generated.definitions import (
    PROVIDER_REJECTED as PROVIDER_REJECTED,
)
from .models import Domain, DomainsOutput, Event, EventsOutput, GetEventsInput, ListDomainsInput

_DEFAULT_BASE_URL = "https://api.mailgun.net"
_DEFAULT_TIMEOUT_SECONDS = 15
_DEFAULT_MAX_RESPONSE_BYTES = 1_048_576
_DOMAIN = re.compile(r"[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])?")

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
            raise ValueError("mailgun configuration is required")
        if credentials is None:
            raise ValueError("mailgun credential source is required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("mailgun timeout and maxResponseBytes must be positive")
        self.base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL)
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def get_events(self) -> "GetEventsOperation":
        """Bind the domain event page Query to this client."""
        return GetEventsOperation(self)

    def list_domains(self) -> "ListDomainsOperation":
        """Bind the domain page Query to this client."""
        return ListDomainsOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def invoke_json_query(
        self,
        call: Call,
        operation: str,
        path: str,
        query: dict[str, str],
        empty: Output,
        decode: Callable[[Any], Output],
    ) -> QueryAttempt[Output]:
        """Send one GET and classify it exactly like the Go invokeJSONQuery helper."""
        try:
            credentials = await self.credentials.resolve(call)
        except (ValueError, LookupError, OSError, ReauthorizationRequiredError):
            return _failed(operation, DEFECT, empty, FailureKind.AUTHENTICATION, _CREDENTIAL_MESSAGE)
        key = credentials.api_key.reveal()
        if not key or not is_header_safe_credential(key):
            return _failed(operation, DEFECT, empty, FailureKind.AUTHENTICATION, _CREDENTIAL_MESSAGE)
        endpoint = self.base_url + path
        if query:
            endpoint += "?" + urlencode(sorted(query.items()))
        try:
            request = self.http_client.build_request(
                "GET",
                endpoint,
                headers={
                    "Accept": "application/json",
                    "Authorization": "Basic " + base64.b64encode(f"api:{key}".encode()).decode(),
                },
            )
            response, _ = await send_traced(self.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(operation, FailureKind.AVAILABILITY, "provider request failed", 0)
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(
                    operation, FailureKind.RATE_LIMIT, "provider rate limited the request", retry_after
                )
            if status == 408 or status >= 500:
                return _retry(
                    operation, FailureKind.AVAILABILITY, "provider is temporarily unavailable", retry_after
                )
            if status != 200:
                return _failed(
                    operation,
                    PROVIDER_REJECTED,
                    empty,
                    FailureKind.PROVIDER_REJECTION,
                    f"provider rejected the request with HTTP {status}",
                )
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _failed(
                    operation,
                    INVALID_RESPONSE,
                    empty,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "provider response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(operation, FailureKind.AVAILABILITY, "provider response was interrupted", 0)
            try:
                value = decode(json.loads(body))
            except (ValueError, TypeError, UnicodeDecodeError):
                return _failed(
                    operation,
                    INVALID_RESPONSE,
                    empty,
                    FailureKind.PROTOCOL,
                    "provider returned an invalid response",
                )
            return QueryAttempt.branch_result(LISTED, value, Receipt())
        finally:
            await response.aclose()


class GetEventsOperation:
    """Read one page of a sending domain's events."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("mailgun client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return GET_EVENTS_DEFINITION

    async def invoke(self, call: Call, input: GetEventsInput) -> QueryAttempt[EventsOutput]:
        """Validate the domain and limit, read once, then select a branch or safe retry."""
        operation = GET_EVENTS_OPERATION_ID
        if (
            not isinstance(input, GetEventsInput)
            or not isinstance(input.domain, str)
            or _DOMAIN.fullmatch(input.domain) is None
            or type(input.limit) is not int
            or not 0 <= input.limit <= 300
        ):
            return _failed(
                operation, DEFECT, EventsOutput(), FailureKind.VALIDATION, "domain or limit is invalid"
            )
        query = {"limit": str(input.limit)} if input.limit > 0 else {}
        return await self.client.invoke_json_query(
            call, operation, f"/v3/{input.domain}/events", query, EventsOutput(), _decode_events
        )


class ListDomainsOperation:
    """Read one page of the account's sending domains."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("mailgun client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return LIST_DOMAINS_DEFINITION

    async def invoke(self, call: Call, input: ListDomainsInput) -> QueryAttempt[DomainsOutput]:
        """Validate the window, read once, then select a branch or safe retry."""
        operation = LIST_DOMAINS_OPERATION_ID
        if (
            not isinstance(input, ListDomainsInput)
            or type(input.limit) is not int
            or type(input.skip) is not int
            or not 0 <= input.limit <= 1000
            or input.skip < 0
        ):
            return _failed(
                operation, DEFECT, DomainsOutput(), FailureKind.VALIDATION, "limit or skip is invalid"
            )
        query: dict[str, str] = {}
        if input.limit > 0:
            query["limit"] = str(input.limit)
        if input.skip > 0:
            query["skip"] = str(input.skip)
        return await self.client.invoke_json_query(
            call, operation, "/v4/domains", query, DomainsOutput(), _decode_domains
        )


_CREDENTIAL_MESSAGE = "connection credentials are unavailable or invalid"


def _failed(
    operation: str, branch: str, value: Output, kind: FailureKind, message: str
) -> QueryAttempt[Output]:
    return QueryAttempt.branch_result(
        branch, value, Receipt(), Failure(kind, CONNECTOR_ID, operation, message)
    )


def _retry(operation: str, kind: FailureKind, message: str, after_seconds: float) -> QueryAttempt[Any]:
    return QueryAttempt.retry(Failure(kind, CONNECTOR_ID, operation, message), after_seconds)


def _decode_events(document: Any) -> EventsOutput:
    items = _object(document).get("items")
    if not isinstance(items, list):
        raise ValueError
    events = []
    for item in map(_object, items):
        timestamp = _number(item, "timestamp")
        events.append(
            Event(_string(item, "id"), _string(item, "event"), 0.0 if timestamp is None else timestamp)
        )
    return EventsOutput(tuple(events))


def _decode_domains(document: Any) -> DomainsOutput:
    document = _object(document)
    items = document.get("items")
    total = document.get("total_count")
    if not isinstance(items, list) or type(total) is not int:
        raise ValueError
    return DomainsOutput(
        tuple(Domain(_string(item, "name"), _string(item, "state")) for item in map(_object, items)), total
    )


def _object(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError
    return value


def _number(document: dict[str, Any], name: str) -> float | None:
    value = document.get(name)
    if value is None:
        return None
    if type(value) not in (int, float):
        raise ValueError
    return float(value)


def _string(document: dict[str, Any], name: str) -> str:
    value = document.get(name)
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError
    return value


Config.__doc__ = "Mailgun HTTP settings; zero or blank values use manifest defaults."
Credentials.__doc__ = "Mailgun private API key, resolved anew for every provider attempt."
