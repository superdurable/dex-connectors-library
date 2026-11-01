# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Resend provider calls and Go-compatible branch classification."""

import json
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Any, TypeVar
from urllib.parse import quote

import httpx
from dex_connectors.sdk import (
    BranchDefinition,
    Call,
    CallID,
    CredentialProvider,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    OperationRef,
    QueryAttempt,
    QueryDefinition,
    ReauthorizationRequiredError,
    Receipt,
    SecretString,
    StepDefaults,
)
from dex_connectors.sdk.providerhttp import (
    MAX_ERROR_BODY_BYTES,
    BodyTooLargeError,
    ProviderTransportError,
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    read_error_tokens,
    send_traced,
    validate_base_url,
)
from dex_connectors.sdk.retry_policy import default_connector_step_execute_retry_policy

from .models import Email, GetEmailInput, SendEmailInput, SentEmail

CONNECTOR_ID = "resend"
SEND_EMAIL_OPERATION_ID = "sendEmail"
GET_EMAIL_OPERATION_ID = "getEmail"
SENT = "sent"
FOUND = "found"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
INVALID_RESPONSE = "invalidResponse"
DEFECT = "defect"
_DEFAULT_BASE_URL = "https://api.resend.com"
_DEFAULT_TIMEOUT_SECONDS = 15
_DEFAULT_MAX_RESPONSE_BYTES = 2_097_152
_STEP_DEFAULTS = StepDefaults(
    timedelta(seconds=30), 2, execute_retry=default_connector_step_execute_retry_policy()
)
_PROVIDER_REJECTED_BRANCH = BranchDefinition(
    PROVIDER_REJECTED, "The provider definitively rejected the request.", True
)
_INVALID_RESPONSE_BRANCH = BranchDefinition(
    INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True
)
_DEFECT_BRANCH = BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True)
SEND_EMAIL_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, SEND_EMAIL_OPERATION_ID),
    (
        BranchDefinition(SENT, "Resend returned the requested result."),
        _PROVIDER_REJECTED_BRANCH,
        _INVALID_RESPONSE_BRANCH,
        _DEFECT_BRANCH,
    ),
    _STEP_DEFAULTS,
)
GET_EMAIL_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_EMAIL_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Resend returned the requested result."),
        BranchDefinition(NOT_FOUND, "The requested resource does not exist.", True),
        _PROVIDER_REJECTED_BRANCH,
        _INVALID_RESPONSE_BRANCH,
        _DEFECT_BRANCH,
    ),
    _STEP_DEFAULTS,
)

Output = TypeVar("Output")


@dataclass(frozen=True, slots=True)
class Config:
    """Resend HTTP settings; zero or blank values use manifest defaults."""

    base_url: str = _DEFAULT_BASE_URL
    timeout_seconds: float = _DEFAULT_TIMEOUT_SECONDS
    max_response_bytes: int = _DEFAULT_MAX_RESPONSE_BYTES


@dataclass(frozen=True, slots=True)
class Credentials:
    """Resend API key, resolved anew for every provider attempt."""

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
            raise ValueError("Resend configuration is required")
        if credentials is None:
            raise ValueError("Resend credential source is required")
        self.base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL)
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("Resend timeout and max response bytes must be positive")
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def send_email(self) -> "SendEmailOperation":
        """Bind the keyed send Mutation to this client."""
        return SendEmailOperation(self)

    def get_email(self) -> "GetEmailOperation":
        """Bind the email lookup Query to this client."""
        return GetEmailOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def _send(
        self, call: Call, method: str, path: str, body: bytes | None, extra_headers: dict[str, str]
    ) -> httpx.Response | str:
        """Send one request, or return the defect message when it cannot be built."""
        try:
            credentials = await self.credentials.resolve(call)
        except (ValueError, LookupError, OSError, ReauthorizationRequiredError):
            return "connection credentials are unavailable"
        key = credentials.api_key.reveal()
        if not key.strip() or not is_header_safe_credential(key):
            return "connection credentials are invalid"
        headers = {
            "Authorization": "Bearer " + key,
            "Accept": "application/json",
            "User-Agent": "superdurable-resend/0.1",
            **extra_headers,
        }
        if body is not None:
            headers["Content-Type"] = "application/json"
        try:
            request = self.http_client.build_request(
                method, self.base_url + path, content=body, headers=headers
            )
        except (httpx.HTTPError, ValueError):
            return "Resend request could not be built"
        response, _ = await send_traced(self.http_client, request)
        return response


class SendEmailOperation:
    """Send one email keyed by the stable Dex call idempotency key."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Resend client is required")
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return SEND_EMAIL_DEFINITION

    def idempotency_key(self, call_id: CallID, input: SendEmailInput) -> IdempotencyKey:
        """Use the stable connector call ID as Resend's Idempotency-Key."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: SendEmailInput) -> MutationAttempt[SentEmail]:
        """Validate, send once with the idempotency key, then select a branch or retry."""
        operation = SEND_EMAIL_OPERATION_ID
        empty = SentEmail()
        if (
            not isinstance(input, SendEmailInput)
            or any(not isinstance(value, str) for value in (input.from_address, input.subject, input.html))
            or not isinstance(input.to, (tuple, list))
            or any(not isinstance(recipient, str) for recipient in input.to)
        ):
            return _mutation_branch(
                operation, DEFECT, empty, FailureKind.VALIDATION, "email input is invalid"
            )
        if not input.from_address or not input.to or not input.subject or not input.html:
            return _mutation_branch(
                operation, DEFECT, empty, FailureKind.VALIDATION, "from, to, subject, and html are required"
            )
        if any(not recipient for recipient in input.to):
            return _mutation_branch(
                operation, DEFECT, empty, FailureKind.VALIDATION, "recipient cannot be blank"
            )
        key = str(call.idempotency_key)
        if not key or len(key) > 256:
            return _mutation_branch(
                operation, DEFECT, empty, FailureKind.VALIDATION, "idempotency key is invalid"
            )
        body = json.dumps(
            {"from": input.from_address, "to": list(input.to), "subject": input.subject, "html": input.html}
        ).encode()
        try:
            response = await self.client._send(call, "POST", "/emails", body, {"Idempotency-Key": key})
        except ProviderTransportError:
            return MutationAttempt.retry(
                _failure(operation, FailureKind.AVAILABILITY, "provider request failed")
            )
        if isinstance(response, str):
            return _mutation_branch(operation, DEFECT, empty, FailureKind.AUTHENTICATION, response)
        try:
            status = response.status_code
            if status == 429 or status == 408 or status >= 500:
                kind = FailureKind.RATE_LIMIT if status == 429 else FailureKind.AVAILABILITY
                return MutationAttempt.retry(
                    _failure(operation, kind, _retry_message(status)), _retry_after_seconds(response)
                )
            if status == 409:
                try:
                    conflict = await read_bounded_body(response, MAX_ERROR_BODY_BYTES)
                except (BodyTooLargeError, ProviderTransportError):
                    conflict = b""
                if read_error_tokens(conflict, ("/name",)) == ("invalid_idempotent_request",):
                    return _mutation_branch(
                        operation,
                        PROVIDER_REJECTED,
                        empty,
                        FailureKind.PROVIDER_REJECTION,
                        "idempotency key was used for a different request",
                    )
                return MutationAttempt.retry(
                    _failure(operation, FailureKind.AVAILABILITY, "provider is processing the same request")
                )
            if status not in (200, 201, 202):
                return _mutation_branch(
                    operation,
                    PROVIDER_REJECTED,
                    empty,
                    FailureKind.PROVIDER_REJECTION,
                    f"provider rejected the request with HTTP {status}",
                )
            content = await _read_success_body(self.client, response)
            if isinstance(content, FailureKind):
                if content == FailureKind.AVAILABILITY:
                    return MutationAttempt.retry(
                        _failure(operation, content, "provider response was interrupted")
                    )
                return _mutation_branch(
                    operation, INVALID_RESPONSE, empty, content, _invalid_message(content)
                )
            try:
                sent = SentEmail(_required_string(_decode_object(content), "id"))
            except (ValueError, TypeError, UnicodeDecodeError):
                return _mutation_branch(
                    operation,
                    INVALID_RESPONSE,
                    empty,
                    FailureKind.PROTOCOL,
                    _invalid_message(FailureKind.PROTOCOL),
                )
            return MutationAttempt.branch_result(SENT, sent, Receipt())
        finally:
            await response.aclose()


class GetEmailOperation:
    """Read one stored Resend email by identifier."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Resend client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return GET_EMAIL_DEFINITION

    async def invoke(self, call: Call, input: GetEmailInput) -> QueryAttempt[Email]:
        """Validate, read once, then select a branch or safe Query retry."""
        operation = GET_EMAIL_OPERATION_ID
        empty = Email()
        if not isinstance(input, GetEmailInput) or not isinstance(input.id, str) or not input.id:
            return _query_branch(operation, DEFECT, empty, FailureKind.VALIDATION, "email ID is required")
        try:
            response = await self.client._send(call, "GET", "/emails/" + quote(input.id, safe=""), None, {})
        except ProviderTransportError:
            return QueryAttempt.retry(
                _failure(operation, FailureKind.AVAILABILITY, "provider request failed")
            )
        if isinstance(response, str):
            return _query_branch(operation, DEFECT, empty, FailureKind.AUTHENTICATION, response)
        try:
            status = response.status_code
            if status == 429 or status == 408 or status >= 500:
                kind = FailureKind.RATE_LIMIT if status == 429 else FailureKind.AVAILABILITY
                return QueryAttempt.retry(
                    _failure(operation, kind, _retry_message(status)), _retry_after_seconds(response)
                )
            if status == 404:
                return _query_branch(
                    operation, NOT_FOUND, empty, FailureKind.NOT_FOUND, "provider resource was not found"
                )
            if status != 200:
                return _query_branch(
                    operation,
                    PROVIDER_REJECTED,
                    empty,
                    FailureKind.PROVIDER_REJECTION,
                    f"provider rejected the request with HTTP {status}",
                )
            content = await _read_success_body(self.client, response)
            if isinstance(content, FailureKind):
                if content == FailureKind.AVAILABILITY:
                    return QueryAttempt.retry(
                        _failure(operation, content, "provider response was interrupted")
                    )
                return _query_branch(operation, INVALID_RESPONSE, empty, content, _invalid_message(content))
            try:
                email = _decode_email(_decode_object(content))
            except (ValueError, TypeError, UnicodeDecodeError):
                return _query_branch(
                    operation,
                    INVALID_RESPONSE,
                    empty,
                    FailureKind.PROTOCOL,
                    _invalid_message(FailureKind.PROTOCOL),
                )
            return QueryAttempt.branch_result(FOUND, email, Receipt())
        finally:
            await response.aclose()


async def _read_success_body(client: Client, response: httpx.Response) -> bytes | FailureKind:
    try:
        return await read_bounded_body(response, client.max_response_bytes)
    except BodyTooLargeError:
        return FailureKind.RESPONSE_TOO_LARGE
    except ProviderTransportError:
        return FailureKind.AVAILABILITY


def _retry_after_seconds(response: httpx.Response) -> float:
    return parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))


def _retry_message(status: int) -> str:
    if status == 429:
        return "provider rate limited the request"
    return "provider is temporarily unavailable"


def _invalid_message(kind: FailureKind) -> str:
    if kind == FailureKind.RESPONSE_TOO_LARGE:
        return "provider response exceeded the configured limit"
    return "provider response is incomplete or malformed"


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, CONNECTOR_ID, operation, message)


def _query_branch(
    operation: str, branch: str, value: Output, kind: FailureKind, message: str
) -> QueryAttempt[Output]:
    return QueryAttempt.branch_result(branch, value, Receipt(), _failure(operation, kind, message))


def _mutation_branch(
    operation: str, branch: str, value: Output, kind: FailureKind, message: str
) -> MutationAttempt[Output]:
    return MutationAttempt.branch_result(branch, value, Receipt(), _failure(operation, kind, message))


def _decode_object(content: bytes) -> dict[str, Any]:
    document = json.loads(content)
    if not isinstance(document, dict):
        raise ValueError
    return document


def _decode_email(document: dict[str, Any]) -> Email:
    recipients = document.get("to")
    if recipients is None:
        recipients = []
    if not isinstance(recipients, list) or any(not isinstance(value, str) for value in recipients):
        raise ValueError
    return Email(
        _required_string(document, "id"),
        _optional_string(document, "from"),
        tuple(recipients),
        _optional_string(document, "subject"),
        _optional_string(document, "created_at"),
    )


def _required_string(document: dict[str, Any], name: str) -> str:
    value = _optional_string(document, name)
    if not value:
        raise ValueError
    return value


def _optional_string(document: dict[str, Any], name: str) -> str:
    value = document.get(name)
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError
    return value
