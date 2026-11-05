# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Confluence gateway transport and OAuth credential resolution."""

from __future__ import annotations

import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import TYPE_CHECKING, Any
from urllib.parse import quote, urlencode

import httpx
from dex_connectors.sdk import (
    Call,
    Failure,
    FailureKind,
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

if TYPE_CHECKING:
    from .operations import (
        AddCommentOperation,
        CreatePageOperation,
        GetPageOperation,
        SearchPagesOperation,
        UpdatePageOperation,
    )

_CLOUD_ID = re.compile(r"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$", re.I)
_DEFAULT_ENDPOINT = "https://api.atlassian.com"


def failure(operation: str, kind: FailureKind, message: str) -> Failure:
    """Build a secret-safe connector failure."""
    return Failure(kind, "confluence", operation, message)


def path_escape(value: str) -> str:
    """Escape one path segment with Go url.PathEscape's safe alphabet."""
    return quote(value, safe="-._~$&+=:@")


def decode_object(response: Exchange) -> dict[str, Any]:
    """Decode only a complete provider JSON object."""
    value = json.loads(response.body)
    if not isinstance(value, dict):
        raise ValueError("invalid response object")
    return value


@dataclass(slots=True)
class Exchange:
    """Provider response with no retained secret-bearing error body."""

    status: int = 0
    headers: httpx.Headers | None = None
    body: bytes = b""
    is_too_large: bool = False
    is_dispatched: bool = False
    error: str = ""
    reflects_secret: bool = False
    body_failed: bool = False


@dataclass(slots=True)
class Session:
    """One invocation's call, selected site, and current access token."""

    call: Call
    cloud_id: str
    token: str


@dataclass(frozen=True, slots=True)
class Classification:
    """A safe exchange decision."""

    outcome: str
    failure: Failure | None = None
    retry_after: float = 0


@dataclass(frozen=True, slots=True)
class SessionError(Exception):
    """A terminal or retryable failure before operation dispatch."""

    failure: Failure
    retryable: bool = False
    retry_after: float = 0


class Client:
    """Bounded Atlassian Confluence REST client for five operations."""

    def __init__(
        self, config: Config, credentials: Any, http_client: httpx.AsyncClient | None = None, now: Any = None
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Confluence configuration and credentials are required")
        self.endpoint = validate_base_url(config.endpoint or _DEFAULT_ENDPOINT).rstrip("/")
        self.cloud_id = config.cloud_id.strip().lower()
        if self.cloud_id and not _CLOUD_ID.fullmatch(self.cloud_id):
            raise ValueError("Confluence cloudId must be a site UUID")
        self.max_response_bytes = config.max_response_bytes or 8_388_608
        if type(self.max_response_bytes) is not int or self.max_response_bytes < 1:
            raise ValueError("Confluence maxResponseBytes must be positive")
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(20)
        self.owns_http_client = http_client is None
        self.now = now or (lambda: datetime.now(UTC))
        self.resolved_cloud_id = ""

    async def aclose(self) -> None:
        """Close only a transport this client created."""
        if self.owns_http_client:
            await self.http_client.aclose()

    def search_pages(self) -> SearchPagesOperation:
        """Bind page search."""
        from .operations import SearchPagesOperation

        return SearchPagesOperation(self)

    def get_page(self) -> GetPageOperation:
        """Bind page read."""
        from .operations import GetPageOperation

        return GetPageOperation(self)

    def create_page(self) -> CreatePageOperation:
        """Bind page create."""
        from .operations import CreatePageOperation

        return CreatePageOperation(self)

    def update_page(self) -> UpdatePageOperation:
        """Bind page update."""
        from .operations import UpdatePageOperation

        return UpdatePageOperation(self)

    def add_comment(self) -> AddCommentOperation:
        """Bind footer comment creation."""
        from .operations import AddCommentOperation

        return AddCommentOperation(self)

    async def start_session(self, call: Call, operation: str) -> Session:
        """Resolve a safe access token and a unique Confluence site."""
        try:
            credential = await self.credentials.resolve(call)
            token = credential.access_token.reveal()
            if not is_header_safe_credential(token):
                raise ValueError("invalid token")
        except Exception as error:
            raise SessionError(
                failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "Confluence connection credentials are unavailable; reconnect the connection",
                )
            ) from error
        session = Session(call, self.cloud_id or self.resolved_cloud_id, token)
        if session.cloud_id:
            return session
        response = await self.exchange(session, "GET", "gateway", "/oauth/token/accessible-resources")
        if response.error:
            raise SessionError(
                failure(
                    operation,
                    FailureKind.TRANSPORT,
                    "Atlassian accessible resources are temporarily unavailable",
                ),
                True,
            )
        if response.status == 429:
            raise SessionError(
                failure(
                    operation,
                    FailureKind.RATE_LIMIT,
                    "Atlassian rate limited the accessible resources lookup",
                ),
                True,
                self.retry_after(response),
            )
        if response.status == 408 or response.status >= 500:
            raise SessionError(
                failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "Atlassian accessible resources are temporarily unavailable",
                ),
                True,
            )
        if not 200 <= response.status < 300:
            raise SessionError(
                failure(
                    operation,
                    status_kind(response.status),
                    (
                        f"Atlassian rejected the accessible resources lookup with HTTP {response.status}; "
                        "reconnect the connection or set cloudId"
                    ),
                )
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
                    and any(isinstance(scope, str) and "confluence" in scope for scope in resource["scopes"])
                }
            )
        except (ValueError, TypeError, KeyError) as error:
            raise SessionError(
                failure(
                    operation,
                    FailureKind.PROTOCOL,
                    "Atlassian returned an invalid accessible resources list; set cloudId",
                )
            ) from error
        if len(sites) != 1:
            if not sites:
                raise SessionError(
                    failure(
                        operation,
                        FailureKind.AUTHORIZATION,
                        (
                            "the authorization grants no Confluence site; reconnect and pick a "
                            "Confluence site on the consent screen"
                        ),
                    )
                )
            raise SessionError(
                failure(
                    operation,
                    FailureKind.VALIDATION,
                    (
                        f"the authorization grants {len(sites)} Confluence sites; choose one with the "
                        "Confluence site picker or set cloudId"
                    ),
                )
            )
        session.cloud_id = sites[0]
        self.resolved_cloud_id = sites[0]
        return session

    async def exchange(
        self,
        session: Session,
        method: str,
        api: str,
        path: str,
        query: dict[str, str] | None = None,
        payload: object | None = None,
    ) -> Exchange:
        """Send one bounded exchange and refresh a rejected token once."""
        prefix = (
            ""
            if api == "gateway"
            else f"/ex/confluence/{session.cloud_id}/wiki/" + ("api/v2" if api == "content" else "rest/api")
        )
        target = self.endpoint + prefix + path
        if query:
            target += "?" + urlencode(sorted(query.items()))
        try:
            body = (
                json.dumps(payload, separators=(",", ":"), ensure_ascii=False).encode()
                if payload is not None
                else None
            )
        except (TypeError, ValueError):
            return Exchange(error="request")
        for attempt in range(2):
            headers = {"Authorization": "Bearer " + session.token, "Accept": "application/json"}
            if body is not None:
                headers["Content-Type"] = "application/json"
            try:
                request = self.http_client.build_request(
                    method, target, headers=headers, content=body, timeout=20
                )
                response, trace = await send_traced(self.http_client, request)
            except ProviderTransportError as error:
                return Exchange(error="transport", is_dispatched=not error.provably_not_sent)
            except (ValueError, httpx.InvalidURL):
                return Exchange(error="request")
            if (
                response.status_code == 401
                and attempt == 0
                and hasattr(self.credentials, "resolve_after_rejection")
            ):
                try:
                    replacement = await self.credentials.resolve_after_rejection(session.call)
                    token = replacement.access_token.reveal()
                    if is_header_safe_credential(token):
                        await response.aclose()
                        session.token = token
                        continue
                except Exception:
                    pass  # A rejected refresh leaves the provider's 401 terminal.
            try:
                contents = await read_bounded_body(
                    response, self.max_response_bytes if 200 <= response.status_code < 300 else 65536
                )
                return Exchange(
                    response.status_code,
                    response.headers,
                    contents,
                    is_dispatched=True,
                    reflects_secret=session.token.encode() in contents,
                )
            except BodyTooLargeError:
                return Exchange(response.status_code, response.headers, is_too_large=True, is_dispatched=True)
            except ProviderTransportError:
                return Exchange(response.status_code, response.headers, is_dispatched=True, body_failed=True)
        raise RuntimeError("Confluence credential retry exhausted")

    def retry_after(self, response: Exchange) -> float:
        """Read Retry-After seconds, capped at one hour."""
        header = response.headers.get("Retry-After", "") if response.headers else ""
        return parse_retry_after(header, self.now())

    def classify_read(self, operation: str, subject: str, response: Exchange) -> Classification:
        """Classify a read without inspecting non-success bodies."""
        if response.error == "request":
            return Classification(
                "defect",
                failure(operation, FailureKind.LOCAL_DEFECT, "Confluence request could not be built"),
            )
        if response.error:
            return Classification(
                "retry",
                failure(operation, FailureKind.TRANSPORT, f"Confluence {subject} is temporarily unavailable"),
            )
        status = response.status
        if status == 404:
            return Classification(
                "notFound",
                failure(
                    operation,
                    FailureKind.NOT_FOUND,
                    f"Confluence {subject} was not found or is not visible to the connection",
                ),
            )
        if status == 429:
            return Classification(
                "retry",
                failure(operation, FailureKind.RATE_LIMIT, f"Confluence rate limited the {subject}"),
                self.retry_after(response),
            )
        if status == 408 or status >= 500:
            return Classification(
                "retry",
                failure(
                    operation, FailureKind.AVAILABILITY, f"Confluence {subject} is temporarily unavailable"
                ),
                self.retry_after(response),
            )
        if 400 <= status < 500:
            return Classification(
                "rejected",
                failure(
                    operation, status_kind(status), f"Confluence rejected the {subject} with HTTP {status}"
                ),
            )
        if not 200 <= status < 300:
            return Classification(
                "invalid",
                failure(
                    operation,
                    FailureKind.PROTOCOL,
                    f"Confluence returned unexpected HTTP {status} for the {subject}",
                ),
            )
        if response.body_failed:
            return Classification(
                "retry",
                failure(operation, FailureKind.TRANSPORT, f"Confluence {subject} response was interrupted"),
            )
        if response.is_too_large or response.reflects_secret:
            return Classification(
                "invalid",
                failure(
                    operation,
                    FailureKind.RESPONSE_TOO_LARGE if response.is_too_large else FailureKind.PROTOCOL,
                    f"Confluence {subject} response exceeds the configured size limit"
                    if response.is_too_large
                    else f"Confluence {subject} response contains credential material",
                ),
            )
        return Classification("accepted")

    def classify_write(self, operation: str, subject: str, response: Exchange) -> Classification:
        """Classify a write for safe read-back or retry decisions."""
        if response.error == "request":
            return Classification(
                "defect",
                failure(operation, FailureKind.LOCAL_DEFECT, "Confluence request could not be built"),
            )
        if response.error and not response.is_dispatched:
            return Classification(
                "notApplied",
                failure(
                    operation,
                    FailureKind.TRANSPORT,
                    f"Confluence could not be reached, so no {subject} was written",
                ),
            )
        if response.error:
            return Classification(
                "ambiguous",
                failure(operation, FailureKind.TRANSPORT, f"Confluence {subject} outcome is unknown"),
            )
        status = response.status
        if 200 <= status < 300:
            if response.body_failed:
                return Classification(
                    "ambiguous",
                    failure(
                        operation, FailureKind.TRANSPORT,
                        f"Confluence accepted the {subject} but its response was interrupted",
                    ),
                )
            if response.is_too_large or response.reflects_secret:
                return Classification(
                    "ambiguous",
                    failure(
                        operation,
                        FailureKind.RESPONSE_TOO_LARGE if response.is_too_large else FailureKind.PROTOCOL,
                        f"Confluence accepted the {subject} but its response is unusable",
                    ),
                )
            return Classification("accepted")
        if status == 429:
            return Classification(
                "notApplied",
                failure(
                    operation,
                    FailureKind.RATE_LIMIT,
                    f"Confluence rate limited the {subject} before writing it",
                ),
                self.retry_after(response),
            )
        if status == 404:
            return Classification(
                "notFound",
                failure(operation, FailureKind.NOT_FOUND, f"Confluence rejected the {subject} with HTTP 404"),
            )
        if 400 <= status < 500 and status != 408:
            return Classification(
                "rejected",
                failure(
                    operation, status_kind(status), f"Confluence rejected the {subject} with HTTP {status}"
                ),
            )
        return Classification(
            "ambiguous",
            failure(
                operation,
                FailureKind.AVAILABILITY,
                f"Confluence {subject} outcome is unknown after HTTP {status}",
            ),
            self.retry_after(response),
        )

    def receipt(self, session: Session, response: Exchange, object_id: str = "") -> Receipt:
        """Record safe request correlation and exact provider object ID."""
        trace = response.headers.get("Atl-Traceid", "") if response.headers else ""
        if not re.fullmatch(r"[\x21-\x7e]{1,128}", trace):
            trace = ""
        return Receipt(
            session.call.id,
            "confluence",
            self.now().astimezone(UTC),
            session.call.idempotency_key,
            object_id,
            trace,
        )


def status_kind(status: int) -> FailureKind:
    """Map Confluence status to the Go connector failure kind."""
    return {
        400: FailureKind.VALIDATION,
        401: FailureKind.AUTHENTICATION,
        403: FailureKind.AUTHORIZATION,
        404: FailureKind.NOT_FOUND,
        409: FailureKind.CONFLICT,
        429: FailureKind.RATE_LIMIT,
    }.get(status, FailureKind.PROVIDER_REJECTION)
