# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Authenticated GetResponse reads with Go-compatible decisions."""

import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any, TypeGuard, TypeVar

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
from ._generated.definitions import GET_CONTACT_DEFINITION, LIST_CONTACTS_DEFINITION
from .models import ContactMetadata, ContactsPage, GetContactInput, ListContactsInput

_CONTACT_ID = re.compile(r"[A-Za-z0-9]{1,64}\Z")
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
    return _Outcome(branch, Failure(kind, "getresponse", operation, message))


def _invalid(operation: str, empty: Output) -> QueryAttempt[Output]:
    return _failure(
        operation, "invalidResponse", FailureKind.PROTOCOL, "GetResponse returned invalid JSON"
    ).with_empty(empty)


def _is_contact_id(value: object) -> bool:
    return isinstance(value, str) and _CONTACT_ID.fullmatch(value) is not None


def _page_and_size(input: ListContactsInput) -> tuple[int, int] | None:
    if type(input.page) is not int or type(input.per_page) is not int:
        return None
    if input.page < 0 or input.per_page < 0 or input.per_page > 1000:
        return None
    return input.page or 1, input.per_page or 20


class Client:
    """One redirect-safe provider transport with call-time credential resolution."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("GetResponse configuration and credential source are required")
        if type(config.max_response_bytes) is not int or config.max_response_bytes < 1:
            raise ValueError("GetResponse response limit must be positive")
        if config.timeout_seconds <= 0:
            raise ValueError("GetResponse timeout must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_contacts(self) -> "ListContactsOperation":
        """Bind the contact page Query."""
        return ListContactsOperation(self)

    def get_contact(self) -> "GetContactOperation":
        """Bind the contact metadata Query."""
        return GetContactOperation(self)

    async def aclose(self) -> None:
        """Close the provider transport when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def get(self, call: Call, operation: str, path: str, has_not_found: bool) -> Any:
        """Fetch JSON or return a classified failure without exposing provider data."""
        try:
            credential = await self.credentials.resolve(call)
            token = credential.api_key.reveal()
            if not is_header_safe_credential(token):
                raise ValueError("invalid API key")
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
                headers={"Accept": "application/json", "X-Auth-Token": f"api-key {token}"},
            )
        except (httpx.HTTPError, ValueError):
            return _Outcome(
                "",
                Failure(FailureKind.TRANSPORT, "getresponse", operation, "GetResponse request failed"),
                True,
            )
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return _Outcome(
                "",
                Failure(FailureKind.TRANSPORT, "getresponse", operation, "GetResponse request failed"),
                True,
            )
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429 and retry_after == 0:
                reset = response.headers.get("X-RateLimit-Reset", "")
                if reset.isascii() and reset.isdecimal() and int(reset) > 0:
                    retry_after = int(reset)
            if status == 429:
                return _Outcome(
                    "",
                    Failure(
                        FailureKind.RATE_LIMIT,
                        "getresponse",
                        operation,
                        "GetResponse rate limited the request",
                    ),
                    True,
                    retry_after,
                )
            if status == 408 or status >= 500:
                return _Outcome(
                    "",
                    Failure(
                        FailureKind.AVAILABILITY,
                        "getresponse",
                        operation,
                        "GetResponse is temporarily unavailable",
                    ),
                    True,
                    retry_after,
                )
            if status == 404 and has_not_found:
                return _failure(
                    operation, "notFound", FailureKind.NOT_FOUND, "GetResponse did not find the resource"
                )
            if status != 200:
                return _failure(
                    operation,
                    "providerRejected",
                    FailureKind.PROVIDER_REJECTION,
                    f"GetResponse rejected HTTP {status}",
                )
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _failure(
                    operation,
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "GetResponse response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _Outcome(
                    "",
                    Failure(
                        FailureKind.TRANSPORT,
                        "getresponse",
                        operation,
                        "GetResponse response was interrupted",
                    ),
                    True,
                )
            try:
                return json.loads(body)
            except (ValueError, UnicodeDecodeError):
                return _failure(
                    operation, "invalidResponse", FailureKind.PROTOCOL, "GetResponse returned invalid JSON"
                )
        finally:
            await response.aclose()


class ListContactsOperation:
    """List one one-based page of GetResponse contacts."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return LIST_CONTACTS_DEFINITION

    async def invoke(self, call: Call, input: ListContactsInput) -> QueryAttempt[ContactsPage]:
        """Fetch one contact page or select a Go-compatible optional branch."""
        empty = ContactsPage()
        pagination = _page_and_size(input) if isinstance(input, ListContactsInput) else None
        if pagination is None:
            return _failure(
                "listContacts", "defect", FailureKind.VALIDATION, "page or perPage is invalid"
            ).with_empty(empty)
        page, per_page = pagination
        response = await self.client.get(
            call, "listContacts", f"/contacts?page={page}&perPage={per_page}", False
        )
        if isinstance(response, _Outcome):
            return response.with_empty(empty)
        if not isinstance(response, list) or any(not _valid_contact(item) for item in response):
            return _invalid("listContacts", empty)
        output = ContactsPage(tuple(_contact_metadata(item) for item in response), page, per_page)
        return QueryAttempt.branch_result("listed", output, Receipt())


class GetContactOperation:
    """Read one contact without embedded custom fields."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return GET_CONTACT_DEFINITION

    async def invoke(self, call: Call, input: GetContactInput) -> QueryAttempt[ContactMetadata]:
        """Fetch contact metadata after validating its alphanumeric ID."""
        empty = ContactMetadata()
        if not isinstance(input, GetContactInput) or not _is_contact_id(input.contact_id):
            return _failure(
                "getContact", "defect", FailureKind.VALIDATION, "contact ID must be alphanumeric"
            ).with_empty(empty)
        response = await self.client.get(call, "getContact", f"/contacts/{input.contact_id}", True)
        if isinstance(response, _Outcome):
            return response.with_empty(empty)
        if not _valid_contact(response):
            return _invalid("getContact", empty)
        return QueryAttempt.branch_result("found", _contact_metadata(response), Receipt())


def _valid_contact(value: object) -> TypeGuard[dict[str, Any]]:
    return (
        isinstance(value, dict)
        and _is_contact_id(value.get("contactId"))
        and isinstance(value.get("email"), str)
        and bool(value["email"])
        and (value.get("name") is None or isinstance(value["name"], str))
    )


def _contact_metadata(value: dict[str, Any]) -> ContactMetadata:
    return ContactMetadata(value["contactId"], value["email"], value.get("name") or "")
