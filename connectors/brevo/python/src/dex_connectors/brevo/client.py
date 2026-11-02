# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Brevo contact upserts and lookups with Go-compatible classification."""

import json
import re
from dataclasses import dataclass as dataclass
from datetime import UTC, datetime
from datetime import timedelta as timedelta
from typing import Any
from urllib.parse import quote

import httpx
from dex_connectors.sdk import BranchDefinition as BranchDefinition
from dex_connectors.sdk import (
    Call,
    CallID,
    CredentialProvider,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
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
    CREATE_OR_UPDATE_CONTACT_DEFINITION as CREATE_OR_UPDATE_CONTACT_DEFINITION,
)
from ._generated.definitions import (
    CREATE_OR_UPDATE_CONTACT_OPERATION_ID as CREATE_OR_UPDATE_CONTACT_OPERATION_ID,
)
from ._generated.definitions import (
    DEFECT as DEFECT,
)
from ._generated.definitions import (
    FOUND as FOUND,
)
from ._generated.definitions import (
    GET_CONTACT_DEFINITION as GET_CONTACT_DEFINITION,
)
from ._generated.definitions import (
    GET_CONTACT_OPERATION_ID as GET_CONTACT_OPERATION_ID,
)
from ._generated.definitions import (
    INVALID_RESPONSE as INVALID_RESPONSE,
)
from ._generated.definitions import (
    NOT_FOUND as NOT_FOUND,
)
from ._generated.definitions import (
    PROVIDER_REJECTED as PROVIDER_REJECTED,
)
from ._generated.definitions import (
    SAVED as SAVED,
)
from .models import Contact, ContactWriteResult, CreateOrUpdateContactInput, GetContactInput

_DEFAULT_BASE_URL = "https://api.brevo.com/v3"
_DEFAULT_TIMEOUT_SECONDS = 15
_DEFAULT_MAX_RESPONSE_BYTES = 2_097_152
_MAX_PAYLOAD_BYTES = 1 << 20
_EMAIL = re.compile(r"[A-Za-z0-9!#$%&'*+/=?^_`{|}~.-]+@[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?")


class Client:
    """One redirect-safe HTTP client and call-time credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None:
            raise ValueError("Brevo configuration is required")
        if credentials is None:
            raise ValueError("Brevo credential source is required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("Brevo timeout and maxResponseBytes must be positive")
        self.base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL)
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def create_or_update_contact(self) -> "CreateOrUpdateContactOperation":
        """Bind the contact upsert Mutation to this client."""
        return CreateOrUpdateContactOperation(self)

    def get_contact(self) -> "GetContactOperation":
        """Bind the contact Query to this client."""
        return GetContactOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def build_request(
        self, call: Call, operation: str, method: str, path: str, body: bytes | None
    ) -> httpx.Request | Failure:
        """Build one authenticated JSON request, or return the defect Failure."""
        try:
            credentials = await self.credentials.resolve(call)
        except (ValueError, LookupError, OSError, ReauthorizationRequiredError):
            credentials = None
        key = "" if credentials is None else credentials.api_key.reveal()
        if not key or not is_header_safe_credential(key):
            return _failure(
                operation,
                FailureKind.AUTHENTICATION,
                "Brevo connection credentials are unavailable or invalid",
            )
        try:
            return self.http_client.build_request(
                method,
                self.base_url + path,
                content=body,
                headers={"api-key": key, "Accept": "application/json", "Content-Type": "application/json"},
            )
        except (httpx.HTTPError, ValueError):
            return _failure(operation, FailureKind.LOCAL_DEFECT, "Brevo request could not be built")


class CreateOrUpdateContactOperation:
    """Create a contact or update it by email; the upsert makes repetition safe."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Brevo client is required")
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return CREATE_OR_UPDATE_CONTACT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreateOrUpdateContactInput) -> IdempotencyKey:
        """Use the stable call ID; Brevo receives no key because the write is an upsert."""
        return IdempotencyKey(call_id)

    async def invoke(
        self, call: Call, input: CreateOrUpdateContactInput
    ) -> MutationAttempt[ContactWriteResult]:
        """Validate, send one upsert, then select a branch or safe retry."""
        operation = CREATE_OR_UPDATE_CONTACT_OPERATION_ID
        if not isinstance(input, CreateOrUpdateContactInput) or not is_plain_email(input.email):
            return _mutation_branch(DEFECT, FailureKind.VALIDATION, "a plain email address is required")
        attributes = input.attributes or {}
        if not isinstance(attributes, dict) or any(
            not isinstance(name, str) or not name or name.upper() != name or not isinstance(value, str)
            for name, value in attributes.items()
        ):
            return _mutation_branch(DEFECT, FailureKind.VALIDATION, "attribute names must be uppercase")
        document: dict[str, Any] = {"email": input.email}
        if attributes:
            document["attributes"] = dict(attributes)
        document["updateEnabled"] = True
        body = json.dumps(document).encode()
        if len(body) > _MAX_PAYLOAD_BYTES:
            return _mutation_branch(DEFECT, FailureKind.VALIDATION, "contact payload is invalid or too large")
        request = await self.client.build_request(call, operation, "POST", "/contacts", body)
        if isinstance(request, Failure):
            return MutationAttempt.branch_result(DEFECT, ContactWriteResult(), Receipt(), request)
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return MutationAttempt.retry(
                _failure(operation, FailureKind.TRANSPORT, "Brevo contact response is unavailable")
            )
        try:
            status = response.status_code
            if _is_retry_status(status):
                return MutationAttempt.retry(
                    _failure(
                        operation,
                        FailureKind.AVAILABILITY,
                        "Brevo contact request is temporarily unavailable",
                    ),
                    _retry_after_seconds(response),
                )
            if status not in (201, 204):
                return _mutation_branch(
                    PROVIDER_REJECTED,
                    _rejection_kind(status),
                    f"Brevo rejected contact request with HTTP {status}",
                )
            if status == 204:
                return MutationAttempt.branch_result(SAVED, ContactWriteResult(input.email, False), Receipt())
            try:
                content = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _mutation_branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Brevo contact response exceeded the limit",
                )
            except ProviderTransportError:
                return MutationAttempt.retry(
                    _failure(operation, FailureKind.AVAILABILITY, "Brevo contact response could not be read")
                )
            contact_id = _created_id(content)
            if contact_id is None:
                return _mutation_branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL, "Brevo returned an invalid contact ID"
                )
            return MutationAttempt.branch_result(
                SAVED, ContactWriteResult(input.email, True, contact_id), Receipt()
            )
        finally:
            await response.aclose()


class GetContactOperation:
    """Read one Brevo contact by email."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Brevo client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return GET_CONTACT_DEFINITION

    async def invoke(self, call: Call, input: GetContactInput) -> QueryAttempt[Contact]:
        """Validate the email, read once, then select a branch or safe retry."""
        operation = GET_CONTACT_OPERATION_ID
        if not isinstance(input, GetContactInput) or not is_plain_email(input.email):
            return _query_branch(DEFECT, FailureKind.VALIDATION, "a plain email address is required")
        request = await self.client.build_request(
            call, operation, "GET", "/contacts/" + quote(input.email, safe=""), None
        )
        if isinstance(request, Failure):
            return QueryAttempt.branch_result(DEFECT, Contact(), Receipt(), request)
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return QueryAttempt.retry(
                _failure(operation, FailureKind.TRANSPORT, "Brevo contact response is unavailable")
            )
        try:
            status = response.status_code
            if _is_retry_status(status):
                return QueryAttempt.retry(
                    _failure(
                        operation, FailureKind.AVAILABILITY, "Brevo contact lookup is temporarily unavailable"
                    ),
                    _retry_after_seconds(response),
                )
            if status == 404:
                return _query_branch(NOT_FOUND, FailureKind.NOT_FOUND, "Brevo contact was not found")
            if status != 200:
                return _query_branch(
                    PROVIDER_REJECTED,
                    _rejection_kind(status),
                    f"Brevo rejected contact lookup with HTTP {status}",
                )
            try:
                content = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _query_branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Brevo contact response exceeded the limit",
                )
            except ProviderTransportError:
                return QueryAttempt.retry(
                    _failure(operation, FailureKind.AVAILABILITY, "Brevo contact response could not be read")
                )
            contact = _decode_contact(content)
            if contact is None:
                return _query_branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL, "Brevo returned an invalid contact"
                )
            return QueryAttempt.branch_result(FOUND, contact, Receipt())
        finally:
            await response.aclose()


def is_plain_email(value: object) -> bool:
    """Accept a bare address without a display name, as the Go connector's net/mail check does."""
    return isinstance(value, str) and _EMAIL.fullmatch(value) is not None


def _created_id(content: bytes) -> int | None:
    try:
        document = json.loads(content)
    except (ValueError, UnicodeDecodeError):
        return None
    contact_id = document.get("id") if isinstance(document, dict) else None
    return contact_id if type(contact_id) is int and contact_id > 0 else None


def _decode_contact(content: bytes) -> Contact | None:
    try:
        document = json.loads(content)
    except (ValueError, UnicodeDecodeError):
        return None
    if not isinstance(document, dict):
        return None
    contact_id = document.get("id")
    email = document.get("email")
    if type(contact_id) is not int or contact_id <= 0 or not is_plain_email(email):
        return None
    return Contact(contact_id, str(email))


def _is_retry_status(status: int) -> bool:
    return status in (408, 409, 429) or status >= 500


def _rejection_kind(status: int) -> FailureKind:
    if status == 401:
        return FailureKind.AUTHENTICATION
    if status == 403:
        return FailureKind.AUTHORIZATION
    return FailureKind.PROVIDER_REJECTION


def _retry_after_seconds(response: httpx.Response) -> float:
    return parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, CONNECTOR_ID, operation, message)


def _query_branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[Contact]:
    return QueryAttempt.branch_result(
        branch, Contact(), Receipt(), _failure(GET_CONTACT_OPERATION_ID, kind, message)
    )


def _mutation_branch(branch: str, kind: FailureKind, message: str) -> MutationAttempt[ContactWriteResult]:
    return MutationAttempt.branch_result(
        branch,
        ContactWriteResult(),
        Receipt(),
        _failure(CREATE_OR_UPDATE_CONTACT_OPERATION_ID, kind, message),
    )


Config.__doc__ = "Brevo HTTP settings; zero or blank values use manifest defaults."
Credentials.__doc__ = "Brevo API key, resolved anew for every provider attempt."
