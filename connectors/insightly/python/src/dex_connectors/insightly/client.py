# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Authenticated Insightly contact reads with Go-compatible decisions."""

import base64
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
from ._generated.definitions import GET_CONTACT_DEFINITION, LIST_CONTACTS_DEFINITION
from .models import ContactMetadata, ContactsPage, GetContactInput, ListContactsInput

Output = TypeVar("Output")
USER_AGENT = "Dex Connector (https://superdurable.io)"


@dataclass(frozen=True, slots=True)
class _Outcome:
    """One classified provider failure or retry."""

    branch: str
    failure: Failure
    is_retry: bool = False
    retry_after_seconds: float = 0

    def with_empty(self, empty: Output) -> QueryAttempt[Output]:
        """Apply a provider decision to the operation's empty output."""
        if self.is_retry:
            return QueryAttempt.retry(self.failure, self.retry_after_seconds)
        return QueryAttempt.branch_result(self.branch, empty, Receipt(), self.failure)


def _failure(operation: str, branch: str, kind: FailureKind, message: str) -> _Outcome:
    return _Outcome(branch, Failure(kind, "insightly", operation, message))


def _invalid(operation: str, empty: Output) -> QueryAttempt[Output]:
    return _failure(
        operation, "invalidResponse", FailureKind.PROTOCOL, "Insightly returned invalid JSON"
    ).with_empty(empty)


class Client:
    """Resolve the Insightly API key per call and bound provider responses."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Insightly configuration and credential source are required")
        if type(config.max_response_bytes) is not int or config.max_response_bytes < 1:
            raise ValueError("Insightly response limit must be positive")
        if config.timeout_seconds <= 0:
            raise ValueError("Insightly timeout must be positive")
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
        """Close the transport when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def get(self, call: Call, operation: str, path: str, has_not_found: bool) -> Any:
        """Fetch JSON or classify a provider failure without retaining its body."""
        try:
            credential = await self.credentials.resolve(call)
            api_key = credential.api_key.reveal()
            if not is_header_safe_credential(api_key):
                raise ValueError("invalid Insightly API key")
        except (ValueError, LookupError, OSError, AttributeError):
            return _failure(
                operation, "defect", FailureKind.AUTHENTICATION,
                "connection credentials are unavailable or invalid",
            )
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + path,
                headers={
                    "Accept": "application/json",
                    "Authorization": "Basic " + base64.b64encode((api_key + ":").encode()).decode(),
                    "User-Agent": USER_AGENT,
                },
            )
        except (httpx.HTTPError, ValueError):
            return _Outcome(
                "", Failure(FailureKind.TRANSPORT, "insightly", operation, "Insightly request failed"), True
            )
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return _Outcome(
                "", Failure(FailureKind.TRANSPORT, "insightly", operation, "Insightly request failed"), True
            )
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _Outcome(
                    "", Failure(FailureKind.RATE_LIMIT, "insightly", operation,
                                "Insightly rate limited the request"),
                    True, retry_after,
                )
            if status == 408 or status >= 500:
                return _Outcome(
                    "", Failure(FailureKind.AVAILABILITY, "insightly", operation,
                                "Insightly is temporarily unavailable"), True, retry_after,
                )
            if status == 404 and has_not_found:
                return _failure(
                    operation, "notFound", FailureKind.NOT_FOUND, "Insightly did not find the resource"
                )
            if status != 200:
                return _failure(
                    operation, "providerRejected", FailureKind.PROVIDER_REJECTION,
                    f"Insightly rejected HTTP {status}",
                )
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _failure(
                    operation, "invalidResponse", FailureKind.RESPONSE_TOO_LARGE,
                    "Insightly response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _Outcome(
                    "", Failure(FailureKind.TRANSPORT, "insightly", operation,
                                "Insightly response was interrupted"), True,
                )
            try:
                return json.loads(body)
            except (ValueError, UnicodeDecodeError):
                return _failure(
                    operation, "invalidResponse", FailureKind.PROTOCOL, "Insightly returned invalid JSON"
                )
        finally:
            await response.aclose()


class ListContactsOperation:
    """List Insightly contact IDs, names, and email addresses."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query contract."""
        return LIST_CONTACTS_DEFINITION

    async def invoke(self, call: Call, input: ListContactsInput) -> QueryAttempt[ContactsPage]:
        """Read a bounded page or select a Go-compatible optional branch."""
        empty = ContactsPage()
        if (
            not isinstance(input, ListContactsInput)
            or type(input.top) is not int or not 1 <= input.top <= 500
            or type(input.skip) is not int or input.skip < 0
        ):
            return _failure(
                "listContacts", "defect", FailureKind.VALIDATION,
                "contact page size or offset is invalid",
            ).with_empty(empty)
        response = await self.client.get(
            call, "listContacts", f"/Contacts?skip={input.skip}&top={input.top}", False
        )
        if isinstance(response, _Outcome):
            return response.with_empty(empty)
        if not isinstance(response, list):
            return _invalid("listContacts", empty)
        contacts = []
        for item in response:
            contact = _contact_metadata(item)
            if contact is None:
                return _invalid("listContacts", empty)
            contacts.append(contact)
        return QueryAttempt.branch_result("listed", ContactsPage(tuple(contacts)), Receipt())


class GetContactOperation:
    """Read one Insightly contact's identity."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query contract."""
        return GET_CONTACT_DEFINITION

    async def invoke(self, call: Call, input: GetContactInput) -> QueryAttempt[ContactMetadata]:
        """Read a contact after validating its positive decimal ID."""
        empty = ContactMetadata()
        if not isinstance(input, GetContactInput) or not _valid_contact_id(input.contact_id):
            return _failure(
                "getContact", "defect", FailureKind.VALIDATION,
                "contact ID must be a positive decimal identifier",
            ).with_empty(empty)
        response = await self.client.get(call, "getContact", f"/Contacts/{input.contact_id}", True)
        if isinstance(response, _Outcome):
            return response.with_empty(empty)
        contact = _contact_metadata(response)
        if contact is None:
            return _invalid("getContact", empty)
        return QueryAttempt.branch_result("found", contact, Receipt())


def _valid_contact_id(contact_id: str) -> bool:
    return (
        isinstance(contact_id, str)
        and re.fullmatch(r"[0-9]+", contact_id) is not None
        and len(contact_id.lstrip("0")) <= 19
        and 0 < int(contact_id.lstrip("0") or "0") <= (1 << 63) - 1
    )


def _contact_metadata(value: Any) -> ContactMetadata | None:
    if not isinstance(value, dict) or type(value.get("CONTACT_ID")) is not int or value["CONTACT_ID"] <= 0:
        return None
    for key in ("FIRST_NAME", "LAST_NAME", "EMAIL_ADDRESS"):
        if value.get(key) is not None and not isinstance(value[key], str):
            return None
    return ContactMetadata(
        value["CONTACT_ID"], value.get("FIRST_NAME") or "", value.get("LAST_NAME") or "",
        value.get("EMAIL_ADDRESS") or "",
    )
