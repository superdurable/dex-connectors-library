# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Postmark provider calls and Go-compatible branch classification."""

import json
from collections.abc import Callable
from datetime import UTC, datetime, timedelta
from typing import Any, TypeVar
from urllib.parse import quote

import httpx
from dex_connectors.sdk import (
    BranchDefinition,
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    QueryAttempt,
    QueryDefinition,
    ReauthorizationRequiredError,
    Receipt,
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
    send_traced,
    validate_base_url,
)
from dex_connectors.sdk.retry_policy import default_connector_step_execute_retry_policy

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
    GET_MESSAGE_DETAILS_DEFINITION as GET_MESSAGE_DETAILS_DEFINITION,
)
from ._generated.definitions import (
    GET_MESSAGE_DETAILS_OPERATION_ID as GET_MESSAGE_DETAILS_OPERATION_ID,
)
from ._generated.definitions import (
    INVALID_RESPONSE as INVALID_RESPONSE,
)
from ._generated.definitions import (
    LIST_OUTBOUND_MESSAGES_DEFINITION as LIST_OUTBOUND_MESSAGES_DEFINITION,
)
from ._generated.definitions import (
    LIST_OUTBOUND_MESSAGES_OPERATION_ID as LIST_OUTBOUND_MESSAGES_OPERATION_ID,
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
from .models import GetMessageDetailsInput, ListOutboundMessagesInput, MessageDetails, MessagePage

_DEFAULT_BASE_URL = "https://api.postmarkapp.com"
_DEFAULT_TIMEOUT_SECONDS = 15
_DEFAULT_MAX_RESPONSE_BYTES = 2_097_152
_MESSAGE_NOT_FOUND_ERROR_CODE = 701
_STEP_DEFAULTS = StepDefaults(
    timedelta(seconds=30), 2, execute_retry=default_connector_step_execute_retry_policy()
)
_SHARED_BRANCHES = (
    BranchDefinition(PROVIDER_REJECTED, "The provider definitively rejected the request.", True),
    BranchDefinition(INVALID_RESPONSE, "The provider returned malformed or oversized JSON.", True),
    BranchDefinition(DEFECT, "Local input or connection credentials are invalid.", True),
)

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
            raise ValueError("Postmark configuration is required")
        if credentials is None:
            raise ValueError("Postmark credential source is required")
        self.base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL)
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("Postmark timeout and max response bytes must be positive")
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def list_outbound_messages(self) -> "ListOutboundMessagesOperation":
        """Bind the outbound message search Query to this client."""
        return ListOutboundMessagesOperation(self)

    def get_message_details(self) -> "GetMessageDetailsOperation":
        """Bind the message details Query to this client."""
        return GetMessageDetailsOperation(self)

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
        is_message_lookup: bool = False,
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
            "X-Postmark-Server-Token": token,
            "Accept": "application/json",
            "User-Agent": "superdurable-postmark/0.1",
        }
        try:
            request = self.http_client.build_request("GET", self.base_url + path, headers=headers)
        except (httpx.HTTPError, ValueError):
            return _branch(
                operation, DEFECT, empty, FailureKind.LOCAL_DEFECT, "Postmark request could not be built"
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
            if is_message_lookup and status == 422:
                if await _is_message_not_found(response):
                    return _branch(
                        operation, NOT_FOUND, empty, FailureKind.NOT_FOUND, "Postmark message was not found"
                    )
                return _branch(
                    operation,
                    PROVIDER_REJECTED,
                    empty,
                    FailureKind.PROVIDER_REJECTION,
                    "Postmark rejected the message request",
                )
            if status == 404 and is_message_lookup:
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


class ListOutboundMessagesOperation:
    """Search one page of Postmark outbound messages."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Postmark client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return LIST_OUTBOUND_MESSAGES_DEFINITION

    async def invoke(self, call: Call, input: ListOutboundMessagesInput) -> QueryAttempt[MessagePage]:
        """Validate the search window, read once, then select a branch or safe retry."""
        operation = LIST_OUTBOUND_MESSAGES_OPERATION_ID
        if (
            not isinstance(input, ListOutboundMessagesInput)
            or type(input.count) is not int
            or type(input.offset) is not int
        ):
            return _branch(
                operation, DEFECT, MessagePage(), FailureKind.VALIDATION, "search input is invalid"
            )
        count = input.count or 50
        if count < 1 or count > 500 or input.offset < 0 or count + input.offset > 10_000:
            return _branch(
                operation,
                DEFECT,
                MessagePage(),
                FailureKind.VALIDATION,
                "count and offset must fit Postmark's 10,000-message search window",
            )
        path = f"/messages/outbound?count={count}&offset={input.offset}"
        return await self.client._run_query(
            call, operation, path, MessagePage(), LISTED, _decode_message_page
        )


class GetMessageDetailsOperation:
    """Read one Postmark outbound message by ID."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Postmark client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return GET_MESSAGE_DETAILS_DEFINITION

    async def invoke(self, call: Call, input: GetMessageDetailsInput) -> QueryAttempt[MessageDetails]:
        """Validate the message ID, read once, then select a branch or safe retry."""
        operation = GET_MESSAGE_DETAILS_OPERATION_ID
        if (
            not isinstance(input, GetMessageDetailsInput)
            or not isinstance(input.message_id, str)
            or not input.message_id
        ):
            return _branch(
                operation, DEFECT, MessageDetails(), FailureKind.VALIDATION, "message ID is required"
            )
        path = "/messages/outbound/" + quote(input.message_id, safe="") + "/details"
        return await self.client._run_query(
            call, operation, path, MessageDetails(), FOUND, _decode_required_message, is_message_lookup=True
        )


async def _is_message_not_found(response: httpx.Response) -> bool:
    if response.headers.get("X-PM-ApiErrorCode") == str(_MESSAGE_NOT_FOUND_ERROR_CODE):
        return True
    try:
        body = await read_bounded_body(response, MAX_ERROR_BODY_BYTES)
        document = json.loads(body)
    except (BodyTooLargeError, ProviderTransportError, ValueError, UnicodeDecodeError):
        return False
    code = _field(document, "ErrorCode") if isinstance(document, dict) else None
    return type(code) is int and code == _MESSAGE_NOT_FOUND_ERROR_CODE


def _retry_after_seconds(response: httpx.Response) -> float:
    return parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, CONNECTOR_ID, operation, message)


def _branch(
    operation: str, branch: str, value: Output, kind: FailureKind, message: str
) -> QueryAttempt[Output]:
    return QueryAttempt.branch_result(branch, value, Receipt(), _failure(operation, kind, message))


def _decode_message_page(document: dict[str, Any]) -> MessagePage:
    messages = _field(document, "Messages")
    if not isinstance(messages, list):
        raise ValueError
    total = _field(document, "TotalCount")
    if total is None:
        total = 0
    if type(total) is not int:
        raise ValueError
    return MessagePage(total, tuple(_decode_message(message) for message in messages))


def _decode_required_message(document: dict[str, Any]) -> MessageDetails:
    message = _decode_message(document)
    if not message.message_id:
        raise ValueError
    return message


def _decode_message(document: Any) -> MessageDetails:
    if not isinstance(document, dict):
        raise ValueError
    return MessageDetails(
        _string(document, "MessageID"), _string(document, "Subject"), _string(document, "Status")
    )


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
