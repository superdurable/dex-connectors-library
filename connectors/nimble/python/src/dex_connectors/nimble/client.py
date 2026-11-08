# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Authenticated Nimble contact reads with Go-compatible decisions."""

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
from ._generated.definitions import GET_CONTACT_DEFINITION, LIST_CONTACT_IDS_DEFINITION
from .models import ContactIDsPage, ContactMetadata, GetContactInput, ListContactIDsInput

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
    return _Outcome(branch, Failure(kind, "nimble", operation, message))


def _invalid(operation: str, empty: Output) -> QueryAttempt[Output]:
    return _failure(
        operation, "invalidResponse", FailureKind.PROTOCOL, "Nimble returned invalid JSON"
    ).with_empty(empty)


class Client:
    """Resolve the Nimble API key per call and bound provider responses."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Nimble configuration and credential source are required")
        if type(config.max_response_bytes) is not int or config.max_response_bytes < 1:
            raise ValueError("Nimble response limit must be positive")
        if config.timeout_seconds <= 0:
            raise ValueError("Nimble timeout must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_contact_ids(self) -> "ListContactIDsOperation":
        """Bind the contact page Query."""
        return ListContactIDsOperation(self)

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
                raise ValueError("invalid Nimble API token")
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
                    "Authorization": "Bearer " + api_key,
                    "User-Agent": USER_AGENT,
                },
            )
        except (httpx.HTTPError, ValueError):
            return _Outcome(
                "", Failure(FailureKind.TRANSPORT, "nimble", operation, "Nimble request failed"), True
            )
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return _Outcome(
                "", Failure(FailureKind.TRANSPORT, "nimble", operation, "Nimble request failed"), True
            )
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _Outcome(
                    "", Failure(FailureKind.RATE_LIMIT, "nimble", operation,
                                "Nimble rate limited the request"),
                    True, retry_after,
                )
            if status == 408 or status >= 500:
                return _Outcome(
                    "", Failure(FailureKind.AVAILABILITY, "nimble", operation,
                                "Nimble is temporarily unavailable"), True, retry_after,
                )
            if status == 404 and has_not_found:
                return _failure(
                    operation, "notFound", FailureKind.NOT_FOUND, "Nimble did not find the resource"
                )
            if status != 200:
                return _failure(
                    operation, "providerRejected", FailureKind.PROVIDER_REJECTION,
                    f"Nimble rejected HTTP {status}",
                )
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _failure(
                    operation, "invalidResponse", FailureKind.RESPONSE_TOO_LARGE,
                    "Nimble response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _Outcome(
                    "", Failure(FailureKind.TRANSPORT, "nimble", operation,
                                "Nimble response was interrupted"), True,
                )
            try:
                return json.loads(body)
            except (ValueError, UnicodeDecodeError):
                return _failure(
                    operation, "invalidResponse", FailureKind.PROTOCOL, "Nimble returned invalid JSON"
                )
        finally:
            await response.aclose()


class ListContactIDsOperation:
    """List Nimble contact IDs with provider pagination metadata."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query contract."""
        return LIST_CONTACT_IDS_DEFINITION

    async def invoke(self, call: Call, input: ListContactIDsInput) -> QueryAttempt[ContactIDsPage]:
        """Read one bounded page or select a Go-compatible optional branch."""
        empty = ContactIDsPage()
        if (
            not isinstance(input, ListContactIDsInput)
            or type(input.page) is not int or input.page < 1
            or type(input.per_page) is not int or not 1 <= input.per_page <= 100
        ):
            return _failure(
                "listContactIDs", "defect", FailureKind.VALIDATION,
                "contact page number or size is invalid",
            ).with_empty(empty)
        response = await self.client.get(
            call, "listContactIDs", f"/contacts/ids?page={input.page}&per_page={input.per_page}", False
        )
        if isinstance(response, _Outcome):
            return response.with_empty(empty)
        if not isinstance(response, dict) or not isinstance(response.get("meta"), dict):
            return _invalid("listContactIDs", empty)
        metadata = response["meta"]
        resources = response.get("resources")
        if (
            not isinstance(resources, list)
            or any(not _valid_contact_id(contact_id) for contact_id in resources)
            or type(metadata.get("page")) is not int or metadata["page"] < 1
            or type(metadata.get("pages")) is not int or metadata["pages"] < 0
            or type(metadata.get("per_page")) is not int or metadata["per_page"] < 1
            or type(metadata.get("total")) is not int or metadata["total"] < 0
        ):
            return _invalid("listContactIDs", empty)
        return QueryAttempt.branch_result(
            "listed", ContactIDsPage(tuple(resources), metadata["page"], metadata["pages"],
                                     metadata["per_page"], metadata["total"]), Receipt(),
        )


class GetContactOperation:
    """Read one Nimble contact's ID, names, and email address."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query contract."""
        return GET_CONTACT_DEFINITION

    async def invoke(self, call: Call, input: GetContactInput) -> QueryAttempt[ContactMetadata]:
        """Read a contact after validating its BSON identifier."""
        empty = ContactMetadata()
        if not isinstance(input, GetContactInput) or not _valid_contact_id(input.contact_id):
            return _failure(
                "getContact", "defect", FailureKind.VALIDATION,
                "contact ID must be a 24-character hexadecimal identifier",
            ).with_empty(empty)
        response = await self.client.get(call, "getContact", f"/contact/{input.contact_id}", True)
        if isinstance(response, _Outcome):
            return response.with_empty(empty)
        if not isinstance(response, dict) or not isinstance(response.get("resources"), list):
            return _invalid("getContact", empty)
        resources = response["resources"]
        if len(resources) != 1 or not isinstance(resources[0], dict):
            return _invalid("getContact", empty)
        contact = resources[0]
        if (
            not _valid_contact_id(contact.get("id"))
            or contact["id"].lower() != input.contact_id.lower()
        ):
            return _invalid("getContact", empty)
        fields = contact.get("fields")
        if fields is not None and not isinstance(fields, dict):
            return _invalid("getContact", empty)
        fields = fields or {}
        for values in fields.values():
            if values is None:
                continue
            if not isinstance(values, list):
                return _invalid("getContact", empty)
            for value in values:
                if value is not None and (
                    not isinstance(value, dict)
                    or (value.get("value") is not None and not isinstance(value["value"], str))
                ):
                    return _invalid("getContact", empty)
        return QueryAttempt.branch_result(
            "found", ContactMetadata(contact["id"], _first_field_value(fields.get("first name")),
                                     _first_field_value(fields.get("last name")),
                                     _first_field_value(fields.get("email"))), Receipt(),
        )


def _valid_contact_id(contact_id: Any) -> bool:
    return isinstance(contact_id, str) and re.fullmatch(r"[0-9a-fA-F]{24}", contact_id) is not None


def _first_field_value(values: Any) -> str:
    if not values:
        return ""
    first = values[0]
    if first is None:
        return ""
    return first.get("value") or ""
