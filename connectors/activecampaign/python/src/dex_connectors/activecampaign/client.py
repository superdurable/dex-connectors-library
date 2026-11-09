# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Authenticated ActiveCampaign reads with Go-compatible decisions."""

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

_NUMERIC_ID = re.compile(r"[0-9]{1,24}\Z")
_COUNT = re.compile(r"\+?[0-9]+\Z")
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
    return _Outcome(branch, Failure(kind, "activecampaign", operation, message))


def _invalid(operation: str, empty: Output) -> QueryAttempt[Output]:
    return _failure(
        operation, "invalidResponse", FailureKind.PROTOCOL, "ActiveCampaign returned invalid JSON"
    ).with_empty(empty)


def _is_numeric_id(value: object) -> bool:
    return isinstance(value, str) and _NUMERIC_ID.fullmatch(value) is not None


def _offset_and_limit(input: ListContactsInput) -> tuple[int, int] | None:
    if type(input.offset) is not int or type(input.limit) is not int:
        return None
    if input.offset < 0 or input.limit < 0 or input.limit > 100:
        return None
    return input.offset, input.limit or 20


class Client:
    """One redirect-safe provider transport with call-time credential resolution."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("ActiveCampaign configuration and credential source are required")
        if type(config.max_response_bytes) is not int or config.max_response_bytes < 1:
            raise ValueError("ActiveCampaign response limit must be positive")
        if config.timeout_seconds <= 0:
            raise ValueError("ActiveCampaign timeout must be positive")
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
                headers={"Accept": "application/json", "Api-Token": token},
            )
        except (httpx.HTTPError, ValueError):
            return _Outcome(
                "",
                Failure(FailureKind.TRANSPORT, "activecampaign", operation, "ActiveCampaign request failed"),
                True,
            )
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return _Outcome(
                "",
                Failure(FailureKind.TRANSPORT, "activecampaign", operation, "ActiveCampaign request failed"),
                True,
            )
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _Outcome(
                    "",
                    Failure(
                        FailureKind.RATE_LIMIT,
                        "activecampaign",
                        operation,
                        "ActiveCampaign rate limited the request",
                    ),
                    True,
                    retry_after,
                )
            if status == 408 or status >= 500:
                return _Outcome(
                    "",
                    Failure(
                        FailureKind.AVAILABILITY,
                        "activecampaign",
                        operation,
                        "ActiveCampaign is temporarily unavailable",
                    ),
                    True,
                    retry_after,
                )
            if status == 404 and has_not_found:
                return _failure(
                    operation, "notFound", FailureKind.NOT_FOUND, "ActiveCampaign did not find the resource"
                )
            if status != 200:
                return _failure(
                    operation,
                    "providerRejected",
                    FailureKind.PROVIDER_REJECTION,
                    f"ActiveCampaign rejected HTTP {status}",
                )
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _failure(
                    operation,
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "ActiveCampaign response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _Outcome(
                    "",
                    Failure(
                        FailureKind.TRANSPORT,
                        "activecampaign",
                        operation,
                        "ActiveCampaign response was interrupted",
                    ),
                    True,
                )
            try:
                return json.loads(body)
            except (ValueError, UnicodeDecodeError):
                return _failure(
                    operation, "invalidResponse", FailureKind.PROTOCOL, "ActiveCampaign returned invalid JSON"
                )
        finally:
            await response.aclose()


class ListContactsOperation:
    """List one offset-based page of contacts."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return LIST_CONTACTS_DEFINITION

    async def invoke(self, call: Call, input: ListContactsInput) -> QueryAttempt[ContactsPage]:
        """Fetch one contact page or select a Go-compatible optional branch."""
        empty = ContactsPage()
        pagination = _offset_and_limit(input) if isinstance(input, ListContactsInput) else None
        if pagination is None:
            return _failure(
                "listContacts", "defect", FailureKind.VALIDATION, "offset or limit is invalid"
            ).with_empty(empty)
        offset, limit = pagination
        response = await self.client.get(
            call, "listContacts", f"/contacts?limit={limit}&offset={offset}", False
        )
        if isinstance(response, _Outcome):
            return response.with_empty(empty)
        if not isinstance(response, dict):
            return _invalid("listContacts", empty)
        contacts = response.get("contacts")
        meta = response.get("meta")
        total = meta.get("total") if isinstance(meta, dict) else None
        if (
            not isinstance(contacts, list)
            or not isinstance(total, str)
            or _COUNT.fullmatch(total) is None
            or int(total) > (1 << 63) - 1
        ):
            return _invalid("listContacts", empty)
        if any(not _valid_contact(contact) for contact in contacts):
            return _invalid("listContacts", empty)
        output = ContactsPage(
            tuple(_contact_metadata(contact) for contact in contacts), offset, limit, int(total)
        )
        return QueryAttempt.branch_result("listed", output, Receipt())


class GetContactOperation:
    """Read one contact without embedded relationship records."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return GET_CONTACT_DEFINITION

    async def invoke(self, call: Call, input: GetContactInput) -> QueryAttempt[ContactMetadata]:
        """Fetch contact metadata after validating its numeric ID."""
        empty = ContactMetadata()
        if not isinstance(input, GetContactInput) or not _is_numeric_id(input.contact_id):
            return _failure(
                "getContact", "defect", FailureKind.VALIDATION, "contact ID must be numeric"
            ).with_empty(empty)
        response = await self.client.get(call, "getContact", f"/contacts/{input.contact_id}", True)
        if isinstance(response, _Outcome):
            return response.with_empty(empty)
        contact = response.get("contact") if isinstance(response, dict) else None
        if not _valid_contact(contact):
            return _invalid("getContact", empty)
        return QueryAttempt.branch_result("found", _contact_metadata(contact), Receipt())


def _valid_contact(value: object) -> TypeGuard[dict[str, Any]]:
    return (
        isinstance(value, dict)
        and _is_numeric_id(value.get("id"))
        and all(
            field not in value or isinstance(value[field], str)
            for field in ("email", "firstName", "lastName")
        )
    )


def _contact_metadata(value: dict[str, Any]) -> ContactMetadata:
    return ContactMetadata(
        value["id"],
        value.get("email", ""),
        value.get("firstName", ""),
        value.get("lastName", ""),
    )
