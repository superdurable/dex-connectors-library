# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Authenticated monday.com GraphQL transport and Go-compatible classifications."""

import json
import re
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import TYPE_CHECKING, Any

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    ReauthorizationRequiredError,
    Receipt,
    RetryError,
)
from dex_connectors.sdk.projectconfig.provider import RefreshFailedError
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

if TYPE_CHECKING:
    from .operations import (
        AddUpdateOperation,
        CreateItemOperation,
        GetItemOperation,
        ListItemsOperation,
        UpdateItemColumnValuesOperation,
    )

API_VERSION = "2026-10"
_DEFAULT_ENDPOINT = "https://api.monday.com/v2"
_REQUEST_ID = re.compile(r"[A-Za-z0-9._:-]{1,128}\Z")
_RATE_CODES = {
    "COMPLEXITY_BUDGET_EXHAUSTED",
    "ComplexityException",
    "RATE_LIMIT_EXCEEDED",
    "IP_RATE_LIMIT_EXCEEDED",
    "maxConcurrencyExceeded",
    "FIELD_LIMIT_EXCEEDED",
}
_TEMPORARY_CODES = {"API_TEMPORARILY_BLOCKED"}
_AUTHENTICATION_CODES = {"Unauthorized", "UNAUTHORIZED", "NOT_AUTHENTICATED"}
_AUTHORIZATION_CODES = {
    "UserUnauthorizedException",
    "USER_UNAUTHORIZED",
    "USER_ACCESS_DENIED",
    "missingRequiredPermissions",
}
_NOT_FOUND_CODES = {
    "ResourceNotFoundException",
    "InvalidBoardIdException",
    "InvalidItemIdException",
    "InvalidGroupIdException",
}
_DOCUMENT_CODES = {
    "JsonParseException",
    "InvalidVersionException",
    "GRAPHQL_PARSE_FAILED",
    "GRAPHQL_VALIDATION_FAILED",
    "undefinedField",
    "argumentLiteralsIncompatible",
    "missingRequiredArguments",
    "variableMismatch",
}
_VALIDATION_CODES = {
    "ColumnValueException",
    "CorrectedValueException",
    "InvalidColumnIdException",
    "InvalidUserIdException",
    "InvalidArgumentException",
    "ItemNameTooLongException",
    "RecordInvalidException",
    "ItemsLimitationException",
}


def failure(operation: str, kind: FailureKind, message: str) -> Failure:
    """Build a secret-free monday.com failure for a single operation."""
    return Failure(kind, "monday", operation, message)


@dataclass(frozen=True, slots=True)
class Exchange:
    """One classified GraphQL exchange, including its safe correlation fields."""

    outcome: str
    status: int = 0
    data: Any = None
    request_id: str = ""
    replayed: bool = False
    problem: Failure | None = None
    retry_after: float = 0
    confirmed_not_processed: bool = False


class Client:
    """Send bounded monday.com requests with call-time OAuth-aware credentials.

    The caller owns an injected HTTP client. Close a client constructed here with
    ``aclose`` after its Worker stops.
    """

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Callable[[], datetime] | None = None,
        endpoint: str = _DEFAULT_ENDPOINT,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("monday.com configuration and credential source are required")
        if type(config.max_response_bytes) is not int or config.max_response_bytes < 1:
            raise ValueError("monday.com response limit must be positive")
        self.endpoint = validate_base_url(endpoint)
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(12)
        self._owns_client = http_client is None
        self.now = now or (lambda: datetime.now(UTC))

    def list_items(self) -> "ListItemsOperation":
        """Return the board item page Query."""
        from .operations import ListItemsOperation

        return ListItemsOperation(self)

    def get_item(self) -> "GetItemOperation":
        """Return the item detail Query."""
        from .operations import GetItemOperation

        return GetItemOperation(self)

    def create_item(self) -> "CreateItemOperation":
        """Return the checkpointed item creation Mutation."""
        from .operations import CreateItemOperation

        return CreateItemOperation(self)

    def update_item_column_values(self) -> "UpdateItemColumnValuesOperation":
        """Return the absolute column replacement Mutation."""
        from .operations import UpdateItemColumnValuesOperation

        return UpdateItemColumnValuesOperation(self)

    def add_update(self) -> "AddUpdateOperation":
        """Return the checkpointed item comment Mutation."""
        from .operations import AddUpdateOperation

        return AddUpdateOperation(self)

    async def aclose(self) -> None:
        """Close the provider transport when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def resolve(self, call: Call, operation: str) -> tuple[Credentials | None, Exchange | None]:
        """Distinguish revoked OAuth grants, temporary refresh failures, and invalid credentials."""
        try:
            credentials = await self.credentials.resolve(call)
        except ReauthorizationRequiredError:
            return None, Exchange(
                "rejected",
                problem=failure(
                    operation, FailureKind.AUTHENTICATION, "monday.com authorization must be renewed"
                ),
            )
        except (RetryError, RefreshFailedError):
            return None, Exchange(
                "retry",
                problem=failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "monday.com OAuth token refresh is temporarily unavailable",
                ),
            )
        except (ValueError, LookupError, OSError):
            return None, Exchange(
                "defect",
                problem=failure(
                    operation, FailureKind.AUTHENTICATION, "monday.com connection credentials are unavailable"
                ),
            )
        method = credentials.auth_method_id
        token = credentials.access_token if method == "monday-oauth" else credentials.api_token
        if (
            method not in {"monday-oauth", "personal-api-token"}
            or token is None
            or not is_header_safe_credential(token.reveal())
        ):
            return None, Exchange(
                "defect",
                problem=failure(
                    operation, FailureKind.AUTHENTICATION, "monday.com connection credentials are unavailable"
                ),
            )
        return credentials, None

    async def exchange(
        self,
        call: Call,
        operation: str,
        document: str,
        variables: dict[str, Any],
        *,
        mutation: bool = False,
    ) -> Exchange:
        """Resolve credentials and classify one provider request, refreshing once after OAuth 401."""
        credentials, resolution = await self.resolve(call, operation)
        if resolution is not None:
            return resolution
        assert credentials is not None
        result = await self._send(call, operation, document, variables, credentials, mutation)
        if result.status == 401 and credentials.auth_method_id == "monday-oauth":
            refresh = getattr(self.credentials, "resolve_after_rejection", None)
            if refresh is not None:
                try:
                    replacement = await refresh(call)
                    token = replacement.access_token
                    if token is not None and is_header_safe_credential(token.reveal()):
                        result = await self._send(call, operation, document, variables, replacement, mutation)
                except (ReauthorizationRequiredError, RetryError, ValueError, LookupError, OSError):
                    pass  # The original credential rejection remains the terminal provider outcome.
        return result

    async def _send(
        self,
        call: Call,
        operation: str,
        document: str,
        variables: dict[str, Any],
        credentials: Credentials,
        mutation: bool,
    ) -> Exchange:
        token = (
            credentials.access_token
            if credentials.auth_method_id == "monday-oauth"
            else credentials.api_token
        )
        assert token is not None
        if mutation and not call.idempotency_key:
            return Exchange(
                "defect",
                problem=failure(
                    operation, FailureKind.LOCAL_DEFECT, "monday.com mutation has no idempotency key"
                ),
            )
        headers = {
            "Accept": "application/json",
            "Content-Type": "application/json",
            "Authorization": token.reveal(),
            "API-Version": API_VERSION,
        }
        if mutation:
            headers["Idempotency-Key"] = str(call.idempotency_key)
        try:
            request = self.http_client.build_request(
                "POST",
                self.endpoint,
                json={"query": document, "variables": variables},
                headers=headers,
            )
        except (ValueError, httpx.HTTPError):
            return Exchange(
                "defect",
                problem=failure(operation, FailureKind.LOCAL_DEFECT, "monday.com request could not be built"),
            )
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError as error:
            outcome = (
                "retry"
                if error.provably_not_sent or not mutation or operation == "updateItemColumnValues"
                else "uncertain"
            )
            message = (
                "monday.com could not be reached; no request was sent"
                if error.provably_not_sent
                else "monday.com request failed before a response arrived"
            )
            return Exchange(
                outcome,
                problem=failure(operation, FailureKind.TRANSPORT, message),
                confirmed_not_processed=error.provably_not_sent,
            )
        status = response.status_code
        replayed = response.headers.get("Idempotency-Replayed", "").strip().lower() == "true"
        retry_after = parse_retry_after(response.headers.get("Retry-After", ""), self.now())
        try:
            body = await read_bounded_body(response, self.max_response_bytes)
        except BodyTooLargeError:
            if 200 <= status < 300:
                return Exchange(
                    "invalid",
                    status,
                    replayed=replayed,
                    problem=failure(
                        operation,
                        FailureKind.RESPONSE_TOO_LARGE,
                        "monday.com response exceeds the configured maxResponseBytes limit",
                    ),
                )
            body = b""
        except ProviderTransportError:
            outcome = "invalid" if mutation and 200 <= status < 300 else "retry"
            return Exchange(
                outcome,
                status,
                replayed=replayed,
                problem=failure(operation, FailureKind.TRANSPORT, "monday.com response could not be read"),
            )
        if token.reveal().encode() in body:
            return Exchange(
                "invalid",
                status,
                replayed=replayed,
                problem=failure(
                    operation, FailureKind.PROTOCOL, "monday.com response reflected the connection credential"
                ),
            )
        parsed: Any = None
        try:
            parsed = json.loads(body)
        except (UnicodeDecodeError, json.JSONDecodeError):
            pass
        request_id = ""
        if isinstance(parsed, dict):
            extensions = parsed.get("extensions")
            candidate = extensions.get("request_id") if isinstance(extensions, dict) else None
            if isinstance(candidate, str) and _REQUEST_ID.fullmatch(candidate):
                request_id = candidate
        if status < 200 or status >= 300:
            return _classify_response(
                operation, status, parsed, request_id, replayed, retry_after, token.reveal()
            )
        if not isinstance(parsed, dict):
            return Exchange(
                "invalid",
                status,
                request_id=request_id,
                replayed=replayed,
                problem=failure(
                    operation,
                    FailureKind.PROTOCOL,
                    "monday.com returned a response that is not a GraphQL JSON object",
                ),
            )
        data = parsed.get("data")
        errors = parsed.get("errors")
        if isinstance(errors, list) and errors:
            return _classify_response(
                operation, status, parsed, request_id, replayed, retry_after, token.reveal(), data
            )
        if data is None:
            return Exchange(
                "invalid",
                status,
                request_id=request_id,
                replayed=replayed,
                problem=failure(
                    operation, FailureKind.PROTOCOL, "monday.com returned neither data nor errors"
                ),
            )
        return Exchange("succeeded", status, data, request_id, replayed)

    def receipt(self, call: Call, exchange: Exchange, object_id: str = "") -> Receipt:
        """Attach provider IDs and the replay flag to a secret-free receipt."""
        return Receipt(
            call.id,
            "monday",
            self.now().astimezone(UTC),
            call.idempotency_key,
            object_id,
            exchange.request_id,
            {"idempotencyReplayed": "true"} if exchange.replayed else {},
        )


def _read_token(document: Any, *path: str) -> str:
    current = document
    for segment in path:
        if isinstance(current, dict):
            current = current.get(segment)
        elif isinstance(current, list) and segment.isdecimal() and int(segment) < len(current):
            current = current[int(segment)]
        else:
            return ""
    if isinstance(current, (str, int, float)) and not isinstance(current, bool):
        value = str(current)
        return value if re.fullmatch(r"[A-Za-z0-9_.:-]{1,64}", value) else ""
    return ""


def _classify_response(
    operation: str,
    http_status: int,
    document: Any,
    request_id: str,
    replayed: bool,
    retry_after: float,
    secret: str,
    data: Any = None,
) -> Exchange:
    code = _read_token(document, "errors", "0", "extensions", "code") or _read_token(document, "error_code")
    status = http_status
    extension_status = _read_token(document, "errors", "0", "extensions", "status_code")
    if 200 <= status < 300 and extension_status.isdecimal() and int(extension_status) >= 400:
        status = int(extension_status)
    retry_seconds = _read_token(document, "errors", "0", "extensions", "retry_in_seconds")
    try:
        retry_seconds_float = float(retry_seconds)
        if 0 < retry_seconds_float <= 3600:
            retry_after = max(retry_after, min(3600, float(__import__("math").ceil(retry_seconds_float))))
    except ValueError:
        pass
    column_id = _read_token(document, "errors", "0", "extensions", "error_data", "column_id")
    if secret in code:
        code = ""
    if secret in column_id:
        column_id = ""
    extras = "; ".join(part for part in (code, f"column {column_id}" if column_id else "") if part)

    def outcome(name: str, kind: FailureKind, verb: str, *, conflict: bool = False) -> Exchange:
        message = f"monday.com {verb} (HTTP {http_status})"
        if extras:
            message += f" [{extras}]"
        delay = max(retry_after, 2) if conflict else retry_after
        confirmed = (
            code in _RATE_CODES | _TEMPORARY_CODES | {"IDEMPOTENCY_CONFLICT"}
            or status in (409, 423, 429, 503)
        ) and data is None
        return Exchange(
            name, http_status, data, request_id, replayed, failure(operation, kind, message), delay, confirmed
        )

    if code == "DAILY_LIMIT_EXCEEDED":
        return outcome(
            "rejected", FailureKind.QUOTA_EXHAUSTED, "daily call limit is exhausted until midnight UTC"
        )
    if code in _RATE_CODES:
        return outcome("retry", FailureKind.RATE_LIMIT, "rate limited the request")
    if code == "IDEMPOTENCY_CONFLICT":
        return outcome(
            "retry",
            FailureKind.CONFLICT,
            "is still processing an earlier attempt with this idempotency key",
            conflict=True,
        )
    if code in _TEMPORARY_CODES:
        return outcome("retry", FailureKind.AVAILABILITY, "temporarily refused the request")
    if code in _AUTHENTICATION_CODES:
        return outcome("rejected", FailureKind.AUTHENTICATION, "rejected the credential")
    if code in _AUTHORIZATION_CODES:
        return outcome("rejected", FailureKind.AUTHORIZATION, "denied permission")
    if code in _NOT_FOUND_CODES:
        return outcome("notFound", FailureKind.NOT_FOUND, "found no such board, group, or item")
    if code in _VALIDATION_CODES:
        return outcome("rejected", FailureKind.VALIDATION, "rejected the input")
    if code in _DOCUMENT_CODES:
        return outcome("defect", FailureKind.PROTOCOL, "rejected the connector's GraphQL request")
    if status == 429:
        return outcome("retry", FailureKind.RATE_LIMIT, "rate limited the request")
    if status == 409:
        return outcome(
            "retry",
            FailureKind.CONFLICT,
            "is still processing an earlier attempt with this idempotency key",
            conflict=True,
        )
    if status == 423:
        return outcome("retry", FailureKind.AVAILABILITY, "locked the board for a concurrent change")
    if status == 408 or status >= 500:
        return outcome("retry", FailureKind.AVAILABILITY, "could not complete the request")
    if status == 401:
        return outcome("rejected", FailureKind.AUTHENTICATION, "rejected the credential")
    if status == 403:
        return outcome("rejected", FailureKind.AUTHORIZATION, "denied permission")
    if status == 404:
        return outcome("notFound", FailureKind.NOT_FOUND, "found no such board, group, or item")
    if status == 400:
        return outcome("defect", FailureKind.PROTOCOL, "rejected the connector's GraphQL request")
    if status == 422:
        return outcome("rejected", FailureKind.VALIDATION, "rejected the input")
    if 300 <= status < 400:
        return outcome("rejected", FailureKind.PROTOCOL, "redirected the request")
    return outcome("rejected", FailureKind.PROVIDER_REJECTION, "rejected the request")
