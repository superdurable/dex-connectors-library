# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""DocuSign account resolution, authenticated requests, and bounded responses."""

from __future__ import annotations

import hashlib
import json
import re
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import TYPE_CHECKING, Any, Protocol
from urllib.parse import quote, quote_plus, urlsplit

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
    read_error_tokens,
    send_traced,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials

if TYPE_CHECKING:
    from .operations import (
        CreateEnvelopeFromTemplateOperation,
        DownloadCombinedDocumentOperation,
        GetEnvelopeOperation,
        ListEnvelopeRecipientsOperation,
        VoidEnvelopeOperation,
    )

_GUID = re.compile(r"[0-9a-fA-F]{8}(?:-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}\Z")
_HOST_LABEL = re.compile(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\Z")
_RATE_CODES = frozenset(
    (
        "HOURLY_APIINVOCATION_LIMIT_EXCEEDED",
        "BURST_APIINVOCATION_LIMIT_EXCEEDED",
        "HOURLY_ENVELOPE_POLLING_LIMIT_EXCEEDED",
        "BURST_ENVELOPE_POLLING_LIMIT_EXCEEDED",
        "HOURLY_APIINVOCATION_ENVELOPE_LIMIT_EXCEEDED",
        "BURST_APIINVOCATION_ENVELOPE_LIMIT_EXCEEDED",
    )
)
_AUTH_CODES = frozenset(
    ("USER_AUTHENTICATION_FAILED", "AUTHORIZATION_INVALID_TOKEN", "PARTNER_AUTHENTICATION_FAILED")
)
_PERMISSION_CODES = frozenset(
    (
        "ACCOUNT_LACKS_PERMISSIONS",
        "USER_LACKS_PERMISSIONS",
        "USER_DOES_NOT_BELONG_TO_SPECIFIED_ACCOUNT",
        "ACCOUNT_NOT_AUTHORIZED_FOR_ENVELOPE",
        "USER_NOT_ENVELOPE_SENDER_OR_RECIPIENT",
        "USER_LACKS_MEMBERSHIP",
        "USER_NOT_ACCOUNT_ADMIN",
    )
)
_NOT_FOUND_CODES = frozenset(("ENVELOPE_DOES_NOT_EXIST", "RESOURCE_NOT_FOUND", "DOCUMENT_DOES_NOT_EXIST"))


def is_guid(value: str) -> bool:
    """Check DocuSign's hyphenated GUID format."""
    return bool(_GUID.fullmatch(value))


def path_escape(value: str) -> str:
    """Apply Go url.PathEscape's segment character set."""
    return quote(value, safe="-._~$&+=:@")


def query_escape(value: str) -> str:
    """Apply Go url.QueryEscape, including its literal unreserved tilde."""
    return quote_plus(value, safe="~")


@dataclass(frozen=True, slots=True)
class ProviderResponse:
    """One status and its bounded response bytes."""

    status: int
    headers: httpx.Headers
    body: bytes


@dataclass(frozen=True, slots=True)
class ProviderOutcome:
    """Classified DocuSign refusal without provider message text."""

    failure: Failure
    error_code: str = ""
    retry: bool = False
    not_found: bool = False
    retry_after: float = 0


@dataclass(slots=True)
class Session:
    """Resolved account and regional API base for one call."""

    account_id: str
    base_uri: str
    credentials: Credentials


class SessionFailure(Exception):
    """Account resolution failure classified before an operation request."""

    def __init__(self, category: str, failure: Failure, retry_after: float = 0) -> None:
        self.category = category
        self.failure = failure
        self.retry_after = retry_after
        super().__init__(failure.message)


class CombinedDocumentStore(Protocol):
    """Persist a bounded PDF outside Dex and return a safe location.

    Implementations must replace or keep repeated writes atomically. They must
    return a location of at most 1024 UTF-8 bytes without credentials or a signed URL.
    The connector owns the bounded bytes and never places them in a Dex Result.
    """

    async def store_combined_document(
        self, account_id: str, envelope_id: str, includes_certificate: bool, content: bytes
    ) -> str:
        """Store one combined PDF and return its durable location."""
        ...


def failure(operation: str, kind: FailureKind, message: str) -> Failure:
    """Create a secret free DocuSign failure."""
    return Failure(kind, "docusign", operation, message)


def _strict_object(body: bytes) -> dict[str, Any]:
    try:
        value = json.loads(body, parse_constant=lambda token: _reject_json_constant(token))
    except (ValueError, UnicodeDecodeError) as error:
        raise ValueError("DocuSign returned a malformed response") from error
    if not isinstance(value, dict):
        raise ValueError("DocuSign returned a malformed response")
    return value


def _reject_json_constant(token: str) -> None:
    raise ValueError("DocuSign returned a malformed response")


def _string(value: dict[str, Any], name: str) -> str:
    result = value.get(name, "")
    if result is None:
        return ""
    if not isinstance(result, str):
        raise ValueError("DocuSign returned a malformed response")
    return result


class Client:
    """Call the account-selected DocuSign API through one credential source.

    The client owns its transport, including an injected one. Close it after its Steps stop.
    """

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Callable[[], datetime] | None = None,
        document_store: CombinedDocumentStore | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("DocuSign configuration and credentials are required")
        if not isinstance(config.account_id, str):
            raise ValueError("DocuSign accountId must be text")
        if config.account_id and not is_guid(config.account_id):
            raise ValueError("configuration accountId must be a DocuSign API account ID GUID")
        limits = (config.max_response_bytes, config.max_document_bytes, config.connect_max_body_bytes)
        if any(type(limit) is not int or limit < 0 for limit in limits):
            raise ValueError("DocuSign response and document limits cannot be negative")
        defaults = Config()
        self.config = Config(
            account_id=config.account_id,
            max_response_bytes=config.max_response_bytes or defaults.max_response_bytes,
            max_document_bytes=config.max_document_bytes or defaults.max_document_bytes,
            connect_max_body_bytes=config.connect_max_body_bytes or defaults.connect_max_body_bytes,
        )
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(115)
        self.now = now or (lambda: datetime.now(UTC))
        self.document_store = document_store
        self._accounts: dict[tuple[str, str], tuple[str, str, datetime]] = {}

    async def aclose(self) -> None:
        """Close the owned provider and OAuth transport."""
        await self.http_client.aclose()

    def create_envelope_from_template(self) -> CreateEnvelopeFromTemplateOperation:
        """Bind the guarded create mutation."""
        from .operations import CreateEnvelopeFromTemplateOperation

        return CreateEnvelopeFromTemplateOperation(self)

    def get_envelope(self) -> GetEnvelopeOperation:
        """Bind the envelope query."""
        from .operations import GetEnvelopeOperation

        return GetEnvelopeOperation(self)

    def list_envelope_recipients(self) -> ListEnvelopeRecipientsOperation:
        """Bind the recipient query."""
        from .operations import ListEnvelopeRecipientsOperation

        return ListEnvelopeRecipientsOperation(self)

    def void_envelope(self) -> VoidEnvelopeOperation:
        """Bind the safe repeatable void mutation."""
        from .operations import VoidEnvelopeOperation

        return VoidEnvelopeOperation(self)

    def download_combined_document(self) -> DownloadCombinedDocumentOperation:
        """Bind the combined document digest query."""
        from .operations import DownloadCombinedDocumentOperation

        return DownloadCombinedDocumentOperation(self)

    def receipt(self, envelope_id: str = "", error_code: str = "") -> Receipt:
        """Record provider identity without secret material."""
        return Receipt(
            provider="docusign",
            observed_at=self.now(),
            provider_object_id=envelope_id,
            metadata={"errorCode": error_code} if error_code else {},
        )

    async def open_session(self, call: Call, operation: str) -> Session:
        """Resolve the account from userinfo and the current OAuth access token."""
        try:
            credentials = await self.credentials.resolve(call)
        except Exception as error:
            from dex_connectors.sdk import ReauthorizationRequiredError

            if isinstance(error, ReauthorizationRequiredError):
                raise SessionFailure(
                    "defect",
                    failure(
                        operation,
                        FailureKind.AUTHENTICATION,
                        "the DocuSign connection needs reauthorization; "
                        "authorize it again in Dex Web Connections",
                    ),
                ) from None
            raise SessionFailure(
                "retry",
                failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "the DocuSign access token could not be loaded or refreshed yet",
                ),
            ) from None
        if credentials.access_token is None or not is_header_safe_credential(
            credentials.access_token.reveal()
        ):
            raise SessionFailure(
                "defect",
                failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "the DocuSign access token is blank or contains characters a header cannot carry",
                ),
            )
        method = credentials.auth_method_id or "docusign-oauth"
        if method not in ("docusign-oauth", "docusign-developer-oauth"):
            raise SessionFailure(
                "defect",
                failure(
                    operation,
                    FailureKind.LOCAL_DEFECT,
                    "the DocuSign connection names no known environment; "
                    "authorize it again in Dex Web Connections",
                ),
            )
        token_digest = hashlib.sha256(credentials.access_token.reveal().encode()).hexdigest()
        cache_key = (call.connection.name, token_digest)
        cached = self._accounts.get(cache_key)
        if cached is not None and self.now() - cached[2] < timedelta(hours=1):
            return Session(cached[0], cached[1], credentials)
        account_server = (
            "https://account-d.docusign.com"
            if method == "docusign-developer-oauth"
            else "https://account.docusign.com"
        )
        try:
            response, credentials = await self._send(
                call,
                credentials,
                "GET",
                account_server + "/oauth/userinfo",
                None,
                "application/json",
                self.config.max_response_bytes,
                25,
            )
        except BodyTooLargeError:
            raise SessionFailure(
                "invalidResponse",
                failure(
                    operation,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "DocuSign account information exceeds the configured maxResponseBytes",
                ),
            ) from None
        except ProviderTransportError:
            raise SessionFailure(
                "retry",
                failure(operation, FailureKind.TRANSPORT, "DocuSign account information could not be read"),
            ) from None
        if response.status == 429:
            delay = (
                self.now().replace(minute=0, second=0, microsecond=0) + timedelta(hours=1) - self.now()
            ).total_seconds()
            raise SessionFailure(
                "retry",
                failure(
                    operation,
                    FailureKind.RATE_LIMIT,
                    "DocuSign limited the hourly account information requests "
                    "for this user or integration key",
                ),
                delay,
            )
        if response.status >= 500 or response.status == 408:
            raise SessionFailure(
                "retry",
                failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    f"DocuSign account information answered HTTP {response.status}",
                ),
            )
        if response.status == 401:
            raise SessionFailure(
                "rejected",
                failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "DocuSign rejected the access token; authorize the connection again",
                ),
            )
        if response.status != 200:
            raise SessionFailure(
                "rejected",
                failure(
                    operation,
                    FailureKind.PROVIDER_REJECTION,
                    f"DocuSign account information answered HTTP {response.status}",
                ),
            )
        try:
            decoded = _strict_object(response.body)
            accounts = decoded.get("accounts")
            if not isinstance(accounts, list) or not accounts:
                raise ValueError("account information lists no accounts")
            selected: dict[str, Any] | None = None
            for entry in accounts:
                if not isinstance(entry, dict):
                    raise ValueError("account information lists no accounts")
                account_id = _string(entry, "account_id")
                default_value = entry.get("is_default")
                is_default = default_value is True or default_value == "true"
                if (not self.config.account_id and is_default) or (
                    self.config.account_id and account_id.lower() == self.config.account_id.lower()
                ):
                    selected = entry
                    break
            if selected is None:
                message = (
                    "the authorizing DocuSign user does not belong to the configured accountId; "
                    "correct accountId or authorize as a member of that account"
                    if self.config.account_id
                    else "DocuSign reports no default account for the authorizing user; "
                    "set the connection's accountId"
                )
                raise SessionFailure("defect", failure(operation, FailureKind.VALIDATION, message))
            account_id = _string(selected, "account_id")
            base_uri = _string(selected, "base_uri")
            parsed = urlsplit(base_uri)
            host = parsed.hostname or ""
            valid_host = (
                host == "demo.docusign.net"
                if method == "docusign-developer-oauth"
                else host.endswith(".docusign.net")
                and host != "demo.docusign.net"
                and bool(_HOST_LABEL.fullmatch(host.removesuffix(".docusign.net")))
            )
            if (
                not is_guid(account_id)
                or parsed.scheme != "https"
                or parsed.netloc != host
                or parsed.path not in ("", "/")
                or parsed.query
                or parsed.fragment
                or not valid_host
            ):
                raise ValueError("the account base URI is not a DocuSign eSignature host of this environment")
        except SessionFailure:
            raise
        except (TypeError, ValueError) as error:
            raise SessionFailure(
                "invalidResponse", failure(operation, FailureKind.PROTOCOL, str(error))
            ) from None
        active_token = credentials.access_token.reveal() if credentials.access_token else ""
        active_key = (call.connection.name, hashlib.sha256(active_token.encode()).hexdigest())
        self._accounts[active_key] = (account_id.lower(), "https://" + host, self.now())
        return Session(account_id.lower(), "https://" + host, credentials)

    async def request(
        self,
        call: Call,
        session: Session,
        method: str,
        path: str,
        query: dict[str, str] | None = None,
        body: dict[str, Any] | None = None,
        accept: str = "application/json",
        maximum_bytes: int | None = None,
        timeout: float = 25,
    ) -> ProviderResponse:
        """Send one account API request with Go-compatible URL and body fields."""
        url = session.base_uri + "/restapi/v2.1/accounts/" + path_escape(session.account_id) + path
        if query:
            url += "?" + "&".join(
                query_escape(key) + "=" + query_escape(value) for key, value in sorted(query.items())
            )
        encoded = (
            json.dumps(body, ensure_ascii=False, separators=(",", ":")).encode() if body is not None else None
        )
        response, replacement = await self._send(
            call,
            session.credentials,
            method,
            url,
            encoded,
            accept,
            maximum_bytes if maximum_bytes is not None else self.config.max_response_bytes,
            timeout,
        )
        session.credentials = replacement
        return response

    async def _send(
        self,
        call: Call,
        credentials: Credentials,
        method: str,
        url: str,
        body: bytes | None,
        accept: str,
        maximum_bytes: int,
        timeout: float,
    ) -> tuple[ProviderResponse, Credentials]:
        for attempt in range(2):
            headers = {
                "Authorization": "Bearer "
                + (credentials.access_token.reveal() if credentials.access_token else ""),
                "Accept": accept,
            }
            if body is not None:
                headers["Content-Type"] = "application/json"
            request = self.http_client.build_request(
                method, url, headers=headers, content=body, timeout=timeout
            )
            response, _trace = await send_traced(self.http_client, request)
            if 200 <= response.status_code < 300 and accept == "application/pdf":
                media_type = response.headers.get("Content-Type", "").split(";", 1)[0].strip().lower()
                if media_type != "application/pdf":
                    await response.aclose()
                    return ProviderResponse(response.status_code, response.headers, b""), credentials
            if (
                response.status_code == 401
                and attempt == 0
                and hasattr(self.credentials, "resolve_after_rejection")
            ):
                try:
                    rejected_body = await read_bounded_body(response, MAX_ERROR_BODY_BYTES)
                except BodyTooLargeError:
                    rejected_body = b""
                rejected = ProviderResponse(401, response.headers, rejected_body)
                try:
                    replacement = await self.credentials.resolve_after_rejection(call)
                except Exception:
                    return rejected, credentials
                if replacement.access_token and is_header_safe_credential(replacement.access_token.reveal()):
                    credentials = replacement
                    continue
                return rejected, credentials
            try:
                bounded = await read_bounded_body(
                    response, maximum_bytes if 200 <= response.status_code < 300 else MAX_ERROR_BODY_BYTES
                )
            except BodyTooLargeError:
                if 200 <= response.status_code < 300:
                    raise
                bounded = b""
            except ProviderTransportError:
                if 200 <= response.status_code < 300:
                    raise
                bounded = b""
            return ProviderResponse(response.status_code, response.headers, bounded), credentials
        raise RuntimeError("DocuSign authenticated request retry was exhausted")

    def classify(self, operation: str, response: ProviderResponse) -> ProviderOutcome:
        """Classify DocuSign errorCode before HTTP status, excluding message text."""
        tokens = read_error_tokens(response.body, ("/errorCode", "/error"))
        code = tokens[0] if tokens else ""
        upper = code.upper()
        status = response.status
        suffix = f" ({code})" if code else ""
        if upper in _RATE_CODES or status == 429:
            if upper.startswith("BURST_"):
                delay = 30.0
            elif response.headers.get("X-RateLimit-Reset", "").strip().isdigit():
                reset = int(response.headers["X-RateLimit-Reset"])
                delay = max(0.0, min(3600.0, reset - self.now().timestamp())) if reset > 0 else 0.0
            else:
                delay = parse_retry_after(response.headers.get("Retry-After", ""), self.now())
                if delay == 0 and upper.startswith("HOURLY_"):
                    delay = (
                        self.now().replace(minute=0, second=0, microsecond=0)
                        + timedelta(hours=1)
                        - self.now()
                    ).total_seconds()
            return ProviderOutcome(
                failure(operation, FailureKind.RATE_LIMIT, "DocuSign rate limited the request" + suffix),
                code,
                True,
                False,
                delay,
            )
        if status >= 500 or status == 408:
            return ProviderOutcome(
                failure(operation, FailureKind.AVAILABILITY, f"DocuSign answered HTTP {status}" + suffix),
                code,
                True,
            )
        if upper == "ENVELOPE_ALLOWANCE_EXCEEDED":
            kind, message = FailureKind.QUOTA_EXHAUSTED, "the DocuSign account has no envelope allowance left"
        elif status == 401 or upper in _AUTH_CODES:
            kind, message = (
                FailureKind.AUTHENTICATION,
                "DocuSign rejected the access token; authorize the connection again",
            )
        elif status == 403 or upper in _PERMISSION_CODES:
            kind, message = (
                FailureKind.AUTHORIZATION,
                "DocuSign denied permission for this account, user, or envelope",
            )
        elif status == 404 or upper in _NOT_FOUND_CODES:
            return ProviderOutcome(
                failure(
                    operation,
                    FailureKind.NOT_FOUND,
                    "the DocuSign envelope or document was not found" + suffix,
                ),
                code,
                False,
                True,
            )
        else:
            kind, message = (
                FailureKind.PROVIDER_REJECTION,
                f"DocuSign rejected the request with HTTP {status}",
            )
        return ProviderOutcome(failure(operation, kind, message + suffix), code)
