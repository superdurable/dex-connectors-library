# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""OAuth bearer HTTP transport and Go-compatible sendRequest and postNonIdempotent outcomes."""

import asyncio
import base64
import re
from collections.abc import Callable
from dataclasses import dataclass, field
from datetime import UTC, datetime
from urllib.parse import unquote, unquote_plus, urlencode, urlsplit, urlunsplit

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
    RequestTrace,
    parse_retry_after,
)
from dex_connectors.sdk.singledispatch import (
    CheckpointError,
    DecisionKind,
    DispatchNotAllowedError,
    Fence,
    InsufficientAttemptTimeError,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import POST_NON_IDEMPOTENT_DEFINITION, SEND_REQUEST_DEFINITION
from ._generated.refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from .models import PostNonIdempotentInput, SendRequestInput, SendRequestOutput

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
_METHODS = frozenset({"GET", "HEAD", "OPTIONS", "POST", "PUT", "PATCH", "DELETE"})
# Only these statuses prove the API refused a request without processing it.
_DEFINITE_REFUSALS = frozenset({400, 401, 403, 404, 411, 413, 414, 415, 422})
_USER_AGENT = "superdurable-dex-oauth2-http-connector/0.1"
_SEND_REQUEST = "sendRequest"
_POST_NON_IDEMPOTENT = "postNonIdempotent"
# The POST gets most of the Execute budget, because no read-back follows it.
_POST_WRITE_WINDOW = 5 / 6
_OPERATION_SECONDS = 25


@dataclass(frozen=True, slots=True)
class _RequestPlan:
    method: str
    url: str
    headers: dict[str, str]
    body: str
    idempotency_key_header: str


@dataclass(frozen=True, slots=True)
class _Exchange:
    status: int = 0
    headers: httpx.Headers = field(default_factory=httpx.Headers)
    body: bytes = b""
    is_truncated: bool = False
    error: bool = False
    is_dispatched: bool = False
    refusal: AddressRefusal | None = None


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "oauth2-http", operation, message)


def _branch(
    operation: str,
    branch: str,
    output: SendRequestOutput,
    kind: FailureKind,
    message: str,
    receipt: Receipt | None = None,
) -> MutationAttempt[SendRequestOutput]:
    return MutationAttempt.branch_result(
        branch, output, receipt or Receipt(), _failure(operation, kind, message)
    )


def _rejection_kind(status: int) -> FailureKind:
    return {
        401: FailureKind.AUTHENTICATION,
        403: FailureKind.AUTHORIZATION,
        404: FailureKind.NOT_FOUND,
        410: FailureKind.NOT_FOUND,
        409: FailureKind.CONFLICT,
        400: FailureKind.VALIDATION,
        422: FailureKind.VALIDATION,
    }.get(status, FailureKind.PROVIDER_REJECTION)


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


def _validate_header_name(operation: str, field: str, name: str) -> None:
    folded = name.lower()
    if not _HEADER_NAME.fullmatch(name):
        raise ValueError(f"{operation} {field} names must be RFC 7230 tokens")
    if folded in _RESERVED or folded.startswith("proxy-"):
        raise ValueError(f"{operation} {field} cannot name Authorization or a header the transport owns")


def _encode_go_query(values: list[tuple[str, str]]) -> str:
    """Match url.Values.Encode: byte-sorted keys and QueryEscape's space-plus encoding."""
    ordered_values = sorted(values, key=lambda item: item[0].encode("utf-8"))
    return urlencode(ordered_values)


def _parse_go_query(query: str) -> list[tuple[str, str]]:
    values: list[tuple[str, str]] = []
    for pair in query.split("&"):
        if not pair:
            continue
        if ";" in pair or re.search(r"%(?![0-9A-Fa-f]{2})", pair):
            continue
        name, _, value = pair.partition("=")
        values.append((unquote_plus(name), unquote_plus(value)))
    return values


def _plan_request(
    operation: str, api_base_url: str, max_body_bytes: int, input: SendRequestInput
) -> _RequestPlan:
    method = input.method.strip().upper() or "GET"
    if method not in _METHODS:
        raise ValueError(f"{operation} method must be GET, HEAD, OPTIONS, POST, PUT, PATCH, or DELETE")
    if bool(input.path) == bool(input.url):
        raise ValueError(f"{operation} needs exactly one of path and url")
    if input.path:
        path = input.path
        if any(ord(char) <= 32 or ord(char) > 126 for char in path):
            raise ValueError(f"{operation} path must be printable ASCII without spaces")
        if not path.startswith("/") or path.startswith("//"):
            raise ValueError(f"{operation} path must start with a single slash")
        if any(char in path for char in "?#\\"):
            raise ValueError(
                f"{operation} path cannot contain ?, #, or a backslash; use query for parameters"
            )
        if re.search(r"%(?![0-9A-Fa-f]{2})", path):
            raise ValueError(f"{operation} path has invalid percent-encoding")
        if ".." in unquote(path).split("/"):
            raise ValueError(f"{operation} path cannot contain a .. segment")
        target = api_base_url + path
    else:
        target = input.url
    try:
        target_origin = _parse_http_url(target)[:3]
    except ValueError as error:
        raise ValueError(f"{operation} url: {error}") from None
    if target_origin != _parse_http_url(api_base_url)[:3]:
        raise ValueError(
            f"{operation} url must have the scheme, host, and port of the connection's apiBaseUrl"
        )
    if input.query:
        parts = urlsplit(target)
        values = _parse_go_query(parts.query)
        for name, entries in input.query.items():
            if not name:
                raise ValueError(f"{operation} query parameter names cannot be empty")
            values.extend((name, value) for value in entries)
        target = urlunsplit(
            (parts.scheme, parts.netloc, parts.path, _encode_go_query(values), parts.fragment)
        )
    headers: dict[str, str] = {}
    for name, value in input.headers.items():
        _validate_header_name(operation, "headers", name)
        if name.lower() in (key.lower() for key in headers):
            raise ValueError(f"{operation} header names must be unique ignoring case")
        if len(value.encode("utf-8")) > 8192:
            raise ValueError(f"{operation} header values are limited to 8192 bytes")
        if any(ord(char) < 32 and char != "\t" or ord(char) == 127 for char in value):
            raise ValueError(f"{operation} header values cannot contain control characters")
        headers[name] = value
    if input.idempotency_key_header:
        _validate_header_name(operation, "idempotencyKeyHeader", input.idempotency_key_header)
    if input.body:
        if method in {"GET", "HEAD"}:
            raise ValueError(f"{operation} body is not allowed with GET or HEAD")
        if len(input.body.encode("utf-8")) > max_body_bytes:
            raise ValueError(f"{operation} body exceeds the connection's maxBodyBytes")
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
                "requestTimeout": f"{config.request_timeout_seconds}s",
            },
            now=self._now,
        )

    def send_request(self) -> "SendRequestOperation":
        """Bind the sendRequest Mutation."""
        return SendRequestOperation(self)

    def post_non_idempotent(self) -> "PostNonIdempotentOperation":
        """Bind the postNonIdempotent Mutation."""
        return PostNonIdempotentOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client."""
        await self._http_client.aclose()

    async def _exchange(self, call: Call, request: _RequestPlan, token: str, deadline: float) -> _Exchange:
        headers = dict(request.headers)
        headers["Authorization"] = "Bearer " + token
        if not any(name.lower() == "user-agent" for name in headers):
            headers["User-Agent"] = _USER_AGENT
        if request.idempotency_key_header:
            headers[request.idempotency_key_header] = str(call.idempotency_key)
        trace = RequestTrace()
        response: httpx.Response | None = None
        chunks: list[bytes] = []
        length = 0
        is_truncated = False
        try:
            http_request = self._http_client.build_request(
                request.method,
                request.url,
                headers=headers,
                content=request.body.encode("utf-8") if request.body else None,
                timeout=self._request_timeout_seconds,
            )
            http_request.extensions["trace"] = trace.observe
            async with asyncio.timeout_at(deadline):
                response = await self._http_client.send(http_request, follow_redirects=False, stream=True)
                if response.is_stream_consumed:
                    chunks.append(response.content)
                    length = len(response.content)
                    is_truncated = length > self._max_body_bytes
                else:
                    async for chunk in response.aiter_bytes():
                        chunks.append(chunk)
                        length += len(chunk)
                        if length > self._max_body_bytes:
                            is_truncated = True
                            break
        except AddressRefusal as error:
            return _Exchange(error=True, refusal=error)
        except httpx.TransportError:
            return _Exchange(
                response.status_code if response else 0,
                response.headers if response else httpx.Headers(),
                b"".join(chunks),
                error=True,
                is_dispatched=response is not None or not trace.provably_not_sent,
            )
        except (TimeoutError, httpx.HTTPError):
            return _Exchange(
                response.status_code if response else 0,
                response.headers if response else httpx.Headers(),
                b"".join(chunks),
                error=True,
                is_dispatched=response is not None
                or (not trace.provably_not_sent and trace.request_headers_started),
            )
        except ValueError:
            return _Exchange(error=True)
        finally:
            if response is not None:
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

    def _receipt(self, call: Call, answer: _Exchange) -> Receipt:
        request_id = answer.headers.get("X-Request-Id", "").strip()
        if not _REQUEST_ID.fullmatch(request_id):
            request_id = ""
        return Receipt(call.id, "oauth2-http", self._now(), call.idempotency_key, "", request_id)

    def _retry_after(self, answer: _Exchange) -> float:
        return parse_retry_after(answer.headers.get("Retry-After", ""), self._now())

    async def _resolve_credentials(
        self, call: Call, deadline: float, operation: str
    ) -> tuple[Credentials | None, MutationAttempt[SendRequestOutput] | None]:
        """Resolve usable credentials, or return the attempt for a resolution that failed."""
        try:
            async with asyncio.timeout_at(deadline):
                credentials = await resolve_credential(self._credentials, call, self._refresh_driver)
            if (
                not credentials.oauth_client_id
                or not credentials.oauth_client_secret.reveal()
                or not credentials.access_token.reveal()
            ):
                raise ValueError("unusable credentials")
            return credentials, None
        except AddressRefusal as error:
            return None, _branch(
                operation,
                "defect",
                SendRequestOutput(),
                FailureKind.VALIDATION,
                "the connection's tokenEndpoint resolves to an address the connector will not connect to: "
                + error.reason,
            )
        except ReauthorizationRequiredError:
            return None, _branch(
                operation,
                "rejected",
                SendRequestOutput(),
                FailureKind.AUTHENTICATION,
                "the OAuth authorization must be renewed in Dex Web Connections",
            )
        except TimeoutError:
            return None, MutationAttempt.retry(
                _failure(
                    operation, FailureKind.AVAILABILITY, "the OAuth access token could not be refreshed now"
                )
            )
        except Exception:
            return None, _branch(
                operation,
                "defect",
                SendRequestOutput(),
                FailureKind.AUTHENTICATION,
                "the connection's credentials are unavailable or unusable",
            )


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
        """Send the request, refreshing after one HTTP 401 when possible.

        A 2xx selects completed; a redirect or a definite refusal selects rejected. Every other
        outcome returns Retry, except that a POST without idempotency_key_header whose answer was
        lost selects uncertain.
        """
        try:
            request = _plan_request(
                _SEND_REQUEST, self._client._api_base_url, self._client._max_body_bytes, input
            )
        except ValueError as error:
            return _branch(_SEND_REQUEST, "defect", SendRequestOutput(), FailureKind.VALIDATION, str(error))
        deadline = asyncio.get_running_loop().time() + _OPERATION_SECONDS
        credentials, failed = await self._client._resolve_credentials(call, deadline, _SEND_REQUEST)
        if credentials is None:
            assert failed is not None
            return failed
        answer = await self._client._exchange(call, request, credentials.access_token.reveal(), deadline)
        used_tokens = [credentials.access_token.reveal()]
        if answer.status == 401 and not answer.error:
            try:
                async with asyncio.timeout_at(deadline):
                    replacement = await resolve_credential_after_rejection(
                        self._client._credentials, call, self._client._refresh_driver
                    )
                next_token = replacement.access_token.reveal()
                if next_token and next_token != used_tokens[0]:
                    answer = await self._client._exchange(call, request, next_token, deadline)
                    used_tokens.append(next_token)
            except Exception:
                # The original 401 remains the authoritative API answer.
                pass
        return self._attempt_for(call, request, answer, used_tokens)

    def _attempt_for(
        self, call: Call, request: _RequestPlan, answer: _Exchange, used_tokens: list[str]
    ) -> MutationAttempt[SendRequestOutput]:
        if answer.refusal is not None:
            return _branch(
                _SEND_REQUEST,
                "defect",
                SendRequestOutput(),
                FailureKind.VALIDATION,
                "the connection's apiBaseUrl resolves to an address the connector will not connect to: "
                + answer.refusal.reason,
            )
        receipt = self._client._receipt(call, answer)
        output = _output(answer, used_tokens)
        if answer.error:
            if not answer.is_dispatched:
                return MutationAttempt.retry(
                    _failure(
                        _SEND_REQUEST,
                        FailureKind.AVAILABILITY,
                        "the API could not be reached; no request was sent",
                    )
                )
            if request.method == "POST" and not request.idempotency_key_header:
                # Kept narrowly so the declared uncertain branch stays reachable; see the README.
                return MutationAttempt.uncertain(
                    output,
                    _failure(
                        _SEND_REQUEST,
                        FailureKind.TRANSPORT,
                        "the POST was sent without an idempotency header "
                        "but the API did not answer completely",
                    ),
                    receipt,
                )
            return MutationAttempt.retry(
                _failure(
                    _SEND_REQUEST,
                    FailureKind.TRANSPORT,
                    "the request was sent but the API did not answer completely",
                )
            )
        status = answer.status
        if 200 <= status < 300:
            return MutationAttempt.branch_result("completed", output, receipt)
        if 300 <= status < 400 or status in _DEFINITE_REFUSALS:
            # The connector never follows a redirect, so a redirect is reported like a refusal.
            return _branch(
                _SEND_REQUEST,
                "rejected",
                output,
                _rejection_kind(status),
                f"the API answered HTTP {status}",
                receipt,
            )
        retry_after = self._client._retry_after(answer)
        if status in (408, 429):
            return MutationAttempt.retry(
                _failure(_SEND_REQUEST, FailureKind.RATE_LIMIT, f"the API answered HTTP {status}"),
                retry_after,
            )
        if status >= 500:
            return MutationAttempt.retry(
                _failure(_SEND_REQUEST, FailureKind.AVAILABILITY, f"the API answered HTTP {status}"),
                retry_after,
            )
        return MutationAttempt.retry(
            _failure(
                _SEND_REQUEST,
                _rejection_kind(status),
                f"the API answered HTTP {status}, which does not prove the request was refused",
            ),
            retry_after,
        )


class PostNonIdempotentOperation:
    """The postNonIdempotent Mutation: one POST per Step execution under the single-dispatch fence."""

    def __init__(self, client: Client) -> None:
        self._client = client

    def definition(self) -> MutationDefinition:
        """Return the manifest's immutable operation definition."""
        return POST_NON_IDEMPOTENT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: PostNonIdempotentInput) -> IdempotencyKey:
        """Use the Call ID; the POST does not carry it, and the fence keys on it."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: PostNonIdempotentInput) -> MutationAttempt[SendRequestOutput]:
        """Send the POST at most once per Step execution.

        A 2xx selects completed. A definite refusal of the only send, or an authorization the
        server will no longer renew, selects rejected. A 429, a 503, a 401 after a renewed token,
        and a POST that never left this process return Retry. Any other outcome after a possible
        send, and any attempt after an earlier possible send, selects uncertain and never sends
        again. An unreadable checkpoint returns Retry.
        """
        client = self._client
        try:
            request = _plan_request(
                _POST_NON_IDEMPOTENT,
                client._api_base_url,
                client._max_body_bytes,
                SendRequestInput(
                    method="POST",
                    path=input.path,
                    url=input.url,
                    query=input.query,
                    headers=input.headers,
                    body=input.body,
                ),
            )
        except ValueError as error:
            return _branch(
                _POST_NON_IDEMPOTENT, "defect", SendRequestOutput(), FailureKind.VALIDATION, str(error)
            )
        try:
            fence = Fence(
                call,
                POST_NON_IDEMPOTENT_DEFINITION.step_defaults.execute_method_timeout,
                write_window_fraction=_POST_WRITE_WINDOW,
                max_sends=call.max_sends,
            )
        except ValueError:
            return _branch(
                _POST_NON_IDEMPOTENT,
                "defect",
                SendRequestOutput(),
                FailureKind.LOCAL_DEFECT,
                "the single-dispatch fence could not be constructed",
            )
        decision = await fence.decide()
        if decision.kind == DecisionKind.FAIL_CLOSED:
            return MutationAttempt.retry(
                _failure(
                    _POST_NON_IDEMPOTENT,
                    FailureKind.AVAILABILITY,
                    "the single-dispatch checkpoint could not be read; nothing was sent in this attempt",
                )
            )
        if decision.kind == DecisionKind.READ_BACK_REQUIRED:
            return MutationAttempt.uncertain(
                SendRequestOutput(),
                _failure(
                    _POST_NON_IDEMPOTENT,
                    FailureKind.TRANSPORT,
                    "an earlier attempt may have sent the POST, so it is not sent again",
                ),
                client._receipt(call, _Exchange()),
            )
        outcome = _PostOutcome(
            client, call, fence, has_possible_earlier_send=decision.prior_send_upper_bound > 0
        )
        deadline = asyncio.get_running_loop().time() + _OPERATION_SECONDS
        credentials, failed = await client._resolve_credentials(call, deadline, _POST_NON_IDEMPOTENT)
        if credentials is None:
            assert failed is not None
            is_terminal = failed.kind != "retry"
            try:
                await fence.record_not_sent()
            except Exception:
                if not is_terminal:
                    return MutationAttempt.retry(
                        _failure(
                            _POST_NON_IDEMPOTENT,
                            FailureKind.AVAILABILITY,
                            "the OAuth access token and the no-send checkpoint are unavailable now",
                        )
                    )
            if is_terminal and outcome.has_possible_earlier_send:
                return outcome.uncertain(
                    SendRequestOutput(),
                    FailureKind.AUTHENTICATION,
                    "an earlier attempt may have sent the POST, "
                    "and the connection's credentials cannot send it now",
                    _Exchange(),
                )
            return failed
        token = credentials.access_token.reveal()
        try:
            answer = await fence.dispatch(
                lambda write_deadline: client._exchange(
                    call, request, token, _loop_deadline(write_deadline, deadline)
                )
            )
        except CheckpointError:
            return MutationAttempt.retry(
                _failure(
                    _POST_NON_IDEMPOTENT,
                    FailureKind.AVAILABILITY,
                    "the single-dispatch claim could not be recorded; nothing was sent",
                )
            )
        except InsufficientAttemptTimeError:
            return MutationAttempt.retry(
                _failure(
                    _POST_NON_IDEMPOTENT,
                    FailureKind.AVAILABILITY,
                    "too little of the Step's time remained to send the POST; nothing was sent",
                )
            )
        except DispatchNotAllowedError:
            return MutationAttempt.retry(
                _failure(
                    _POST_NON_IDEMPOTENT,
                    FailureKind.LOCAL_DEFECT,
                    "the single-dispatch fence refused the POST; nothing was sent",
                )
            )
        except TimeoutError:
            return outcome.uncertain(
                SendRequestOutput(),
                FailureKind.TRANSPORT,
                "the POST was sent but the API did not answer completely",
                _Exchange(),
            )
        return await outcome.attempt_for(answer, token, deadline)


def _loop_deadline(write_deadline: datetime, operation_deadline: float) -> float:
    """Convert the fence's wall-clock write deadline to an event loop deadline."""
    remaining = (write_deadline - datetime.now(UTC)).total_seconds()
    return min(operation_deadline, asyncio.get_running_loop().time() + max(remaining, 0))


class _PostOutcome:
    """Classifies the answer to one fenced POST."""

    def __init__(self, client: Client, call: Call, fence: Fence, *, has_possible_earlier_send: bool) -> None:
        self._client = client
        self._call = call
        self._fence = fence
        # An earlier send may have been processed, so a refusal of this one cannot prove nothing changed.
        self.has_possible_earlier_send = has_possible_earlier_send

    async def attempt_for(
        self, answer: _Exchange, token: str, deadline: float
    ) -> MutationAttempt[SendRequestOutput]:
        output = _output(answer, [token])
        status = answer.status
        if answer.refusal is not None:
            return await self._refuse(
                "defect",
                SendRequestOutput(),
                FailureKind.VALIDATION,
                "the connection's apiBaseUrl resolves to an address the connector will not connect to: "
                + answer.refusal.reason,
                answer,
            )
        if answer.error and not answer.is_dispatched:
            return await self._retry_unprocessed(
                FailureKind.AVAILABILITY, "the API could not be reached; the POST was not sent", 0
            )
        if answer.error:
            return self.uncertain(
                output,
                FailureKind.TRANSPORT,
                "the POST was sent but the API did not answer completely",
                answer,
            )
        if 200 <= status < 300:
            return MutationAttempt.branch_result(
                "completed", output, self._client._receipt(self._call, answer)
            )
        if status == 429:
            return await self._retry_unprocessed(
                FailureKind.RATE_LIMIT,
                "the API answered HTTP 429 without processing the POST",
                self._client._retry_after(answer),
            )
        if status == 503:
            return await self._retry_unprocessed(
                FailureKind.AVAILABILITY,
                "the API answered HTTP 503 without processing the POST",
                self._client._retry_after(answer),
            )
        if status == 401:
            return await self._renew_after_rejection(output, token, answer, deadline)
        if status in _DEFINITE_REFUSALS:
            return await self._refuse(
                "rejected", output, _rejection_kind(status), f"the API answered HTTP {status}", answer
            )
        if status >= 500:
            return self.uncertain(
                output,
                FailureKind.AVAILABILITY,
                f"the API answered HTTP {status} after the POST was sent",
                answer,
            )
        return self.uncertain(
            output,
            _rejection_kind(status),
            f"the API answered HTTP {status}, which does not prove the POST was refused",
            answer,
        )

    async def _renew_after_rejection(
        self, output: SendRequestOutput, token: str, answer: _Exchange, deadline: float
    ) -> MutationAttempt[SendRequestOutput]:
        """Renew after a 401 and return Retry; an authorization that cannot be renewed selects rejected."""
        try:
            async with asyncio.timeout_at(deadline):
                replacement = await resolve_credential_after_rejection(
                    self._client._credentials, self._call, self._client._refresh_driver
                )
            is_renewed = (
                bool(replacement.access_token.reveal()) and replacement.access_token.reveal() != token
            )
        except TimeoutError:
            is_renewed = True
        except Exception:
            is_renewed = False
        if is_renewed:
            return await self._retry_unprocessed(
                FailureKind.AUTHENTICATION,
                "the API answered HTTP 401 without processing the POST; "
                "it is sent again with a renewed token",
                0,
            )
        return await self._refuse(
            "rejected",
            output,
            FailureKind.AUTHENTICATION,
            "the API answered HTTP 401 and the OAuth authorization could not be renewed",
            answer,
        )

    async def _retry_unprocessed(
        self, kind: FailureKind, message: str, retry_after: float
    ) -> MutationAttempt[SendRequestOutput]:
        """Record that the API did not process this send, then return Retry."""
        try:
            await self._fence.record_can_resend()
        except Exception:
            # The claim stays the last checkpoint, so the next attempt selects uncertain instead of sending.
            return MutationAttempt.retry(
                _failure(
                    _POST_NON_IDEMPOTENT,
                    FailureKind.AVAILABILITY,
                    "the retry checkpoint could not be recorded",
                )
            )
        return MutationAttempt.retry(_failure(_POST_NON_IDEMPOTENT, kind, message), retry_after)

    async def _refuse(
        self, branch: str, output: SendRequestOutput, kind: FailureKind, message: str, answer: _Exchange
    ) -> MutationAttempt[SendRequestOutput]:
        """Select branch for a refusal of the only send, or uncertain after an earlier possible send."""
        if self.has_possible_earlier_send:
            return self.uncertain(
                output, kind, message + "; an earlier attempt may have sent the POST", answer
            )
        try:
            await self._fence.record_can_resend()
        except Exception:
            # Best effort: the branch is terminal; a lost canResend only makes a later attempt uncertain.
            pass
        return _branch(
            _POST_NON_IDEMPOTENT, branch, output, kind, message, self._client._receipt(self._call, answer)
        )

    def uncertain(
        self, output: SendRequestOutput, kind: FailureKind, message: str, answer: _Exchange
    ) -> MutationAttempt[SendRequestOutput]:
        """Select uncertain; the claim stays the last checkpoint, so nothing is sent again."""
        return MutationAttempt.uncertain(
            output, _failure(_POST_NON_IDEMPOTENT, kind, message), self._client._receipt(self._call, answer)
        )
