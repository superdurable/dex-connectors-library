# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Authenticated Eventbrite reads with Go-compatible decisions."""

import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any, TypeVar

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
from ._generated.definitions import (
    GET_ATTENDEE_DEFINITION,
    LIST_EVENTS_DEFINITION,
    LIST_ORGANIZATIONS_DEFINITION,
)
from .models import (
    AttendeeMetadata,
    EventMetadata,
    EventsPage,
    GetAttendeeInput,
    ListEventsInput,
    ListOrganizationsInput,
    OrganizationMetadata,
    OrganizationsPage,
)

_NUMERIC_ID = re.compile(r"[0-9]{1,24}\Z")
Output = TypeVar("Output")


@dataclass(frozen=True, slots=True)
class _Outcome:
    """One classified provider failure or retry."""

    branch: str
    failure: Failure
    is_retry: bool = False
    retry_after_seconds: float = 0

    def with_empty(self, empty: Output) -> QueryAttempt[Output]:
        """Apply the decision to an operation's empty output type."""
        if self.is_retry:
            return QueryAttempt.retry(self.failure, self.retry_after_seconds)
        return QueryAttempt.branch_result(self.branch, empty, Receipt(), self.failure)


def _failure(operation: str, branch: str, kind: FailureKind, message: str) -> _Outcome:
    return _Outcome(branch, Failure(kind, "eventbrite", operation, message))


def _invalid(operation: str, empty: Output) -> QueryAttempt[Output]:
    return _failure(
        operation, "invalidResponse", FailureKind.PROTOCOL, "Eventbrite returned invalid JSON"
    ).with_empty(empty)


def _is_numeric_id(value: object) -> bool:
    return isinstance(value, str) and _NUMERIC_ID.fullmatch(value) is not None


def _page_number(page: object) -> int | None:
    if type(page) is not int:
        return None
    if page == 0:
        return 1
    return page if 0 < page <= 10000 else None


class Client:
    """One redirect-safe provider transport with call-time credential resolution."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Eventbrite configuration and credential source are required")
        if type(config.max_response_bytes) is not int or config.max_response_bytes < 1:
            raise ValueError("Eventbrite response limit must be positive")
        if config.timeout_seconds <= 0:
            raise ValueError("Eventbrite timeout must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_organizations(self) -> "ListOrganizationsOperation":
        """Bind the accessible organization Query."""
        return ListOrganizationsOperation(self)

    def list_events(self) -> "ListEventsOperation":
        """Bind the organization event Query."""
        return ListEventsOperation(self)

    def get_attendee(self) -> "GetAttendeeOperation":
        """Bind the attendee metadata Query."""
        return GetAttendeeOperation(self)

    async def aclose(self) -> None:
        """Close the provider transport when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def get(self, call: Call, operation: str, path: str, has_not_found: bool) -> Any:
        """Fetch JSON or return a classified failure without exposing provider data."""
        try:
            credential = await self.credentials.resolve(call)
            token = credential.private_token.reveal()
            if not is_header_safe_credential(token):
                raise ValueError("invalid private token")
        except (ValueError, LookupError, OSError, AttributeError):
            return _failure(
                operation,
                "defect",
                FailureKind.AUTHENTICATION,
                "connection credentials are unavailable or invalid",
            )
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + path,
                headers={"Accept": "application/json", "Authorization": "Bearer " + token},
            )
        except (httpx.HTTPError, ValueError):
            return _Outcome(
                "", Failure(FailureKind.TRANSPORT, "eventbrite", operation, "Eventbrite request failed"), True
            )
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return _Outcome(
                "", Failure(FailureKind.TRANSPORT, "eventbrite", operation, "Eventbrite request failed"), True
            )
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _Outcome(
                    "",
                    Failure(
                        FailureKind.RATE_LIMIT, "eventbrite", operation, "Eventbrite rate limited the request"
                    ),
                    True,
                    retry_after,
                )
            if status == 408 or status >= 500:
                return _Outcome(
                    "",
                    Failure(
                        FailureKind.AVAILABILITY,
                        "eventbrite",
                        operation,
                        "Eventbrite is temporarily unavailable",
                    ),
                    True,
                    retry_after,
                )
            if status == 404 and has_not_found:
                return _failure(
                    operation, "notFound", FailureKind.NOT_FOUND, "Eventbrite did not find the resource"
                )
            if status != 200:
                return _failure(
                    operation,
                    "providerRejected",
                    FailureKind.PROVIDER_REJECTION,
                    f"Eventbrite rejected HTTP {status}",
                )
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _failure(
                    operation,
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Eventbrite response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _Outcome(
                    "",
                    Failure(
                        FailureKind.TRANSPORT, "eventbrite", operation, "Eventbrite response was interrupted"
                    ),
                    True,
                )
            try:
                return json.loads(body)
            except (ValueError, UnicodeDecodeError):
                return _failure(
                    operation, "invalidResponse", FailureKind.PROTOCOL, "Eventbrite returned invalid JSON"
                )
        finally:
            await response.aclose()


class ListOrganizationsOperation:
    """List organizations available to the connected account."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return LIST_ORGANIZATIONS_DEFINITION

    async def invoke(self, call: Call, input: ListOrganizationsInput) -> QueryAttempt[OrganizationsPage]:
        """Fetch one page or select a Go-compatible optional branch."""
        empty = OrganizationsPage()
        page = _page_number(input.page) if isinstance(input, ListOrganizationsInput) else None
        if page is None:
            return _failure(
                "listOrganizations", "defect", FailureKind.VALIDATION, "page must be between 1 and 10000"
            ).with_empty(empty)
        response = await self.client.get(
            call, "listOrganizations", f"/users/me/organizations/?page={page}", False
        )
        if isinstance(response, _Outcome):
            return response.with_empty(empty)
        if not isinstance(response, dict) or not isinstance(response.get("organizations"), list):
            return _invalid("listOrganizations", empty)
        pagination = _pagination(response)
        if pagination is None:
            return _invalid("listOrganizations", empty)
        organizations = response["organizations"]
        if any(
            not isinstance(item, dict)
            or not _is_numeric_id(item.get("id"))
            or not isinstance(item.get("name", ""), str)
            for item in organizations
        ):
            return _invalid("listOrganizations", empty)
        output = OrganizationsPage(
            tuple(OrganizationMetadata(item["id"], item.get("name", "")) for item in organizations),
            *pagination,
        )
        return QueryAttempt.branch_result("listed", output, Receipt())


class ListEventsOperation:
    """List events for one organization."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return LIST_EVENTS_DEFINITION

    async def invoke(self, call: Call, input: ListEventsInput) -> QueryAttempt[EventsPage]:
        """Fetch one page after validating the organization ID."""
        empty = EventsPage()
        page = _page_number(input.page) if isinstance(input, ListEventsInput) else None
        if page is None or not _is_numeric_id(input.organization_id):
            return _failure(
                "listEvents", "defect", FailureKind.VALIDATION, "organization ID or page is invalid"
            ).with_empty(empty)
        response = await self.client.get(
            call, "listEvents", f"/organizations/{input.organization_id}/events/?page={page}", True
        )
        if isinstance(response, _Outcome):
            return response.with_empty(empty)
        if not isinstance(response, dict) or not isinstance(response.get("events"), list):
            return _invalid("listEvents", empty)
        pagination = _pagination(response)
        if pagination is None:
            return _invalid("listEvents", empty)
        events = response["events"]
        if any(not _valid_event(item) for item in events):
            return _invalid("listEvents", empty)
        output = EventsPage(
            tuple(
                EventMetadata(item["id"], item.get("name", {}).get("text", ""), item.get("status", ""))
                for item in events
            ),
            *pagination,
        )
        return QueryAttempt.branch_result("listed", output, Receipt())


class GetAttendeeOperation:
    """Read attendee identity and status without private profile data."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return GET_ATTENDEE_DEFINITION

    async def invoke(self, call: Call, input: GetAttendeeInput) -> QueryAttempt[AttendeeMetadata]:
        """Fetch the selected attendee after validating both numeric IDs."""
        empty = AttendeeMetadata()
        if (
            not isinstance(input, GetAttendeeInput)
            or not _is_numeric_id(input.event_id)
            or not _is_numeric_id(input.attendee_id)
        ):
            return _failure(
                "getAttendee", "defect", FailureKind.VALIDATION, "event ID or attendee ID is invalid"
            ).with_empty(empty)
        response = await self.client.get(
            call, "getAttendee", f"/events/{input.event_id}/attendees/{input.attendee_id}/", True
        )
        if isinstance(response, _Outcome):
            return response.with_empty(empty)
        if (
            not isinstance(response, dict)
            or not _is_numeric_id(response.get("id"))
            or not _is_numeric_id(response.get("event_id"))
        ):
            return _invalid("getAttendee", empty)
        if not isinstance(response.get("status", ""), str) or not isinstance(
            response.get("created", ""), str
        ):
            return _invalid("getAttendee", empty)
        output = AttendeeMetadata(
            response["id"], response["event_id"], response.get("status", ""), response.get("created", "")
        )
        return QueryAttempt.branch_result("found", output, Receipt())


def _pagination(response: dict[str, Any]) -> tuple[int, int] | None:
    pagination = response.get("pagination")
    if not isinstance(pagination, dict):
        return None
    number = pagination.get("page_number")
    count = pagination.get("page_count")
    if type(number) is not int or number <= 0 or type(count) is not int or count < 0:
        return None
    return number, count


def _valid_event(item: object) -> bool:
    if not isinstance(item, dict) or not _is_numeric_id(item.get("id")):
        return False
    name = item.get("name", {})
    return (
        isinstance(name, dict)
        and isinstance(name.get("text", ""), str)
        and isinstance(item.get("status", ""), str)
    )
