# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Linear GraphQL transport and operation bindings."""

from __future__ import annotations

import ipaddress
import json
import re
from dataclasses import dataclass, field, replace
from datetime import UTC, datetime
from typing import TYPE_CHECKING, Any
from urllib.parse import urlsplit

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    ReauthorizationRequiredError,
    Receipt,
)
from dex_connectors.sdk.projectconfig.provider import RefreshFailedError
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    parse_retry_after,
    read_bounded_body,
    send_traced,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials

if TYPE_CHECKING:
    from .operations import (
        AddCommentOperation,
        CreateIssueOperation,
        FindUserByEmailOperation,
        GetIssueOperation,
        ListWorkflowStatesOperation,
        SearchIssuesOperation,
        UpdateIssueOperation,
    )

_DEFAULT_URL = "https://api.linear.app/graphql"
_REQUEST_ID = re.compile(r"[A-Za-z0-9._:-]{1,128}\Z")
_TOKEN = re.compile(r"[\x21-\x7e]+\Z")
_CODES = re.compile(r"[A-Za-z0-9_.:-]{1,64}\Z")
_KNOWN_TYPES = {
    "feature not accessible",
    "invalid input",
    "ratelimited",
    "network error",
    "authentication error",
    "forbidden",
    "bootstrap error",
    "unknown",
    "internal error",
    "other",
    "user error",
    "graphql error",
    "lock timeout",
    "usage limit exceeded",
}


@dataclass(frozen=True, slots=True)
class Exchange:
    """One classified GraphQL exchange."""

    outcome: str
    data: Any = None
    status: int = 0
    request_id: str = ""
    failure: Failure | None = None
    retry_after: float = 0
    refreshed: bool = False
    credentials: Credentials | None = field(default=None, repr=False, compare=False)


class Client:
    """Send bounded GraphQL requests with current private or shared credentials."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        *,
        api_url: str = _DEFAULT_URL,
        now: Any = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Linear configuration and credential source are required")
        if config.max_response_bytes <= 0 or config.webhook_max_body_bytes <= 0:
            raise ValueError("Linear response and webhook body limits must be positive")
        if config.webhook_signature_tolerance_seconds < 1:
            raise ValueError("Linear webhookSignatureTolerance must be at least one second")
        if any(ord(character) <= 32 or ord(character) > 126 for character in api_url):
            raise ValueError("Linear API URL must use printable ASCII without spaces")
        if "?" in api_url or "#" in api_url:
            raise ValueError("Linear API URL cannot contain a query or fragment")
        parsed = urlsplit(api_url)
        try:
            is_loopback = (
                parsed.hostname == "localhost" or ipaddress.ip_address(parsed.hostname or "").is_loopback
            )
        except ValueError:
            is_loopback = parsed.hostname == "localhost"
        if (
            not parsed.hostname
            or parsed.scheme not in ("http", "https")
            or (parsed.scheme != "https" and not is_loopback)
            or parsed.username is not None
            or parsed.password is not None
            or parsed.query
            or parsed.fragment
        ):
            raise ValueError(
                "Linear API URL must use HTTPS or loopback and have no user info, query, or fragment"
            )
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or httpx.AsyncClient(follow_redirects=False)
        self.api_url = api_url.rstrip("/")
        self.now = now or (lambda: datetime.now(UTC))

    def search_issues(self) -> SearchIssuesOperation:
        """Bind the searchIssues Query."""
        from .operations import SearchIssuesOperation

        return SearchIssuesOperation(self)

    def get_issue(self) -> GetIssueOperation:
        """Bind the getIssue Query."""
        from .operations import GetIssueOperation

        return GetIssueOperation(self)

    def create_issue(self) -> CreateIssueOperation:
        """Bind the createIssue Mutation."""
        from .operations import CreateIssueOperation

        return CreateIssueOperation(self)

    def update_issue(self) -> UpdateIssueOperation:
        """Bind the updateIssue Mutation."""
        from .operations import UpdateIssueOperation

        return UpdateIssueOperation(self)

    def add_comment(self) -> AddCommentOperation:
        """Bind the addComment Mutation."""
        from .operations import AddCommentOperation

        return AddCommentOperation(self)

    def find_user_by_email(self) -> FindUserByEmailOperation:
        """Bind the findUserByEmail Query."""
        from .operations import FindUserByEmailOperation

        return FindUserByEmailOperation(self)

    def list_workflow_states(self) -> ListWorkflowStatesOperation:
        """Bind the listWorkflowStates Query."""
        from .operations import ListWorkflowStatesOperation

        return ListWorkflowStatesOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider transport after Worker shutdown."""
        await self.http_client.aclose()

    def receipt(self, call: Call, exchange: Exchange, object_id: str = "") -> Receipt:
        """Correlate a provider outcome without storing credentials or response bodies."""
        return Receipt(
            call_id=call.id,
            idempotency_key=call.idempotency_key,
            provider="linear",
            provider_object_id=object_id,
            provider_request_id=exchange.request_id,
            observed_at=self.now(),
        )

    async def resolve(self, call: Call, operation: str) -> Credentials | Exchange:
        """Resolve the current credential and classify refresh failures."""
        try:
            credential = await self.credentials.resolve(call)
        except ReauthorizationRequiredError:
            return Exchange(
                "rejected",
                failure=_failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "Linear authorization must be renewed in Dex Web Connections",
                ),
            )
        except RefreshFailedError:
            return Exchange(
                "retry",
                failure=_failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "Linear OAuth token refresh is temporarily unavailable",
                ),
            )
        except (ValueError, LookupError, OSError):
            return Exchange(
                "defect",
                failure=_failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "Linear connection credentials are unavailable or unusable",
                ),
            )
        if not isinstance(credential, Credentials):
            return Exchange(
                "defect",
                failure=_failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "Linear connection credentials are unavailable or unusable",
                ),
            )
        token = credential.access_token if credential.auth_method_id == "linear-oauth" else credential.api_key
        if (
            credential.auth_method_id not in ("linear-oauth", "personal-api-key")
            or token is None
            or not _TOKEN.fullmatch(token.reveal())
        ):
            return Exchange(
                "defect",
                failure=_failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "Linear connection credentials are unavailable or unusable",
                ),
            )
        return credential

    async def exchange(
        self,
        call: Call,
        operation: str,
        credentials: Credentials,
        operation_name: str,
        document: str,
        variables: dict[str, Any],
        *,
        allow_refresh: bool = True,
    ) -> Exchange:
        """Send one named GraphQL request and refresh once after an OAuth 401."""
        result = await self._send(operation, credentials, operation_name, document, variables)
        if (
            result.status == 401
            and allow_refresh
            and credentials.auth_method_id == "linear-oauth"
            and hasattr(self.credentials, "resolve_after_rejection")
        ):
            try:
                replacement = await self.credentials.resolve_after_rejection(call)
            except Exception:
                return replace(result, refreshed=True)
            if isinstance(replacement, Credentials) and replacement.access_token is not None:
                second = await self._send(operation, replacement, operation_name, document, variables)
                return replace(second, refreshed=True, credentials=replacement)
            return replace(result, refreshed=True)
        return result

    async def _send(
        self,
        operation: str,
        credentials: Credentials,
        operation_name: str,
        document: str,
        variables: dict[str, Any],
    ) -> Exchange:
        token = (
            credentials.access_token if credentials.auth_method_id == "linear-oauth" else credentials.api_key
        )
        assert token is not None
        secret = token.reveal()
        authorization = f"Bearer {secret}" if credentials.auth_method_id == "linear-oauth" else secret
        try:
            request = self.http_client.build_request(
                "POST",
                self.api_url,
                json={"operationName": operation_name, "query": document, "variables": variables},
                headers={
                    "Accept": "application/json",
                    "Content-Type": "application/json",
                    "Authorization": authorization,
                },
                timeout=12,
            )
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError as error:
            message = (
                "Linear could not be reached; no request was sent"
                if error.provably_not_sent
                else "Linear request failed before a response arrived"
            )
            return Exchange(
                "retry",
                failure=_failure(operation, FailureKind.TRANSPORT, message),
            )
        except httpx.TransportError:
            return Exchange(
                "retry",
                failure=_failure(
                    operation, FailureKind.TRANSPORT, "Linear request failed before a response arrived"
                ),
            )
        except (httpx.HTTPError, ValueError):
            return Exchange(
                "defect",
                failure=_failure(operation, FailureKind.LOCAL_DEFECT, "Linear request could not be built"),
            )
        status = response.status_code
        request_id = response.headers.get("X-Request-Id", "").strip()
        if not _REQUEST_ID.fullmatch(request_id):
            request_id = ""
        is_success = 200 <= status < 300
        try:
            body = await read_bounded_body(response, self.config.max_response_bytes)
        except BodyTooLargeError:
            if not is_success:
                return _classify_failure(
                    operation,
                    status,
                    request_id,
                    {},
                    parse_retry_after(response.headers.get("Retry-After", ""), self.now()),
                )
            return Exchange(
                "invalid",
                status=status,
                request_id=request_id,
                failure=_failure(
                    operation,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Linear response exceeds the configured maxResponseBytes limit",
                ),
            )
        except ProviderTransportError:
            return Exchange(
                "retry",
                status=status,
                request_id=request_id,
                failure=_failure(operation, FailureKind.TRANSPORT, "Linear response could not be read"),
            )
        if secret.encode() in body:
            return Exchange(
                "invalid",
                status=status,
                request_id=request_id,
                failure=_failure(
                    operation, FailureKind.PROTOCOL, "Linear response reflected the connection credential"
                ),
            )
        retry_after = parse_retry_after(response.headers.get("Retry-After", ""), self.now())
        try:
            envelope = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            envelope = None
        if not isinstance(envelope, dict) or (
            isinstance(envelope, dict)
            and envelope.get("errors") is not None
            and not isinstance(envelope.get("errors"), list)
        ):
            if is_success:
                return Exchange(
                    "invalid",
                    status=status,
                    request_id=request_id,
                    failure=_failure(
                        operation,
                        FailureKind.PROTOCOL,
                        "Linear returned a response that is not a GraphQL JSON object",
                    ),
                )
            return _classify_failure(operation, status, request_id, {}, retry_after)
        data = envelope.get("data")
        errors = envelope.get("errors")
        if errors or not is_success:
            return _classify_failure(operation, status, request_id, errors, retry_after)
        if data is None:
            return Exchange(
                "invalid",
                status=status,
                request_id=request_id,
                failure=_failure(operation, FailureKind.PROTOCOL, "Linear returned neither data nor errors"),
            )
        return Exchange("succeeded", data=data, status=status, request_id=request_id)


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "linear", operation, message)


def _classify_failure(
    operation: str, status: int, request_id: str, errors: Any, retry_after: float
) -> Exchange:
    first = errors[0] if isinstance(errors, list) and errors and isinstance(errors[0], dict) else {}
    extensions_value = first.get("extensions")
    extensions: dict[str, Any] = extensions_value if isinstance(extensions_value, dict) else {}
    code = extensions.get("code", "")
    error_type = extensions.get("type", "")
    code = code if isinstance(code, str) and _CODES.fullmatch(code) else ""
    error_type = error_type if isinstance(error_type, str) and error_type in _KNOWN_TYPES else ""
    suffix = f" [{'; '.join(part for part in (code, error_type) if part)}]" if code or error_type else ""
    if code == "RATELIMITED" or error_type == "ratelimited":
        outcome, kind, verb = "retry", FailureKind.RATE_LIMIT, "rate limited the request"
    elif code in ("AUTHENTICATION_ERROR", "UNAUTHENTICATED") or error_type == "authentication error":
        outcome, kind, verb = "rejected", FailureKind.AUTHENTICATION, "rejected the credential"
    elif code == "FORBIDDEN" or error_type in ("forbidden", "feature not accessible"):
        outcome, kind, verb = "rejected", FailureKind.AUTHORIZATION, "denied permission"
    elif error_type == "usage limit exceeded":
        outcome, kind, verb = "rejected", FailureKind.QUOTA_EXHAUSTED, "reported a workspace usage limit"
    elif (
        code
        in (
            "GRAPHQL_PARSE_FAILED",
            "GRAPHQL_VALIDATION_FAILED",
            "BAD_REQUEST",
            "OPERATION_RESOLUTION_FAILURE",
        )
        or error_type == "graphql error"
    ):
        outcome, kind, verb = "defect", FailureKind.PROTOCOL, "rejected the connector's GraphQL request"
    elif code in ("INTERNAL_SERVER_ERROR", "INTERNAL_ERROR", "LOCK_TIMEOUT") or error_type in (
        "internal error",
        "lock timeout",
        "network error",
        "bootstrap error",
    ):
        outcome, kind, verb = "retry", FailureKind.AVAILABILITY, "could not complete the request"
    elif code in ("INPUT_ERROR", "BAD_USER_INPUT", "INVALID_INPUT") or error_type in (
        "invalid input",
        "user error",
    ):
        outcome, kind, verb = "rejected", FailureKind.VALIDATION, "rejected the input"
    elif status == 429:
        outcome, kind, verb = "retry", FailureKind.RATE_LIMIT, "rate limited the request"
    elif status == 408 or status >= 500:
        outcome, kind, verb = "retry", FailureKind.AVAILABILITY, "could not complete the request"
    elif status == 401:
        outcome, kind, verb = "rejected", FailureKind.AUTHENTICATION, "rejected the credential"
    elif status == 403:
        outcome, kind, verb = "rejected", FailureKind.AUTHORIZATION, "denied permission"
    elif status == 400 and not code and not error_type:
        outcome, kind, verb = "defect", FailureKind.PROTOCOL, "rejected the connector's GraphQL request"
    elif 300 <= status < 400:
        outcome, kind, verb = "rejected", FailureKind.PROTOCOL, "redirected the request"
    else:
        outcome, kind, verb = "rejected", FailureKind.PROVIDER_REJECTION, "rejected the request"
    return Exchange(
        outcome,
        status=status,
        request_id=request_id,
        retry_after=retry_after,
        failure=_failure(operation, kind, f"Linear {verb} (HTTP {status}){suffix}"),
    )
