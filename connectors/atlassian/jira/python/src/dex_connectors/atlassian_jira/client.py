# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Jira Cloud transport, credential resolution, and response classification."""

import json
import re
from dataclasses import dataclass, field
from datetime import UTC, datetime
from typing import TYPE_CHECKING, Any
from urllib.parse import quote, quote_plus

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    ReauthorizationRequiredError,
    Receipt,
)
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
        CreateIssueOperation,
        GetIssueOperation,
        SearchIssuesOperation,
        TransitionIssueOperation,
    )

_CLOUD_ID = re.compile(r"[0-9a-fA-F]{8}(?:-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}\Z")
_FIELD_ID = re.compile(r"[A-Za-z][A-Za-z0-9_.-]{0,63}\Z")


def path_escape(value: str) -> str:
    """Escape a path segment with Go url.PathEscape's safe characters."""
    return quote(value, safe="-._~$&+=:@")


def query_escape(value: str) -> str:
    """Escape a query component like Go url.QueryEscape, including tilde."""
    return quote_plus(value, safe="")


def failure(operation: str, kind: FailureKind, message: str) -> Failure:
    """Build a secret-safe Jira failure."""
    return Failure(kind, "jira", operation, message)


@dataclass(frozen=True, slots=True)
class Exchange:
    """One HTTP result with bounded body and dispatch evidence."""

    status: int = 0
    headers: httpx.Headers = field(default_factory=httpx.Headers)
    body: bytes = b""
    too_large: bool = False
    body_failed: bool = False
    reflects_secret: bool = False
    transport_error: bool = False
    not_sent: bool = False
    request_defect: bool = False


@dataclass(frozen=True, slots=True)
class Classification:
    """Go-equivalent meaning of one provider response."""

    outcome: str
    failure: Failure | None = None
    retry_after: float = 0
    rejected_fields: tuple[str, ...] = ()


class SessionError(Exception):
    """A credential or site lookup outcome before the operation request."""

    def __init__(self, reason: Failure, retryable: bool = False, retry_after: float = 0) -> None:
        self.failure = reason
        self.retryable = retryable
        self.retry_after = retry_after
        super().__init__(reason.message)


class Client:
    """Bounded Jira HTTP client with generated OAuth credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Jira configuration and credentials are required")
        self.endpoint = validate_base_url(config.endpoint or Config("").endpoint).rstrip("/")
        self.cloud_id = config.cloud_id.strip().lower()
        if self.cloud_id and not _CLOUD_ID.fullmatch(self.cloud_id):
            raise ValueError("Jira cloudId must be a site UUID")
        if config.max_response_bytes <= 0:
            raise ValueError("Jira maxResponseBytes must be positive")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(20)
        self._owns_http_client = http_client is None
        self._resolved_cloud_id = ""

    async def aclose(self) -> None:
        """Close a connector-owned HTTP client after Worker shutdown."""
        if self._owns_http_client:
            await self.http_client.aclose()

    def search_issues(self) -> "SearchIssuesOperation":
        """Return the searchIssues Query."""
        from .operations import SearchIssuesOperation

        return SearchIssuesOperation(self)

    def get_issue(self) -> "GetIssueOperation":
        """Return the getIssue Query."""
        from .operations import GetIssueOperation

        return GetIssueOperation(self)

    def create_issue(self) -> "CreateIssueOperation":
        """Return the fenced createIssue Mutation."""
        from .operations import CreateIssueOperation

        return CreateIssueOperation(self)

    def transition_issue(self) -> "TransitionIssueOperation":
        """Return the transitionIssue Mutation."""
        from .operations import TransitionIssueOperation

        return TransitionIssueOperation(self)

    def add_comment(self) -> "AddCommentOperation":
        """Return the fenced addComment Mutation."""
        from .operations import AddCommentOperation

        return AddCommentOperation(self)

    async def session(self, call: Call, operation: str) -> tuple[Credentials, str]:
        """Resolve credentials at call time and find the connected Jira site."""
        try:
            credentials = await self.credentials.resolve(call)
        except ReauthorizationRequiredError:
            raise SessionError(
                failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "Jira connection credentials are unavailable; reconnect the connection",
                )
            ) from None
        except Exception:
            raise SessionError(
                failure(
                    operation, FailureKind.AVAILABILITY, "Jira credential lookup is temporarily unavailable"
                ),
                True,
            ) from None
        if not isinstance(credentials, Credentials) or not credentials.access_token.reveal():
            raise SessionError(
                failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "Jira connection credentials are unavailable; reconnect the connection",
                )
            )
        if self.cloud_id or self._resolved_cloud_id:
            return credentials, self.cloud_id or self._resolved_cloud_id
        exchange = await self.request(
            call, credentials, "GET", "/oauth/token/accessible-resources", gateway=True
        )
        if exchange.request_defect:
            raise SessionError(
                failure(operation, FailureKind.LOCAL_DEFECT, "Jira request could not be built")
            )
        if (
            exchange.transport_error
            or exchange.body_failed
            or exchange.status == 408
            or exchange.status >= 500
        ):
            raise SessionError(
                failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "Atlassian accessible resources are temporarily unavailable",
                ),
                True,
            )
        if exchange.status == 429:
            raise SessionError(
                failure(
                    operation,
                    FailureKind.RATE_LIMIT,
                    "Atlassian rate limited the accessible resources lookup",
                ),
                True,
                self.retry_after(exchange),
            )
        if not 200 <= exchange.status < 300:
            raise SessionError(
                failure(
                    operation,
                    status_failure_kind(exchange.status),
                    f"Atlassian rejected the accessible resources lookup with HTTP {exchange.status}; "
                    "reconnect the connection or set cloudId",
                )
            )
        if exchange.too_large or exchange.reflects_secret:
            raise SessionError(
                failure(
                    operation,
                    FailureKind.PROTOCOL,
                    "Atlassian returned an unusable accessible resources list; set cloudId",
                )
            )
        try:
            resources = json.loads(exchange.body)
            if not isinstance(resources, list):
                raise ValueError("invalid resources")
            sites = sorted(
                {
                    item["id"].lower()
                    for item in resources
                    if isinstance(item, dict)
                    and isinstance(item.get("id"), str)
                    and _CLOUD_ID.fullmatch(item["id"])
                    and isinstance(item.get("scopes"), list)
                    and any(scope in ("read:jira-work", "write:jira-work") for scope in item["scopes"])
                }
            )
        except (ValueError, TypeError, KeyError):
            raise SessionError(
                failure(
                    operation,
                    FailureKind.PROTOCOL,
                    "Atlassian returned an invalid accessible resources list; set cloudId",
                )
            ) from None
        if not sites:
            raise SessionError(
                failure(
                    operation,
                    FailureKind.AUTHORIZATION,
                    "the authorization grants no Jira site; reconnect and pick a Jira site "
                    "on the consent screen",
                )
            )
        if len(sites) > 1:
            raise SessionError(
                failure(
                    operation,
                    FailureKind.VALIDATION,
                    f"the authorization grants {len(sites)} Jira sites; choose one with the "
                    "Jira site picker or set cloudId",
                )
            )
        self._resolved_cloud_id = sites[0]
        return credentials, sites[0]

    async def request(
        self,
        call: Call,
        credentials: Credentials,
        method: str,
        path: str,
        *,
        cloud_id: str = "",
        gateway: bool = False,
        payload: Any = None,
        query: dict[str, str] | None = None,
        fenced: bool = False,
    ) -> Exchange:
        """Send one bounded exchange, refreshing a rejected read credential once."""
        url = self.endpoint
        if not gateway:
            url += f"/ex/jira/{path_escape(cloud_id)}/rest/api/3"
        url += path
        if query:
            url += "?" + "&".join(
                f"{query_escape(key)}={query_escape(value)}" for key, value in sorted(query.items())
            )
        try:
            body = (
                json.dumps(payload, separators=(",", ":"), ensure_ascii=False, allow_nan=False).encode()
                if payload is not None
                else None
            )
        except (TypeError, ValueError):
            return Exchange(request_defect=True)
        for index in range(2):
            result = await self._send_once(credentials, method, url, body)
            if (
                fenced
                or result.status != 401
                or index
                or not hasattr(self.credentials, "resolve_after_rejection")
            ):
                return result
            try:
                credentials = await self.credentials.resolve_after_rejection(call)
                if not credentials.access_token.reveal():
                    return result
            except Exception:
                return result
        return result

    async def _send_once(
        self, credentials: Credentials, method: str, url: str, body: bytes | None
    ) -> Exchange:
        token = credentials.access_token.reveal()
        headers = {"Authorization": f"Bearer {token}", "Accept": "application/json"}
        if body is not None:
            headers["Content-Type"] = "application/json"
        try:
            request = self.http_client.build_request(method, url, headers=headers, content=body, timeout=20)
        except (httpx.HTTPError, ValueError):
            return Exchange(request_defect=True)
        try:
            response, _ = await send_traced(self.http_client, request)
            limit = (
                self.config.max_response_bytes if 200 <= response.status_code < 300 else MAX_ERROR_BODY_BYTES
            )
            try:
                contents = await read_bounded_body(response, limit)
            except BodyTooLargeError:
                return Exchange(response.status_code, response.headers, too_large=True)
            except ProviderTransportError:
                return Exchange(response.status_code, response.headers, body_failed=True)
            if token.encode() in contents:
                return Exchange(response.status_code, response.headers, reflects_secret=True)
            return Exchange(response.status_code, response.headers, contents)
        except ProviderTransportError as error:
            return Exchange(transport_error=True, not_sent=error.provably_not_sent)
        except (httpx.HTTPError, ValueError):
            return Exchange(transport_error=True)

    def retry_after(self, response: Exchange) -> float:
        """Parse bounded Retry-After guidance."""
        return parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))

    def receipt(self, call: Call, response: Exchange | None = None, object_id: str = "") -> Receipt:
        """Record safe request and object identifiers, excluding credential material."""
        request_id = ""
        if response is not None:
            for header in ("X-Arequestid", "Atl-Traceid"):
                value = response.headers.get(header, "")
                if re.fullmatch(r"[A-Za-z0-9_.:-]{1,128}", value):
                    request_id = value
                    break
        return Receipt(call.id, "jira", datetime.now(UTC), call.idempotency_key, object_id, request_id)

    def classify_read(self, operation: str, subject: str, response: Exchange) -> Classification:
        """Classify a Jira read before its operation chooses a branch."""
        status = response.status
        if response.request_defect:
            return Classification(
                "defect", failure(operation, FailureKind.LOCAL_DEFECT, "Jira request could not be built")
            )
        if response.transport_error:
            return Classification(
                "retry",
                failure(operation, FailureKind.TRANSPORT, f"Jira {subject} is temporarily unavailable"),
            )
        if status == 404:
            return Classification(
                "notFound",
                failure(
                    operation,
                    FailureKind.NOT_FOUND,
                    f"Jira {subject} was not found or is not visible to the connection",
                ),
            )
        if status == 429:
            return Classification(
                "retry",
                failure(operation, FailureKind.RATE_LIMIT, f"Jira rate limited the {subject}"),
                self.retry_after(response),
            )
        if status == 408 or status >= 500:
            return Classification(
                "retry",
                failure(operation, FailureKind.AVAILABILITY, f"Jira {subject} is temporarily unavailable"),
                self.retry_after(response),
            )
        if 400 <= status < 500:
            fields = rejected_fields(response.body)
            return Classification(
                "providerRejected",
                rejection_failure(operation, subject, status, fields),
                rejected_fields=fields,
            )
        if not 200 <= status < 300:
            return Classification(
                "invalidResponse",
                failure(
                    operation,
                    FailureKind.PROTOCOL,
                    f"Jira returned unexpected HTTP {status} for the {subject}",
                ),
            )
        if response.body_failed:
            return Classification(
                "retry", failure(operation, FailureKind.TRANSPORT, f"Jira {subject} response was interrupted")
            )
        if response.too_large:
            return Classification(
                "invalidResponse",
                failure(
                    operation,
                    FailureKind.RESPONSE_TOO_LARGE,
                    f"Jira {subject} response exceeds the configured size limit",
                ),
            )
        if response.reflects_secret:
            return Classification(
                "invalidResponse",
                failure(
                    operation, FailureKind.PROTOCOL, f"Jira {subject} response contains credential material"
                ),
            )
        return Classification("success")

    def classify_write(self, operation: str, subject: str, response: Exchange) -> Classification:
        """Classify a non-idempotent write before read-back."""
        if response.request_defect:
            return Classification(
                "defect", failure(operation, FailureKind.LOCAL_DEFECT, "Jira request could not be built")
            )
        if response.transport_error:
            if response.not_sent:
                return Classification(
                    "retry",
                    failure(
                        operation,
                        FailureKind.TRANSPORT,
                        f"Jira could not be reached, so no {subject} was written",
                    ),
                )
            return Classification(
                "uncertain", failure(operation, FailureKind.TRANSPORT, f"Jira {subject} outcome is unknown")
            )
        status = response.status
        if 200 <= status < 300:
            if response.body_failed:
                return Classification(
                    "uncertain",
                    failure(
                        operation,
                        FailureKind.TRANSPORT,
                        f"Jira accepted the {subject} but its response was interrupted",
                    ),
                )
            if response.too_large:
                return Classification(
                    "uncertain",
                    failure(
                        operation,
                        FailureKind.RESPONSE_TOO_LARGE,
                        f"Jira accepted the {subject} but its response exceeds the configured size limit",
                    ),
                )
            if response.reflects_secret:
                return Classification(
                    "uncertain",
                    failure(
                        operation,
                        FailureKind.PROTOCOL,
                        f"Jira accepted the {subject} but its response contains credential material",
                    ),
                )
            return Classification("accepted")
        if status == 429:
            return Classification(
                "retry",
                failure(
                    operation, FailureKind.RATE_LIMIT, f"Jira rate limited the {subject} before writing it"
                ),
                self.retry_after(response),
            )
        if status == 503:
            return Classification(
                "retry",
                failure(operation, FailureKind.AVAILABILITY, f"Jira {subject} is temporarily unavailable"),
                self.retry_after(response),
            )
        fields = rejected_fields(response.body)
        if status == 404:
            return Classification("notFound", rejection_failure(operation, subject, status, ()))
        if 400 <= status < 500 and status != 408:
            return Classification(
                "rejected", rejection_failure(operation, subject, status, fields), rejected_fields=fields
            )
        return Classification(
            "uncertain",
            failure(
                operation, FailureKind.AVAILABILITY, f"Jira {subject} outcome is unknown after HTTP {status}"
            ),
        )


def status_failure_kind(status: int) -> FailureKind:
    """Translate definite provider rejections to failure kinds."""
    return {
        400: FailureKind.VALIDATION,
        401: FailureKind.AUTHENTICATION,
        403: FailureKind.AUTHORIZATION,
        404: FailureKind.NOT_FOUND,
        409: FailureKind.CONFLICT,
        429: FailureKind.RATE_LIMIT,
    }.get(status, FailureKind.PROVIDER_REJECTION)


def rejected_fields(body: bytes) -> tuple[str, ...]:
    """Keep bounded, safe Jira field IDs from a rejection body."""
    try:
        document = json.loads(body)
        errors = document.get("errors") if isinstance(document, dict) else None
        return (
            tuple(sorted(key for key in errors if _FIELD_ID.fullmatch(key))[:20])
            if isinstance(errors, dict)
            else ()
        )
    except (ValueError, TypeError):
        return ()


def rejection_failure(operation: str, subject: str, status: int, fields: tuple[str, ...]) -> Failure:
    """Describe HTTP rejection without provider-supplied message text."""
    message = f"Jira rejected the {subject} with HTTP {status}"
    if fields:
        message += " (fields: " + ", ".join(fields) + ")"
    return failure(operation, status_failure_kind(status), message)
