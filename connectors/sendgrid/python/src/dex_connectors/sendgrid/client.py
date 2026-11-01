# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""SendGrid statistics and template reads with Go-compatible classification."""

import json
import re
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, date, datetime, timedelta
from typing import Any, TypeVar
from urllib.parse import parse_qs, urlencode, urlsplit

import httpx
from dex_connectors.sdk import (
    BranchDefinition,
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    OperationRef,
    QueryAttempt,
    QueryDefinition,
    ReauthorizationRequiredError,
    Receipt,
    SecretString,
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

from .models import GetStatsInput, ListTemplatesInput, StatsDay, StatsOutput, Template, TemplatesOutput

CONNECTOR_ID = "sendgrid"
GET_STATS_OPERATION_ID = "getStats"
LIST_TEMPLATES_OPERATION_ID = "listTemplates"
FOUND = "found"
LISTED = "listed"
PROVIDER_REJECTED = "providerRejected"
INVALID_RESPONSE = "invalidResponse"
DEFECT = "defect"
TEMPLATE_GENERATIONS = ("dynamic", "legacy")
_DEFAULT_BASE_URL = "https://api.sendgrid.com"
_DEFAULT_TIMEOUT_SECONDS = 15
_DEFAULT_MAX_RESPONSE_BYTES = 1_048_576
_DEFAULT_TEMPLATE_PAGE_SIZE = 100
_MAX_TEMPLATE_PAGE_SIZE = 200
_ISO_DATE = re.compile(r"\d{4}-\d{2}-\d{2}")
_CREDENTIAL_MESSAGE = "connection credentials are unavailable or invalid"
_STEP_DEFAULTS = StepDefaults(
    timedelta(seconds=30), 2, execute_retry=default_connector_step_execute_retry_policy()
)
_SHARED_BRANCHES = (
    BranchDefinition(PROVIDER_REJECTED, "The provider conclusively rejected the request.", True),
    BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized data.", True),
    BranchDefinition(DEFECT, "Local input or credentials are invalid.", True),
)
GET_STATS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_STATS_OPERATION_ID),
    (BranchDefinition(FOUND, "SendGrid returned the requested result."), *_SHARED_BRANCHES),
    _STEP_DEFAULTS,
)
LIST_TEMPLATES_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_TEMPLATES_OPERATION_ID),
    (BranchDefinition(LISTED, "SendGrid returned the requested result."), *_SHARED_BRANCHES),
    _STEP_DEFAULTS,
)

Output = TypeVar("Output")


@dataclass(frozen=True, slots=True)
class Config:
    """SendGrid HTTP settings; zero or blank values use manifest defaults."""

    base_url: str = _DEFAULT_BASE_URL
    timeout_seconds: float = _DEFAULT_TIMEOUT_SECONDS
    max_response_bytes: int = _DEFAULT_MAX_RESPONSE_BYTES


@dataclass(frozen=True, slots=True)
class Credentials:
    """SendGrid API key, resolved anew for every provider attempt."""

    api_key: SecretString


class Client:
    """One redirect-safe HTTP client and call-time credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None:
            raise ValueError("sendgrid configuration is required")
        if credentials is None:
            raise ValueError("sendgrid credential source is required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("sendgrid timeout and maxResponseBytes must be positive")
        self.base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL)
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def get_stats(self) -> "GetStatsOperation":
        """Bind the global statistics Query to this client."""
        return GetStatsOperation(self)

    def list_templates(self) -> "ListTemplatesOperation":
        """Bind the transactional template listing Query to this client."""
        return ListTemplatesOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def invoke_query(
        self,
        call: Call,
        operation: str,
        success: str,
        method: str,
        path: str,
        query: dict[str, str],
        payload: bytes | None,
        empty: Output,
        decode: Callable[[bytes], Output],
    ) -> QueryAttempt[Output]:
        """Send one request and classify it exactly like the Go invokeJSONQuery helper."""
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
        headers = {"Authorization": "Bearer " + key, "Accept": "application/json"}
        if payload is not None:
            headers["Content-Type"] = "application/json"
        try:
            request = self.http_client.build_request(method, endpoint, content=payload, headers=headers)
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
                value = decode(body)
            except (ValueError, TypeError, UnicodeDecodeError):
                return _failed(
                    operation,
                    INVALID_RESPONSE,
                    empty,
                    FailureKind.PROTOCOL,
                    "provider returned an invalid response",
                )
            return QueryAttempt.branch_result(success, value, Receipt())
        finally:
            await response.aclose()


class GetStatsOperation:
    """Read global email statistics for an inclusive date range."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("sendgrid client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return GET_STATS_DEFINITION

    async def invoke(self, call: Call, input: GetStatsInput) -> QueryAttempt[StatsOutput]:
        """Validate the date range, read once, then select a branch or safe retry."""
        operation = GET_STATS_OPERATION_ID
        start = _parse_date(input.start_date) if isinstance(input, GetStatsInput) else None
        if start is None:
            return _failed(
                operation, DEFECT, StatsOutput(), FailureKind.VALIDATION, "startDate must be YYYY-MM-DD"
            )
        query = {"start_date": input.start_date}
        if input.end_date:
            end = _parse_date(input.end_date)
            if end is None or end < start:
                return _failed(
                    operation,
                    DEFECT,
                    StatsOutput(),
                    FailureKind.VALIDATION,
                    "endDate must be YYYY-MM-DD and not before startDate",
                )
            query["end_date"] = input.end_date
        return await self.client.invoke_query(
            call, operation, FOUND, "GET", "/v3/stats", query, None, StatsOutput(), _decode_stats
        )


class ListTemplatesOperation:
    """List one page of transactional templates of one generation."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("sendgrid client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return LIST_TEMPLATES_DEFINITION

    async def invoke(self, call: Call, input: ListTemplatesInput) -> QueryAttempt[TemplatesOutput]:
        """Validate the page selection, list once, then select a branch or safe retry."""
        operation = LIST_TEMPLATES_OPERATION_ID
        if not isinstance(input, ListTemplatesInput) or not isinstance(input.page_token, str):
            return _failed(
                operation, DEFECT, TemplatesOutput(), FailureKind.VALIDATION, "template page is invalid"
            )
        generation = input.generation or "dynamic"
        if generation not in TEMPLATE_GENERATIONS:
            return _failed(
                operation,
                DEFECT,
                TemplatesOutput(),
                FailureKind.VALIDATION,
                "generation must be dynamic or legacy",
            )
        page_size = input.page_size or _DEFAULT_TEMPLATE_PAGE_SIZE
        if type(page_size) is not int or not 1 <= page_size <= _MAX_TEMPLATE_PAGE_SIZE:
            return _failed(
                operation, DEFECT, TemplatesOutput(), FailureKind.VALIDATION, "pageSize must be 1 to 200"
            )
        query = {"generations": generation, "page_size": str(page_size)}
        if input.page_token:
            query["page_token"] = input.page_token
        return await self.client.invoke_query(
            call, operation, LISTED, "GET", "/v3/templates", query, None, TemplatesOutput(), _decode_templates
        )


def _parse_date(value: object) -> date | None:
    if not isinstance(value, str) or _ISO_DATE.fullmatch(value) is None:
        return None
    try:
        return date.fromisoformat(value)
    except ValueError:
        return None


def _decode_stats(body: bytes) -> StatsOutput:
    document = json.loads(body)
    if not isinstance(document, list):
        raise ValueError
    days = []
    for day in document:
        day = _optional_object(day)
        day_date = _optional(day, "date", "")
        stats = _optional(day, "stats", [])
        if not isinstance(day_date, str) or not day_date or not isinstance(stats, list) or not stats:
            raise ValueError
        requests = delivered = 0
        for category in stats:
            metrics = _optional_object(_optional(_optional_object(category), "metrics", {}))
            category_requests = _optional(metrics, "requests", 0)
            category_delivered = _optional(metrics, "delivered", 0)
            if type(category_requests) is not int or type(category_delivered) is not int:
                raise ValueError
            requests += category_requests
            delivered += category_delivered
        days.append(StatsDay(day_date, requests, delivered))
    return StatsOutput(tuple(days))


def _decode_templates(body: bytes) -> TemplatesOutput:
    document = _optional_object(json.loads(body))
    result = document.get("result")
    metadata = _optional_object(_optional(document, "_metadata", {}))
    count = _optional(metadata, "count", 0)
    next_url = _optional(metadata, "next", "")
    if not isinstance(result, list) or type(count) is not int or not isinstance(next_url, str):
        raise ValueError
    next_page_token = ""
    if next_url:
        parsed = urlsplit(next_url)
        if parsed.scheme not in ("https", "http"):
            raise ValueError
        next_page_token = parse_qs(parsed.query).get("page_token", [""])[0]
        if not next_page_token:
            raise ValueError
    return TemplatesOutput(tuple(_decode_template(template) for template in result), count, next_page_token)


def _decode_template(document: Any) -> Template:
    document = _optional_object(document)
    fields = tuple(_optional(document, name, "") for name in ("id", "name", "generation"))
    if not all(isinstance(value, str) for value in fields):
        raise ValueError
    return Template(*fields)


def _optional_object(value: Any) -> dict[str, Any]:
    """Return a JSON object, treating null like Go's zero struct."""
    if value is None:
        return {}
    if not isinstance(value, dict):
        raise ValueError
    return value


def _optional(document: dict[str, Any], name: str, default: Any) -> Any:
    """Return a JSON field, treating absent or null like Go's zero value."""
    value = document.get(name)
    return default if value is None else value


def _failed(
    operation: str, branch: str, value: Output, kind: FailureKind, message: str
) -> QueryAttempt[Output]:
    return QueryAttempt.branch_result(
        branch, value, Receipt(), Failure(kind, CONNECTOR_ID, operation, message)
    )


def _retry(operation: str, kind: FailureKind, message: str, after_seconds: float) -> QueryAttempt[Any]:
    return QueryAttempt.retry(Failure(kind, CONNECTOR_ID, operation, message), after_seconds)
