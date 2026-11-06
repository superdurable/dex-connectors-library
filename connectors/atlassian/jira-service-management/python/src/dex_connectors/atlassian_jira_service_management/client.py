# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Authenticated, bounded Jira Service Management provider client."""

import asyncio
import json
import re
import time
from contextvars import ContextVar
from dataclasses import dataclass, field
from datetime import UTC, datetime
from typing import TYPE_CHECKING, Any
from urllib.parse import quote, quote_plus

import httpx
from dex_connectors.sdk import Call, CredentialProvider, Failure, FailureKind, Receipt
from dex_connectors.sdk.providerhttp import (
    MAX_ERROR_BODY_BYTES,
    BodyTooLargeError,
    ProviderTransportError,
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
        AddCommentOperation,
        CreateTicketOperation,
        FindCustomerByEmailOperation,
        GetTicketOperation,
        SearchTicketsOperation,
        TransitionTicketOperation,
        UpdateTicketOperation,
    )

_CLOUD_ID = re.compile(r"[0-9a-fA-F]{8}(?:-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}\Z")
_FIELD_ID = re.compile(r"[A-Za-z][A-Za-z0-9_.-]{0,63}\Z")
_ERROR_KEY = re.compile(r"[A-Za-z][A-Za-z0-9_.-]{0,127}\Z")
_PATH_SAFE = "-._~$&+=:@"
_operation_deadline: ContextVar[float] = ContextVar("jira_operation_deadline", default=float("inf"))


def path_escape(value: str) -> str:
    """Escape a path segment like Go's url.PathEscape."""
    return quote(value, safe=_PATH_SAFE)


def _query_escape(value: str) -> str:
    return quote_plus(value, safe="~")


@dataclass(frozen=True, slots=True)
class ProviderResponse:
    """Provider status and bounded body after one exchange."""

    status_code: int = 0
    headers: httpx.Headers = field(default_factory=httpx.Headers)
    body: bytes = b""
    is_body_too_large: bool = False
    has_body_read_failed: bool = False
    reflects_secret: bool = False
    is_dispatched: bool = False
    transport_error: bool = False
    request_not_built: bool = False
    too_little_time: bool = False


@dataclass(frozen=True, slots=True)
class Classification:
    """Safe provider outcome shared by operation invocations."""

    outcome: str
    failure: Failure | None = None
    retry_after: float = 0
    field_ids: tuple[str, ...] = ()
    error_key: str = ""


def failure(operation: str, kind: FailureKind, message: str) -> Failure:
    """Build a secret-safe provider failure."""
    return Failure(kind, "jira-service-management", operation, message)


def _failure_kind(status: int) -> FailureKind:
    return {
        400: FailureKind.VALIDATION,
        422: FailureKind.VALIDATION,
        401: FailureKind.AUTHENTICATION,
        403: FailureKind.AUTHORIZATION,
        404: FailureKind.NOT_FOUND,
        409: FailureKind.CONFLICT,
        429: FailureKind.RATE_LIMIT,
    }.get(status, FailureKind.PROVIDER_REJECTION)


def _rejection(body: bytes) -> tuple[tuple[str, ...], str]:
    try:
        document = json.loads(body)
        if not isinstance(document, dict):
            return (), ""
        errors = document.get("errors")
        fields = (
            tuple(sorted(key for key in errors if _FIELD_ID.fullmatch(key)))[:20]
            if isinstance(errors, dict)
            else ()
        )
        message = document.get("i18nErrorMessage")
        key = message.get("i18nKey", "") if isinstance(message, dict) else ""
        return fields, key if isinstance(key, str) and _ERROR_KEY.fullmatch(key) else ""
    except (ValueError, TypeError):
        return (), ""


def _rejection_failure(
    operation: str, subject: str, response: ProviderResponse
) -> tuple[Failure, tuple[str, ...], str]:
    fields, error_key = _rejection(response.body)
    message = f"Jira Service Management rejected the {subject} with HTTP {response.status_code}"
    if fields:
        message += " (fields: " + ", ".join(fields) + ")"
    if error_key:
        message += f" [{error_key}]"
    return failure(operation, _failure_kind(response.status_code), message), fields, error_key


class Client:
    """Execute Jira provider calls through one OAuth credential source.

    The caller owns an injected HTTP client; a client created here is closed by ``aclose``.
    """

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Jira Service Management configuration and credentials are required")
        endpoint = validate_base_url(config.endpoint or "https://api.atlassian.com")
        cloud_id = config.cloud_id.strip()
        if cloud_id and not _CLOUD_ID.fullmatch(cloud_id):
            raise ValueError("Jira Service Management cloudId must be a site UUID")
        if config.max_response_bytes < 1:
            raise ValueError("Jira Service Management maxResponseBytes must be positive")
        self.config = config
        self.endpoint = endpoint.rstrip("/")
        self.cloud_id = cloud_id.lower()
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(20)
        self._owns_http_client = http_client is None
        self._resolved_cloud_id = ""

    async def aclose(self) -> None:
        """Close the connector-owned HTTP transport."""
        if self._owns_http_client:
            await self.http_client.aclose()

    def find_customer_by_email(self) -> "FindCustomerByEmailOperation":
        """Return the customer lookup query."""
        from .operations import FindCustomerByEmailOperation

        return FindCustomerByEmailOperation(self)

    def search_tickets(self) -> "SearchTicketsOperation":
        """Return the ticket search query."""
        from .operations import SearchTicketsOperation

        return SearchTicketsOperation(self)

    def get_ticket(self) -> "GetTicketOperation":
        """Return the ticket details query."""
        from .operations import GetTicketOperation

        return GetTicketOperation(self)

    def create_ticket(self) -> "CreateTicketOperation":
        """Return the ticket creation mutation."""
        from .operations import CreateTicketOperation

        return CreateTicketOperation(self)

    def update_ticket(self) -> "UpdateTicketOperation":
        """Return the ticket update mutation."""
        from .operations import UpdateTicketOperation

        return UpdateTicketOperation(self)

    def transition_ticket(self) -> "TransitionTicketOperation":
        """Return the ticket transition mutation."""
        from .operations import TransitionTicketOperation

        return TransitionTicketOperation(self)

    def add_comment(self) -> "AddCommentOperation":
        """Return the request comment mutation."""
        from .operations import AddCommentOperation

        return AddCommentOperation(self)

    async def resolve(self, call: Call, operation: str) -> tuple[Credentials | None, Classification | None]:
        """Resolve credentials and the configured or provider-derived cloud ID."""
        deadline = time.monotonic() + 25
        if call.context is not None and call.context.deadline is not None:
            deadline = min(
                deadline, time.monotonic() + (call.context.deadline - datetime.now(UTC)).total_seconds() - 5
            )
        _operation_deadline.set(deadline)
        if deadline - time.monotonic() < 3:
            return None, Classification(
                "retry",
                failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "the Step attempt has too little time left to start a provider request",
                ),
            )
        try:
            credentials = await self.credentials.resolve(call)
            token = credentials.access_token.reveal()
            if not token or any(character in token for character in "\r\n\x00"):
                raise ValueError("invalid access token")
        except Exception:
            return None, Classification(
                "defect",
                failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "Jira Service Management connection credentials are unavailable; "
                    "reconnect the connection",
                ),
            )
        if not self.cloud_id and not self._resolved_cloud_id:
            response = await self._exchange(
                call, credentials, "GET", "gateway", "/oauth/token/accessible-resources"
            )
            if response.too_little_time:
                return None, Classification(
                    "retry",
                    failure(
                        operation,
                        FailureKind.AVAILABILITY,
                        "the Step attempt has too little time left to start a provider request",
                    ),
                )
            if response.transport_error:
                return None, Classification(
                    "retry",
                    failure(
                        operation,
                        FailureKind.TRANSPORT,
                        "Atlassian accessible resources are temporarily unavailable",
                    ),
                )
            if response.status_code == 429:
                return None, Classification(
                    "retry",
                    failure(
                        operation,
                        FailureKind.RATE_LIMIT,
                        "Atlassian rate limited the accessible resources lookup",
                    ),
                    self.retry_after(response),
                )
            if response.status_code == 408 or response.status_code >= 500 or response.has_body_read_failed:
                return None, Classification(
                    "retry",
                    failure(
                        operation,
                        FailureKind.AVAILABILITY,
                        "Atlassian accessible resources are temporarily unavailable",
                    ),
                )
            if not 200 <= response.status_code < 300:
                return None, Classification(
                    "defect",
                    failure(
                        operation,
                        _failure_kind(response.status_code),
                        "Atlassian rejected the accessible resources lookup with HTTP "
                        f"{response.status_code}; "
                        "reconnect the connection or set cloudId",
                    ),
                )
            if response.is_body_too_large or response.reflects_secret:
                return None, Classification(
                    "defect",
                    failure(
                        operation,
                        FailureKind.PROTOCOL,
                        "Atlassian returned an unusable accessible resources list; set cloudId",
                    ),
                )
            try:
                resources = json.loads(response.body)
                if not isinstance(resources, list):
                    raise ValueError("invalid resources")
                sites = sorted(
                    {
                        resource["id"].lower()
                        for resource in resources
                        if isinstance(resource, dict)
                        and isinstance(resource.get("id"), str)
                        and _CLOUD_ID.fullmatch(resource["id"])
                        and isinstance(resource.get("scopes"), list)
                        and any(
                            scope in ("read:servicedesk-request", "write:servicedesk-request")
                            for scope in resource["scopes"]
                        )
                    }
                )
            except (ValueError, TypeError, KeyError):
                return None, Classification(
                    "defect",
                    failure(
                        operation,
                        FailureKind.PROTOCOL,
                        "Atlassian returned an invalid accessible resources list; set cloudId",
                    ),
                )
            if len(sites) == 0:
                return None, Classification(
                    "defect",
                    failure(
                        operation,
                        FailureKind.AUTHORIZATION,
                        "the authorization grants no Jira Service Management site; "
                        "reconnect and pick the site on the consent screen",
                    ),
                )
            if len(sites) != 1:
                return None, Classification(
                    "defect",
                    failure(
                        operation,
                        FailureKind.VALIDATION,
                        f"the authorization grants {len(sites)} Jira Service Management sites; "
                        "choose one with the site picker or set cloudId",
                    ),
                )
            self._resolved_cloud_id = sites[0]
        return credentials, None

    async def exchange(
        self,
        call: Call,
        credentials: Credentials,
        method: str,
        family: str,
        path: str,
        *,
        query: dict[str, str] | None = None,
        payload: Any = None,
        experimental: bool = False,
    ) -> ProviderResponse:
        """Send one authenticated exchange and refresh once after HTTP 401."""
        response = await self._exchange(
            call, credentials, method, family, path, query=query, payload=payload, experimental=experimental
        )
        if response.status_code == 401 and hasattr(self.credentials, "resolve_after_rejection"):
            try:
                replacement = await self.credentials.resolve_after_rejection(call)
                if replacement.access_token.reveal():
                    response = await self._exchange(
                        call,
                        replacement,
                        method,
                        family,
                        path,
                        query=query,
                        payload=payload,
                        experimental=experimental,
                    )
            except Exception:
                pass
        return response

    async def _exchange(
        self,
        call: Call,
        credentials: Credentials,
        method: str,
        family: str,
        path: str,
        *,
        query: dict[str, str] | None = None,
        payload: Any = None,
        experimental: bool = False,
    ) -> ProviderResponse:
        remaining = _operation_deadline.get() - time.monotonic()
        if remaining < 3 and method != "GET":
            return ProviderResponse(too_little_time=True)
        if remaining <= 0:
            return ProviderResponse(too_little_time=True)
        try:
            async with asyncio.timeout(None if remaining == float("inf") else remaining):
                return await self._exchange_once(
                    call, credentials, method, family, path,
                    query=query, payload=payload, experimental=experimental,
                )
        except TimeoutError:
            return ProviderResponse(is_dispatched=method != "GET", transport_error=True)

    async def _exchange_once(
        self,
        call: Call,
        credentials: Credentials,
        method: str,
        family: str,
        path: str,
        *,
        query: dict[str, str] | None = None,
        payload: Any = None,
        experimental: bool = False,
    ) -> ProviderResponse:
        remaining = _operation_deadline.get() - time.monotonic()
        target = self.endpoint
        if family != "gateway":
            prefix = "/rest/api/3" if family == "jira" else "/rest/servicedeskapi"
            target += "/ex/jira/" + path_escape(self.cloud_id or self._resolved_cloud_id) + prefix
        target += path
        if query:
            target += "?" + "&".join(
                f"{_query_escape(key)}={_query_escape(value)}" for key, value in sorted(query.items())
            )
        headers = {
            "Authorization": "Bearer " + credentials.access_token.reveal(),
            "Accept": "application/json",
        }
        if experimental:
            headers["X-ExperimentalApi"] = "opt-in"
        if payload is not None:
            headers["Content-Type"] = "application/json"
        try:
            request = self.http_client.build_request(
                method, target, json=payload, headers=headers, timeout=min(20, remaining)
            )
            response, _ = await send_traced(self.http_client, request)
            limit = (
                self.config.max_response_bytes if 200 <= response.status_code < 300 else MAX_ERROR_BODY_BYTES
            )
            try:
                body = await read_bounded_body(response, limit)
            except BodyTooLargeError:
                return ProviderResponse(
                    response.status_code, response.headers, is_body_too_large=True, is_dispatched=True
                )
            except ProviderTransportError:
                return ProviderResponse(
                    response.status_code, response.headers, has_body_read_failed=True, is_dispatched=True
                )
            token = credentials.access_token.reveal().encode()
            if token and token in body:
                return ProviderResponse(
                    response.status_code, response.headers, reflects_secret=True, is_dispatched=True
                )
            return ProviderResponse(response.status_code, response.headers, body=body, is_dispatched=True)
        except ProviderTransportError as error:
            return ProviderResponse(is_dispatched=not error.provably_not_sent, transport_error=True)
        except (httpx.HTTPError, ValueError, TypeError):
            return ProviderResponse(request_not_built=True)

    def classify_read(self, operation: str, subject: str, response: ProviderResponse) -> Classification:
        """Classify a read using Go's branch and retry boundaries."""
        if response.too_little_time:
            return Classification(
                "retry",
                failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "the Step attempt has too little time left to start a provider request",
                ),
            )
        if response.request_not_built:
            return Classification(
                "defect",
                failure(
                    operation, FailureKind.LOCAL_DEFECT, "Jira Service Management request could not be built"
                ),
            )
        if response.transport_error:
            return Classification(
                "retry",
                failure(
                    operation,
                    FailureKind.TRANSPORT,
                    f"Jira Service Management {subject} is temporarily unavailable",
                ),
            )
        status = response.status_code
        if status == 404:
            return Classification(
                "notFound",
                failure(
                    operation,
                    FailureKind.NOT_FOUND,
                    f"Jira Service Management {subject} was not found or is not visible to the connection",
                ),
            )
        if status == 429:
            return Classification(
                "retry",
                failure(
                    operation, FailureKind.RATE_LIMIT, f"Jira Service Management rate limited the {subject}"
                ),
                self.retry_after(response),
            )
        if status == 408 or status >= 500:
            return Classification(
                "retry",
                failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    f"Jira Service Management {subject} is temporarily unavailable",
                ),
                self.retry_after(response),
            )
        if 400 <= status < 500:
            rejection, fields, key = _rejection_failure(operation, subject, response)
            return Classification("providerRejected", rejection, field_ids=fields, error_key=key)
        if not 200 <= status < 300:
            return Classification(
                "invalidResponse",
                failure(
                    operation,
                    FailureKind.PROTOCOL,
                    f"Jira Service Management returned unexpected HTTP {status} for the {subject}",
                ),
            )
        if response.has_body_read_failed:
            return Classification(
                "retry",
                failure(
                    operation,
                    FailureKind.TRANSPORT,
                    f"Jira Service Management {subject} response was interrupted",
                ),
            )
        if response.is_body_too_large:
            return Classification(
                "invalidResponse",
                failure(
                    operation,
                    FailureKind.RESPONSE_TOO_LARGE,
                    f"Jira Service Management {subject} response exceeds the configured size limit",
                ),
            )
        if response.reflects_secret:
            return Classification(
                "invalidResponse",
                failure(
                    operation,
                    FailureKind.PROTOCOL,
                    f"Jira Service Management {subject} response contains credential material",
                ),
            )
        return Classification("success")

    def classify_write(self, operation: str, subject: str, response: ProviderResponse) -> Classification:
        """Classify an unkeyed write without assuming ambiguous delivery is safe to repeat."""
        if response.too_little_time:
            return Classification(
                "retry",
                failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    f"the Step attempt has too little time left to start the {subject} request",
                ),
            )
        if response.request_not_built:
            return Classification(
                "defect",
                failure(
                    operation, FailureKind.LOCAL_DEFECT, "Jira Service Management request could not be built"
                ),
            )
        if response.transport_error:
            if not response.is_dispatched:
                return Classification(
                    "retry",
                    failure(
                        operation,
                        FailureKind.TRANSPORT,
                        f"Jira Service Management could not be reached, so no {subject} was written",
                    ),
                )
            return Classification(
                "uncertain",
                failure(
                    operation, FailureKind.TRANSPORT, f"Jira Service Management {subject} outcome is unknown"
                ),
            )
        status = response.status_code
        if 200 <= status < 300:
            if response.has_body_read_failed:
                return Classification(
                    "uncertain",
                    failure(
                        operation,
                        FailureKind.TRANSPORT,
                        f"Jira Service Management accepted the {subject} but its response was interrupted",
                    ),
                )
            if response.is_body_too_large:
                return Classification(
                    "uncertain",
                    failure(
                        operation,
                        FailureKind.RESPONSE_TOO_LARGE,
                        f"Jira Service Management accepted the {subject} but its response "
                        "exceeds the configured size limit",
                    ),
                )
            if response.reflects_secret:
                return Classification(
                    "uncertain",
                    failure(
                        operation,
                        FailureKind.PROTOCOL,
                        f"Jira Service Management accepted the {subject} but its response "
                        "contains credential material",
                    ),
                )
            return Classification("success")
        if status == 429:
            return Classification(
                "retry",
                failure(
                    operation,
                    FailureKind.RATE_LIMIT,
                    f"Jira Service Management rate limited the {subject} before writing it",
                ),
                self.retry_after(response),
            )
        if status == 503:
            return Classification(
                "retry",
                failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    f"Jira Service Management is temporarily unavailable for the {subject}",
                ),
                self.retry_after(response),
            )
        if status == 409:
            return Classification(
                "uncertain",
                failure(
                    operation,
                    FailureKind.CONFLICT,
                    f"Jira Service Management {subject} outcome requires read-back after HTTP 409",
                ),
            )
        if status == 404:
            rejection, fields, key = _rejection_failure(operation, subject, response)
            return Classification("notFound", rejection, field_ids=fields, error_key=key)
        if 400 <= status < 500 and status != 408:
            rejection, fields, key = _rejection_failure(operation, subject, response)
            return Classification("providerRejected", rejection, field_ids=fields, error_key=key)
        return Classification(
            "uncertain",
            failure(
                operation,
                FailureKind.AVAILABILITY,
                f"Jira Service Management {subject} outcome is unknown after HTTP {status}",
            ),
        )

    def retry_after(self, response: ProviderResponse) -> float:
        """Read a bounded Retry-After value."""
        return parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))

    def receipt(self, call: Call, response: ProviderResponse, object_id: str = "") -> Receipt:
        """Build provider correlation without retaining credential material."""
        request_id = ""
        for header in ("X-Arequestid", "Atl-Traceid"):
            candidate = response.headers.get(header, "")
            if candidate and len(candidate) <= 128 and all(" " < character <= "~" for character in candidate):
                request_id = candidate
                break
        return Receipt(
            call.id, "jira-service-management", datetime.now(UTC), call.idempotency_key, object_id, request_id
        )
