# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded LinkedIn UserInfo requests and Query decisions."""

import ipaddress
import json
import re
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Any, Final
from urllib.parse import urlsplit

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    QueryAttempt,
    QueryDefinition,
    Receipt,
)
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import (
    AUTHORIZATION_REVOKED,
    DEFECT,
    GET_AUTHENTICATED_PROFILE_DEFINITION,
    INSUFFICIENT_SCOPE,
    INVALID_RESPONSE,
    NOT_FOUND,
    PROFILE_LOADED,
    PROVIDER_REJECTED,
    VERIFIED_EMAIL_REQUIRED,
)
from ._go_text import go_canonical_email_address, go_decode_utf8
from ._go_url import go_safe_url
from .models import AuthenticatedProfile, GetAuthenticatedProfileInput

MINIMUM_POLL_INTERVAL: Final = timedelta(minutes=1)
"""Bounds profileChanged polling: profiles change rarely and LinkedIn limits are daily."""

_USER_AGENT = "superdurable-dex-linkedin-connector/0.1"
_REQUEST_ID = re.compile(r"[A-Za-z0-9_.:/-]{1,128}\Z")
_LONE_SURROGATE: Final = re.compile("[\ud800-\udfff]")
# The runes Go's strings.TrimSpace removes: unicode.IsSpace.
_GO_SPACE: Final = "\t\n\v\f\r \x85\xa0                　"
# The UserInfo claims Go decodes into strings or a bool, with their zero values.
_TYPED_CLAIMS: Final[tuple[tuple[str, str | bool], ...]] = (
    ("sub", ""),
    ("name", ""),
    ("given_name", ""),
    ("family_name", ""),
    ("picture", ""),
    ("email", ""),
    ("email_verified", False),
)
_USER_INFO_CLAIMS: Final = (*(name for name, _ in _TYPED_CLAIMS), "locale")
# The non-ASCII runes Go's encoding/json folds onto an ASCII letter when matching keys.
_GO_KEY_FOLDS: Final = {"\u017f": "S", "\u212a": "K"}


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "linkedin", "getAuthenticatedProfile", message)


def _branch(
    branch: str, kind: FailureKind, message: str, request_id: str = ""
) -> QueryAttempt[AuthenticatedProfile]:
    return QueryAttempt.branch_result(
        branch, AuthenticatedProfile(), Receipt(provider_request_id=request_id), _failure(kind, message)
    )


def _loopback(host: str) -> bool:
    if host.lower() == "localhost":
        return True
    try:
        return ipaddress.ip_address(host).is_loopback
    except ValueError:
        return False


@dataclass(frozen=True, slots=True)
class _UserInfoExchange:
    """One answered UserInfo request: the status, headers, bounded body, and safe request ID."""

    status: int
    headers: httpx.Headers
    body: bytes
    request_id: str


class _UserInfoTooLargeError(Exception):
    """The UserInfo body exceeded maxResponseBytes."""

    def __init__(self, request_id: str) -> None:
        super().__init__("LinkedIn UserInfo response exceeds the configured size limit")
        self.request_id = request_id


class Client:
    """Own the provider HTTP transport and resolve credentials per Dex call.

    `poll_interval` is the profileChanged Trigger's delay between polls, from
    `Config.poll_interval_seconds` (zero uses the manifest's 15 minutes). `now` supplies poll times. The
    constructor raises ValueError for an interval under one minute, as Go's New does.
    """

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("LinkedIn configuration and credentials are required")
        defaults = Config()
        config = Config(
            config.user_info_url or defaults.user_info_url,
            config.timeout_seconds or defaults.timeout_seconds,
            config.max_response_bytes or defaults.max_response_bytes,
            config.poll_interval_seconds or defaults.poll_interval_seconds,
        )
        parsed = urlsplit(config.user_info_url)
        if (
            not parsed.scheme
            or not parsed.hostname
            or (parsed.scheme != "https" and not _loopback(parsed.hostname))
        ):
            raise ValueError("LinkedIn UserInfo URL must be HTTPS or loopback")
        if parsed.username is not None or parsed.password is not None or parsed.query or parsed.fragment:
            raise ValueError("LinkedIn UserInfo URL cannot contain user info, query, or fragment")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("LinkedIn timeout and maxResponseBytes must be positive")
        poll_interval_seconds = config.poll_interval_seconds
        if (
            isinstance(poll_interval_seconds, bool)
            or not isinstance(poll_interval_seconds, int | float)
            or not poll_interval_seconds >= MINIMUM_POLL_INTERVAL.total_seconds()
        ):
            raise ValueError("LinkedIn pollInterval must be at least 1m")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self.poll_interval = timedelta(seconds=poll_interval_seconds)
        self.now = now or (lambda: datetime.now(UTC))

    def get_authenticated_profile(self) -> "GetAuthenticatedProfileOperation":
        """Bind the authenticated profile Query."""
        return GetAuthenticatedProfileOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider transport after Worker shutdown."""
        await self.http_client.aclose()

    async def _send_user_info(self, call: Call, credentials: Credentials) -> _UserInfoExchange:
        """Send one UserInfo GET, and once more after a refresh when LinkedIn answers 401.

        Raises _UserInfoTooLargeError, and
        ProviderTransportError, httpx.HTTPError, or ValueError when LinkedIn could not be reached.
        """
        for request_attempt in range(2):
            request = self.http_client.build_request(
                "GET",
                self.config.user_info_url,
                headers={
                    "Accept": "application/json",
                    "Authorization": "Bearer " + credentials.access_token.reveal(),
                    "User-Agent": _USER_AGENT,
                },
                timeout=self.config.timeout_seconds,
            )
            response, _ = await send_traced(self.http_client, request)
            request_id = _request_id(response.headers)
            try:
                body = await read_bounded_body(response, self.config.max_response_bytes)
            except BodyTooLargeError:
                raise _UserInfoTooLargeError(request_id) from None
            exchange = _UserInfoExchange(response.status_code, response.headers, body, request_id)
            if (
                exchange.status != 401
                or request_attempt > 0
                or not hasattr(self.credentials, "resolve_after_rejection")
            ):
                return exchange
            try:
                replacement = await self.credentials.resolve_after_rejection(call)
            except Exception:
                # A refresh that cannot replace the rejected token leaves LinkedIn's 401 as the answer.
                return exchange
            if not isinstance(replacement, Credentials) or not replacement.access_token.reveal():
                return exchange
            credentials = replacement
        raise AssertionError("unreachable: the second attempt always returns")


class GetAuthenticatedProfileOperation:
    """Fetch one LinkedIn UserInfo document per Execute attempt."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("LinkedIn client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query definition."""
        return GET_AUTHENTICATED_PROFILE_DEFINITION

    async def invoke(
        self, call: Call, input: GetAuthenticatedProfileInput
    ) -> QueryAttempt[AuthenticatedProfile]:
        """Resolve OAuth material, call UserInfo, and classify the response."""
        try:
            credentials = await self.client.credentials.resolve(call)
            if not isinstance(credentials, Credentials) or not credentials.access_token.reveal():
                raise ValueError("missing access token")
        except Exception:
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "LinkedIn authorization is unavailable or revoked"
            )
        try:
            exchange = await self.client._send_user_info(call, credentials)
        except _UserInfoTooLargeError as error:
            return _branch(
                INVALID_RESPONSE,
                FailureKind.RESPONSE_TOO_LARGE,
                "LinkedIn UserInfo response exceeds the configured size limit",
                error.request_id,
            )
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return QueryAttempt.retry(_failure(FailureKind.AVAILABILITY, "LinkedIn is unavailable"))
        status, request_id = exchange.status, exchange.request_id
        if status == 429:
            delay = parse_retry_after(exchange.headers.get("Retry-After", ""), datetime.now(UTC)) or 60.0
            return QueryAttempt.retry(
                _failure(FailureKind.RATE_LIMIT, "LinkedIn rate limit was reached"), float(delay)
            )
        if status >= 500:
            return QueryAttempt.retry(_failure(FailureKind.AVAILABILITY, "LinkedIn is unavailable"))
        if status == 401:
            return _branch(
                AUTHORIZATION_REVOKED,
                FailureKind.AUTHENTICATION,
                "LinkedIn authorization is invalid or revoked",
                request_id,
            )
        if status == 403:
            return _branch(
                INSUFFICIENT_SCOPE,
                FailureKind.AUTHORIZATION,
                "LinkedIn authorization is forbidden or lacks scope",
                request_id,
            )
        if status == 404:
            return _branch(NOT_FOUND, FailureKind.NOT_FOUND, "LinkedIn member was not found", request_id)
        if not 200 <= status < 300:
            return _branch(
                PROVIDER_REJECTED,
                FailureKind.PROVIDER_REJECTION,
                "LinkedIn rejected the UserInfo request",
                request_id,
            )
        try:
            claims = _decode_user_info(exchange.body)
        except ValueError:
            claims = {}
        subject = claims.get("sub", "")
        if not _valid_subject(subject):
            return _branch(
                INVALID_RESPONSE,
                FailureKind.PROTOCOL,
                "LinkedIn returned an invalid UserInfo response",
                request_id,
            )
        email = _verified_email(claims["email"], claims["email_verified"])
        if not email:
            return _branch(
                VERIFIED_EMAIL_REQUIRED,
                FailureKind.AUTHENTICATION,
                "LinkedIn verified email is required",
                request_id,
            )
        profile = AuthenticatedProfile(
            subject=subject,
            name=_bounded(claims["name"], 256),
            given_name=_bounded(claims["given_name"], 128),
            family_name=_bounded(claims["family_name"], 128),
            picture_url=_safe_url(claims["picture"]),
            locale=_locale(claims.get("locale")),
            verified_email=email,
        )
        return QueryAttempt.branch_result(PROFILE_LOADED, profile, Receipt(provider_request_id=request_id))


def _request_id(headers: httpx.Headers) -> str:
    for name in ("X-LI-UUID", "X-RestLi-Id", "X-Request-Id"):
        value = str(headers.get(name, "")).strip()
        if _REQUEST_ID.fullmatch(value):
            return value
    return ""


def _decode_user_info(body: bytes) -> dict[str, Any]:
    """Decode a UserInfo document as Go decodes it into its userInfo struct.

    Keys match claims ignoring case, a later key overrides an earlier one, a null claim keeps the earlier
    value, and a claim of another JSON type is invalid. Invalid UTF-8 and lone surrogates become U+FFFD,
    and sub is trimmed. locale stays raw. Raises ValueError.
    """
    document = json.loads(go_decode_utf8(body), object_pairs_hook=_JSONMembers)
    if document is None:
        document = _JSONMembers()
    if not isinstance(document, _JSONMembers):
        raise ValueError("LinkedIn UserInfo response is not an object")
    claims: dict[str, Any] = dict(_TYPED_CLAIMS)
    claims["locale"] = None
    for key, value in document:
        claim = _go_json_field(key, _USER_INFO_CLAIMS)
        if claim is None:
            continue
        if claim == "locale":
            claims["locale"] = value
            continue
        if value is None:
            continue
        if type(value) is not type(claims[claim]):
            raise ValueError(f"LinkedIn UserInfo claim {claim} has an invalid type")
        claims[claim] = _go_string(value) if isinstance(value, str) else value
    claims["sub"] = _go_trim_space(claims["sub"])
    return claims


class _JSONMembers(list[tuple[str, Any]]):
    """A decoded JSON object as its ordered members, so duplicate keys apply in order as in Go."""


def _go_json_field(key: str, names: tuple[str, ...]) -> str | None:
    """Return the field Go's encoding/json fills for key: an exact match, else a case-insensitive one."""
    if key in names:
        return key
    folded = _go_folded_key(key)
    return next((name for name in names if name.upper() == folded), None)


def _go_folded_key(key: str) -> str:
    return "".join(c.upper() if c.isascii() else _GO_KEY_FOLDS.get(c, c) for c in key)


def _go_string(value: str) -> str:
    """Replace lone surrogates with U+FFFD, as Go's encoding/json decodes them."""
    return _LONE_SURROGATE.sub("�", value)


def _go_trim_space(value: str) -> str:
    """Trim the white space Go's strings.TrimSpace trims."""
    return value.strip(_GO_SPACE)


def _valid_subject(subject: str) -> bool:
    return bool(subject) and len(subject.encode()) <= 255


def _bounded(value: str, maximum: int) -> str:
    """Trim value and cut it to maximum UTF-8 bytes on a character boundary, as Go's boundedString does."""
    value = _go_trim_space(_go_string(value))
    return value.encode()[:maximum].decode("utf-8", "ignore")


def _verified_email(value: str, verified: bool) -> str:
    if not verified:
        return ""
    return _canonical_email(value)


def _canonical_email(value: str) -> str:
    """Return the trimmed, lower-cased address, or "" when it is not one bounded address, as Go does."""
    return go_canonical_email_address(_go_trim_space(_go_string(value)))


def _locale(value: object) -> str:
    if isinstance(value, str):
        return _bounded(value, 64)
    if isinstance(value, _JSONMembers):
        members: dict[str, Any] = {"language": None, "country": None}
        for key, member in value:
            field = _go_json_field(key, ("language", "country"))
            if field is not None and member is not None:
                members[field] = member
        language, country = members["language"], members["country"]
        if language is not None and not isinstance(language, str):
            return ""
        if country is not None and not isinstance(country, str):
            return ""
        parts = (_bounded(language or "", 16), _bounded(country or "", 16))
        return "-".join(part for part in parts if part)
    return ""


def _safe_url(value: str) -> str:
    """Return the bounded https URL as Go's url.URL.String writes it, or "" for any other URL."""
    return go_safe_url(_bounded(value, 2048))
