# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Authenticated Atlassian gateway transport and shared response classification."""

import json
import re
from dataclasses import dataclass, field
from datetime import UTC, datetime
from typing import TYPE_CHECKING, Any
from urllib.parse import quote

import httpx
from dex_connectors.sdk import Call, CredentialProvider, Failure, FailureKind, Receipt
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

_CLOUD_ID = re.compile(r"[0-9a-fA-F]{8}-(?:[0-9a-fA-F]{4}-){3}[0-9a-fA-F]{12}\Z")
_FIELD_ID = re.compile(r"[A-Za-z][A-Za-z0-9_.-]{0,63}\Z")
_ERROR_KEY = re.compile(r"[A-Za-z][A-Za-z0-9_.-]{0,127}\Z")
_SITE_SCOPES = {"read:servicedesk-request", "write:servicedesk-request"}


@dataclass(frozen=True, slots=True)
class Exchange:
    """One bounded provider exchange and its dispatch evidence."""

    status: int = 0
    headers: httpx.Headers = field(default_factory=httpx.Headers)
    body: bytes = b""
    error: str = ""
    is_body_too_large: bool = False
    has_body_read_failed: bool = False
    reflects_secret: bool = False


@dataclass(slots=True)
class Session:
    """Credential and site resolved for one Execute attempt."""

    call: Call
    token: str
    cloud_id: str


@dataclass(frozen=True, slots=True)
class SessionProblem(Exception):
    """A terminal or retryable failure before an operation request."""

    failure: Failure
    retry: bool = False
    retry_after: float = 0.0


class Client:
    """Send Jira REST requests using generated refreshable credentials."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Jira Service Management configuration and credentials are required")
        self.endpoint = validate_base_url(config.endpoint or Config("").endpoint)
        cloud_id = config.cloud_id.strip().lower()
        if cloud_id and not _CLOUD_ID.fullmatch(cloud_id):
            raise ValueError("cloudId must be an Atlassian site UUID")
        response_limit = config.max_response_bytes or Config("").max_response_bytes
        if type(config.max_response_bytes) is not int or response_limit < 1:
            raise ValueError("maxResponseBytes must be positive")
        self.cloud_id = cloud_id
        self.max_response_bytes = response_limit
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(20)
        self._owns_http_client = http_client is None
        self._resolved_cloud_id = ""

    def find_customer_by_email(self) -> "FindCustomerByEmailOperation":
        """Bind the customer lookup Query."""
        from .operations import FindCustomerByEmailOperation

        return FindCustomerByEmailOperation(self)

    def search_tickets(self) -> "SearchTicketsOperation":
        """Bind the ticket search Query."""
        from .operations import SearchTicketsOperation

        return SearchTicketsOperation(self)

    def get_ticket(self) -> "GetTicketOperation":
        """Bind the ticket detail Query."""
        from .operations import GetTicketOperation

        return GetTicketOperation(self)

    def create_ticket(self) -> "CreateTicketOperation":
        """Bind the customer request Mutation."""
        from .operations import CreateTicketOperation

        return CreateTicketOperation(self)

    def update_ticket(self) -> "UpdateTicketOperation":
        """Bind the repeatable issue edit Mutation."""
        from .operations import UpdateTicketOperation

        return UpdateTicketOperation(self)

    def transition_ticket(self) -> "TransitionTicketOperation":
        """Bind the repeatable workflow transition Mutation."""
        from .operations import TransitionTicketOperation

        return TransitionTicketOperation(self)

    def add_comment(self) -> "AddCommentOperation":
        """Bind the single dispatch comment Mutation."""
        from .operations import AddCommentOperation

        return AddCommentOperation(self)

    async def aclose(self) -> None:
        """Close only a connector owned HTTP client."""
        if self._owns_http_client:
            await self.http_client.aclose()

    async def session(self, call: Call, operation: str) -> Session:
        """Resolve OAuth credentials and choose the configured or unique JSM site."""
        try:
            credentials = await self.credentials.resolve(call)
            token = credentials.access_token.reveal()
            if not isinstance(credentials, Credentials) or not is_header_safe_credential(token):
                raise ValueError("invalid access token")
        except Exception as error:
            raise SessionProblem(
                failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "Jira Service Management connection credentials are unavailable; "
                    "reconnect the connection",
                )
            ) from error
        session = Session(call, token, self.cloud_id or self._resolved_cloud_id)
        if session.cloud_id:
            return session
        response = await self.exchange(session, "GET", "gateway", "/oauth/token/accessible-resources")
        if response.error:
            raise SessionProblem(
                failure(
                    operation,
                    FailureKind.TRANSPORT,
                    "Atlassian accessible resources are temporarily unavailable",
                ),
                True,
            )
        if response.status == 429:
            raise SessionProblem(
                failure(
                    operation,
                    FailureKind.RATE_LIMIT,
                    "Atlassian rate limited the accessible resources lookup",
                ),
                True,
                self.retry_after(response),
            )
        if response.status == 408 or response.status >= 500 or response.has_body_read_failed:
            raise SessionProblem(
                failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "Atlassian accessible resources are temporarily unavailable",
                ),
                True,
            )
        if not 200 <= response.status < 300:
            raise SessionProblem(
                failure(
                    operation,
                    status_failure_kind(response.status),
                    f"Atlassian rejected the accessible resources lookup with HTTP {response.status}; "
                    "reconnect the connection or set cloudId",
                )
            )
        if response.is_body_too_large or response.reflects_secret:
            raise SessionProblem(
                failure(
                    operation,
                    FailureKind.PROTOCOL,
                    "Atlassian returned an unusable accessible resources list; set cloudId",
                )
            )
        try:
            resources = json.loads(response.body)
            if not isinstance(resources, list):
                raise ValueError("invalid resources")
            cloud_ids = sorted(
                {
                    resource["id"].lower()
                    for resource in resources
                    if isinstance(resource, dict)
                    and isinstance(resource.get("id"), str)
                    and _CLOUD_ID.fullmatch(resource["id"])
                    and isinstance(resource.get("scopes"), list)
                    and any(scope in _SITE_SCOPES for scope in resource["scopes"])
                }
            )
        except (ValueError, TypeError, KeyError) as error:
            raise SessionProblem(
                failure(
                    operation,
                    FailureKind.PROTOCOL,
                    "Atlassian returned an invalid accessible resources list; set cloudId",
                )
            ) from error
        if not cloud_ids:
            raise SessionProblem(
                failure(
                    operation,
                    FailureKind.AUTHORIZATION,
                    "the authorization grants no Jira Service Management site; reconnect and "
                    "pick the site on the consent screen",
                )
            )
        if len(cloud_ids) > 1:
            raise SessionProblem(
                failure(
                    operation,
                    FailureKind.VALIDATION,
                    f"the authorization grants {len(cloud_ids)} Jira Service Management sites; "
                    "choose one with the site picker or set cloudId",
                )
            )
        self._resolved_cloud_id = cloud_ids[0]
        session.cloud_id = cloud_ids[0]
        return session

    async def exchange(
        self,
        session: Session,
        method: str,
        family: str,
        path: str,
        query: dict[str, str] | None = None,
        payload: object | None = None,
        experimental: bool = False,
    ) -> Exchange:
        """Send a bounded request and refresh once after an HTTP 401."""
        prefix = "" if family == "gateway" else f"/ex/jira/{path_escape(session.cloud_id)}"
        prefix += "/rest/api/3" if family == "jira" else "/rest/servicedeskapi" if family == "desk" else ""
        target = self.endpoint + prefix + path
        if query:
            target += "?" + "&".join(
                query_escape(key) + "=" + query_escape(value) for key, value in sorted(query.items())
            )
        try:
            body = (
                None
                if payload is None
                else json.dumps(payload, separators=(",", ":"), ensure_ascii=False, allow_nan=False).encode()
            )
        except (TypeError, ValueError):
            return Exchange(error="notBuilt")
        for attempt in range(2):
            headers = {"Authorization": f"Bearer {session.token}", "Accept": "application/json"}
            if body is not None:
                headers["Content-Type"] = "application/json"
            if experimental:
                headers["X-ExperimentalApi"] = "opt-in"
            try:
                request = self.http_client.build_request(
                    method, target, headers=headers, content=body, timeout=20.0
                )
            except (httpx.HTTPError, ValueError):
                return Exchange(error="notBuilt")
            try:
                response, _ = await send_traced(self.http_client, request)
            except ProviderTransportError as error:
                return Exchange(error="beforeSend" if error.provably_not_sent else "afterSend")
            if (
                response.status_code == 401
                and attempt == 0
                and hasattr(self.credentials, "resolve_after_rejection")
            ):
                try:
                    replacement = await self.credentials.resolve_after_rejection(session.call)
                    replacement_token = replacement.access_token.reveal()
                    if is_header_safe_credential(replacement_token):
                        session.token = replacement_token
                        continue
                except Exception:
                    pass
            limit = self.max_response_bytes if 200 <= response.status_code < 300 else MAX_ERROR_BODY_BYTES
            try:
                contents = await read_bounded_body(response, limit)
            except BodyTooLargeError:
                return Exchange(status=response.status_code, headers=response.headers, is_body_too_large=True)
            except ProviderTransportError:
                return Exchange(
                    status=response.status_code, headers=response.headers, has_body_read_failed=True
                )
            reflects_secret = session.token.encode() in contents
            return Exchange(
                response.status_code,
                response.headers,
                b"" if reflects_secret else contents,
                reflects_secret=reflects_secret,
            )
        return Exchange(error="notBuilt")

    def receipt(self, call: Call, exchange: Exchange, object_id: str = "") -> Receipt:
        """Record stable provider IDs and a safe request correlation ID."""
        request_id = next(
            (
                value
                for header in ("X-Arequestid", "Atl-Traceid")
                if (value := exchange.headers.get(header, "")) and safe_provider_token(value)
            ),
            "",
        )
        return Receipt(
            call.id, "jira-service-management", datetime.now(UTC), call.idempotency_key, object_id, request_id
        )

    def retry_after(self, exchange: Exchange) -> float:
        """Parse a bounded Retry-After value."""
        return parse_retry_after(exchange.headers.get("Retry-After", ""), datetime.now(UTC))


def path_escape(value: str) -> str:
    """Match Go url.PathEscape for a path segment."""
    return quote(value, safe="-._~$&+=:@")


def query_escape(value: str) -> str:
    """Match Go url.QueryEscape, including escaped tildes and plus spaces."""
    return quote(value, safe="-._").replace("~", "%7E").replace("%20", "+")


def safe_provider_token(value: str) -> bool:
    """Reject unsafe provider correlation IDs."""
    return bool(value and len(value) <= 128 and all(33 <= ord(character) <= 126 for character in value))


def failure(operation: str, kind: FailureKind, message: str) -> Failure:
    """Construct a secret safe failure."""
    return Failure(kind, "jira-service-management", operation, message)


def status_failure_kind(status: int) -> FailureKind:
    """Classify conclusive HTTP rejection by status."""
    return {
        400: FailureKind.VALIDATION,
        401: FailureKind.AUTHENTICATION,
        402: FailureKind.AUTHORIZATION,
        403: FailureKind.AUTHORIZATION,
        404: FailureKind.NOT_FOUND,
        409: FailureKind.CONFLICT,
    }.get(status, FailureKind.PROVIDER_REJECTION)


def rejection_fields(exchange: Exchange) -> tuple[tuple[str, ...], str]:
    """Keep only machine readable field IDs and error keys from a rejection."""
    try:
        body: Any = json.loads(exchange.body)
        if not isinstance(body, dict):
            return (), ""
        errors = body.get("errors", {})
        fields = (
            tuple(sorted(key for key in errors if _FIELD_ID.fullmatch(key)))
            if isinstance(errors, dict)
            else ()
        )
        i18n = body.get("i18nErrorMessage")
        key = i18n.get("i18nKey", "") if isinstance(i18n, dict) else ""
        return fields[:20], key if isinstance(key, str) and _ERROR_KEY.fullmatch(key) else ""
    except (ValueError, TypeError):
        return (), ""
