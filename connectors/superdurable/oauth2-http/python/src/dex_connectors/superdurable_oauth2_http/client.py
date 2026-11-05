# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""OAuth bearer HTTP transport and Go-compatible sendRequest outcomes."""

import asyncio
import base64
import re
from collections.abc import Callable
from dataclasses import dataclass, field
from datetime import UTC, datetime
from urllib.parse import parse_qsl, unquote, urlencode, urlsplit, urlunsplit

import httpx
from dex_connectors.sdk import (
    Call,
    CallID,
    CredentialProvider,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    ReauthorizationRequiredError,
    Receipt,
    resolve_credential,
    resolve_credential_after_rejection,
)
from dex_connectors.sdk.oauthtoken import ManifestCredentialRefreshDriver
from dex_connectors.sdk.providerhttp import (
    AddressRefusal,
    ProviderTransportError,
    parse_retry_after,
    send_traced,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import SEND_REQUEST_DEFINITION
from ._generated.refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from .models import SendRequestInput, SendRequestOutput

_HEADER_NAME = re.compile(r"^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")
_REQUEST_ID = re.compile(r"^[A-Za-z0-9._:-]{1,128}$")
_RESERVED = frozenset(
    {
        "authorization",
        "proxy-authorization",
        "host",
        "content-length",
        "transfer-encoding",
        "connection",
        "keep-alive",
        "te",
        "trailer",
        "upgrade",
        "cookie",
    }
)
_SAFE_METHODS = frozenset({"GET", "HEAD", "OPTIONS"})
_METHODS = _SAFE_METHODS | {"POST", "PUT", "PATCH", "DELETE"}
_USER_AGENT = "superdurable-dex-oauth2-http-connector/0.1"


@dataclass(frozen=True, slots=True)
class _RequestPlan:
    method: str
    url: str
    headers: dict[str, str]
    body: str
    idempotency_key_header: str

    @property
    def is_safe_to_repeat(self) -> bool:
        return self.method in _SAFE_METHODS


@dataclass(frozen=True, slots=True)
class _Exchange:
    status: int = 0
    headers: httpx.Headers = field(default_factory=httpx.Headers)
    body: bytes = b""
    is_truncated: bool = False
    error: bool = False
    is_dispatched: bool = False
    refusal: AddressRefusal | None = None


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "oauth2-http", "sendRequest", message)


def _branch(
    branch: str, output: SendRequestOutput, kind: FailureKind, message: str, receipt: Receipt | None = None
) -> MutationAttempt[SendRequestOutput]:
    return MutationAttempt.branch_result(branch, output, receipt or Receipt(), _failure(kind, message))


def _parse_http_url(value: str) -> tuple[str, str, int, str, str]:
    if not value or any(ord(char) <= 32 or ord(char) > 126 for char in value) or "#" in value:
        raise ValueError("URL must be printable ASCII without spaces and cannot contain a fragment")
    try:
        parts = urlsplit(value)
        if not value.startswith(("http://", "https://")) or not parts.hostname or parts.username is not None:
            raise ValueError
        port = parts.port or (80 if parts.scheme == "http" else 443)
    except ValueError:
        raise ValueError(
            "URL must be an absolute http or https URL with a host and no user information"
        ) from None
    return parts.scheme, parts.hostname.lower(), port, parts.path, parts.query


def _validate_header_name(field: str, name: str) -> None:
    folded = name.lower()
    if not _HEADER_NAME.fullmatch(name):
        raise ValueError(f"sendRequest {field} names must be RFC 7230 tokens")
    if folded in _RESERVED or folded.startswith("proxy-"):
        raise ValueError(f"sendRequest {field} cannot name Authorization or a header the transport owns")


def _encode_go_query(values: list[tuple[str, str]]) -> str:
    """Match url.Values.Encode: byte-sorted keys and QueryEscape's space-plus encoding."""
    ordered_values = sorted(values, key=lambda item: item[0].encode("utf-8"))
    return urlencode(ordered_values)


def _plan_request(api_base_url: str, max_body_bytes: int, input: SendRequestInput) -> _RequestPlan:
    method = input.method.strip().upper() or "GET"
    if method not in _METHODS:
        raise ValueError("sendRequest method must be GET, HEAD, OPTIONS, POST, PUT, PATCH, or DELETE")
    if bool(input.path) == bool(input.url):
        raise ValueError("sendRequest needs exactly one of path and url")
    if input.path:
        path = input.path
        if any(ord(char) <= 32 or ord(char) > 126 for char in path):
            raise ValueError("sendRequest path must be printable ASCII without spaces")
        if not path.startswith("/") or path.startswith("//"):
            raise ValueError("sendRequest path must start with a single slash")
        if any(char in path for char in "?#\\"):
            raise ValueError("sendRequest path cannot contain ?, #, or a backslash; use query for parameters")
        if re.search(r"%(?![0-9A-Fa-f]{2})", path):
            raise ValueError("sendRequest path has invalid percent-encoding")
        if ".." in unquote(path).split("/"):
            raise ValueError("sendRequest path cannot contain a .. segment")
        target = api_base_url + path
    else:
        target = input.url
    try:
        target_origin = _parse_http_url(target)[:3]
    except ValueError as error:
        raise ValueError(f"sendRequest url: {error}") from None
    if target_origin != _parse_http_url(api_base_url)[:3]:
        raise ValueError(
            "sendRequest url must have the scheme, host, and port of the connection's apiBaseUrl"
        )
    if input.query:
        parts = urlsplit(target)
        values = parse_qsl(parts.query, keep_blank_values=True)
        for name, entries in input.query.items():
            if not name:
                raise ValueError("sendRequest query parameter names cannot be empty")
            values.extend((name, value) for value in entries)
        target = urlunsplit(
            (parts.scheme, parts.netloc, parts.path, _encode_go_query(values), parts.fragment)
        )
    headers: dict[str, str] = {}
    for name, value in input.headers.items():
        _validate_header_name("headers", name)
        if name.lower() in (key.lower() for key in headers):
            raise ValueError("sendRequest header names must be unique ignoring case")
        if len(value.encode("utf-8")) > 8192:
            raise ValueError("sendRequest header values are limited to 8192 bytes")
        if any(ord(char) < 32 and char != "\t" or ord(char) == 127 for char in value):
            raise ValueError("sendRequest header values cannot contain control characters")
        headers[name] = value
    if input.idempotency_key_header:
        _validate_header_name("idempotencyKeyHeader", input.idempotency_key_header)
    if input.body:
        if method in {"GET", "HEAD"}:
            raise ValueError("sendRequest body is not allowed with GET or HEAD")
        if len(input.body.encode("utf-8")) > max_body_bytes:
            raise ValueError("sendRequest body exceeds the connection's maxBodyBytes")
        if not any(key.lower() == "content-type" and value for key, value in headers.items()):
            headers["Content-Type"] = "application/json"
    return _RequestPlan(method, target, headers, input.body, input.idempotency_key_header)


def _output(exchange: _Exchange, tokens: list[str]) -> SendRequestOutput:
    content_type = exchange.headers.get("Content-Type", "")
    if len(content_type.encode("utf-8")) > 256 or any(
        ord(char) < 32 or ord(char) > 126 for char in content_type
    ):
        content_type = ""
    body = exchange.body
    for token in tokens:
        if len(token.encode("utf-8")) >= 8:
            body = body.replace(token.encode("utf-8"), b"[REDACTED]")
    try:
        text = body.decode("utf-8")
        return SendRequestOutput(exchange.status, content_type, text, False, exchange.is_truncated)
    except UnicodeDecodeError:
        return SendRequestOutput(
            exchange.status, content_type, base64.b64encode(body).decode("ascii"), True, exchange.is_truncated
        )


class Client:
    """One OAuth-protected API client with a manifest generated refresh driver."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient,
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if credentials is None or http_client is None:
            raise ValueError("OAuth 2.0 HTTP credentials and HTTP client are required")
        _parse_http_url(config.authorization_endpoint)
        _parse_http_url(config.token_endpoint)
        if not config.scopes:
            raise ValueError("OAuth 2.0 HTTP scopes are required")
        _parse_http_url(config.api_base_url)
        if "?" in config.api_base_url:
            raise ValueError("OAuth 2.0 HTTP apiBaseUrl cannot contain a query")
        if config.max_body_bytes < 1 or config.request_timeout_seconds < 1:
            raise ValueError("OAuth 2.0 HTTP maxBodyBytes and requestTimeout must be positive")
        self._api_base_url = config.api_base_url.rstrip("/")
        self._max_body_bytes = config.max_body_bytes
        self._request_timeout_seconds = config.request_timeout_seconds
        self._credentials = credentials
        self._http_client = http_client
        self._now = now or (lambda: datetime.now(UTC))
        self._refresh_driver = ManifestCredentialRefreshDriver[Credentials](
            "OAuth 2.0 HTTP",
            AUTHORIZATION,
            CREDENTIAL_FIELDS,
            http_client,
            settings={
                "authorizationEndpoint": config.authorization_endpoint,
                "tokenEndpoint": config.token_endpoint,
                "scopes": " ".join(config.scopes),
                "tokenEndpointAuthMethod": config.token_endpoint_auth_method,
                "apiBaseUrl": config.api_base_url,
            },
            now=self._now,
        )

    def send_request(self) -> "SendRequestOperation":
        """Bind the sendRequest Mutation."""
        return SendRequestOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client."""
        await self._http_client.aclose()

    async def _exchange(self, call: Call, request: _RequestPlan, token: str) -> _Exchange:
        headers = dict(request.headers)
        headers["Authorization"] = "Bearer " + token
        if not any(name.lower() == "user-agent" for name in headers):
            headers["User-Agent"] = _USER_AGENT
        if request.idempotency_key_header:
            headers[request.idempotency_key_header] = str(call.idempotency_key)
        try:
            http_request = self._http_client.build_request(
                request.method,
                request.url,
                headers=headers,
                content=request.body.encode("utf-8") if request.body else None,
                timeout=self._request_timeout_seconds,
            )
            response, _ = await send_traced(self._http_client, http_request)
        except AddressRefusal as error:
            return _Exchange(error=True, refusal=error)
        except ProviderTransportError as error:
            return _Exchange(error=True, is_dispatched=not error.provably_not_sent)
        except (httpx.HTTPError, ValueError):
            return _Exchange(error=True)
        chunks: list[bytes] = []
        length = 0
        is_truncated = False
        try:
            if response.is_stream_consumed:
                chunks.append(response.content)
                length = len(response.content)
                is_truncated = length > self._max_body_bytes
            else:
                async for chunk in response.aiter_raw():
                    chunks.append(chunk)
                    length += len(chunk)
                    if length > self._max_body_bytes:
                        is_truncated = True
                        break
        except httpx.HTTPError:
            return _Exchange(
                response.status_code, response.headers, b"".join(chunks), error=True, is_dispatched=True
            )
        finally:
            await response.aclose()
        body = b"".join(chunks)
        if is_truncated:
            body = body[: self._max_body_bytes]
            while body:
                try:
                    body.decode("utf-8")
                    break
                except UnicodeDecodeError as error:
                    if error.reason != "unexpected end of data":
                        break
                    body = body[:-1]
        return _Exchange(response.status_code, response.headers, body, is_truncated, is_dispatched=True)


class SendRequestOperation:
    """The sendRequest Mutation bound to one OAuth HTTP client."""

    def __init__(self, client: Client) -> None:
        self._client = client

    def definition(self) -> MutationDefinition:
        """Return the manifest's immutable operation definition."""
        return SEND_REQUEST_DEFINITION

    def idempotency_key(self, call_id: CallID, input: SendRequestInput) -> IdempotencyKey:
        """Use the Call ID for each retry of one Step."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: SendRequestInput) -> MutationAttempt[SendRequestOutput]:
        """Classify one provider attempt, refreshing after one HTTP 401 when possible."""
        try:
            request = _plan_request(self._client._api_base_url, self._client._max_body_bytes, input)
        except ValueError as error:
            return _branch("defect", SendRequestOutput(), FailureKind.VALIDATION, str(error))
        try:
            async with asyncio.timeout(25):
                credentials = await resolve_credential(
                    self._client._credentials, call, self._client._refresh_driver
                )
                if (
                    not credentials.oauth_client_id
                    or not credentials.oauth_client_secret.reveal()
                    or not credentials.access_token.reveal()
                ):
                    raise ValueError("unusable credentials")
                answer = await self._client._exchange(call, request, credentials.access_token.reveal())
                used_tokens = [credentials.access_token.reveal()]
                if answer.status == 401 and not answer.error:
                    try:
                        replacement = await resolve_credential_after_rejection(
                            self._client._credentials, call, self._client._refresh_driver
                        )
                        next_token = replacement.access_token.reveal()
                        if next_token and next_token != used_tokens[0]:
                            answer = await self._client._exchange(call, request, next_token)
                            used_tokens.append(next_token)
                    except Exception:
                        pass
        except AddressRefusal as error:
            return _branch(
                "defect",
                SendRequestOutput(),
                FailureKind.VALIDATION,
                "the connection's tokenEndpoint resolves to an address the connector will not connect to: "
                + error.reason,
            )
        except ReauthorizationRequiredError:
            return _branch(
                "rejected",
                SendRequestOutput(),
                FailureKind.AUTHENTICATION,
                "the OAuth authorization must be renewed in Dex Web Connections",
            )
        except (TimeoutError, asyncio.CancelledError):
            return MutationAttempt.retry(
                _failure(FailureKind.AVAILABILITY, "the OAuth access token could not be refreshed now")
            )
        except Exception:
            return _branch(
                "defect",
                SendRequestOutput(),
                FailureKind.AUTHENTICATION,
                "the connection's credentials are unavailable or unusable",
            )
        return self._attempt_for(call, request, answer, used_tokens)

    def _attempt_for(
        self, call: Call, request: _RequestPlan, answer: _Exchange, used_tokens: list[str]
    ) -> MutationAttempt[SendRequestOutput]:
        if answer.refusal is not None:
            return _branch(
                "defect",
                SendRequestOutput(),
                FailureKind.VALIDATION,
                "the connection's apiBaseUrl resolves to an address the connector will not connect to: "
                + answer.refusal.reason,
            )
        request_id = answer.headers.get("X-Request-Id", "").strip()
        if not _REQUEST_ID.fullmatch(request_id):
            request_id = ""
        receipt = Receipt(call.id, "oauth2-http", self._client._now(), call.idempotency_key, "", request_id)
        output = _output(answer, used_tokens)
        if answer.error:
            if not answer.is_dispatched:
                return MutationAttempt.retry(
                    _failure(FailureKind.AVAILABILITY, "the API could not be reached; no request was sent")
                )
            if request.is_safe_to_repeat:
                return MutationAttempt.retry(
                    _failure(
                        FailureKind.TRANSPORT, "the API did not answer the " + request.method + " request"
                    )
                )
            return MutationAttempt.uncertain(
                output,
                _failure(FailureKind.TRANSPORT, "the request was sent but the API did not answer completely"),
                receipt,
            )
        status = answer.status
        if 200 <= status < 300:
            return MutationAttempt.branch_result("completed", output, receipt)
        if status in (408, 429):
            return MutationAttempt.retry(
                _failure(FailureKind.RATE_LIMIT, f"the API answered HTTP {status}"),
                parse_retry_after(answer.headers.get("Retry-After", ""), self._client._now()),
            )
        if status >= 500:
            if request.is_safe_to_repeat:
                return MutationAttempt.retry(
                    _failure(FailureKind.AVAILABILITY, f"the API answered HTTP {status}"),
                    parse_retry_after(answer.headers.get("Retry-After", ""), self._client._now()),
                )
            return MutationAttempt.uncertain(
                output,
                _failure(
                    FailureKind.AVAILABILITY, f"the API answered HTTP {status} after the request was sent"
                ),
                receipt,
            )
        kind = {
            401: FailureKind.AUTHENTICATION,
            403: FailureKind.AUTHORIZATION,
            404: FailureKind.NOT_FOUND,
            410: FailureKind.NOT_FOUND,
            409: FailureKind.CONFLICT,
            400: FailureKind.VALIDATION,
            422: FailureKind.VALIDATION,
        }.get(status, FailureKind.PROVIDER_REJECTION)
        return _branch("rejected", output, kind, f"the API answered HTTP {status}", receipt)
