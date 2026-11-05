# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded LinkedIn UserInfo requests and Query decisions."""

import ipaddress
import json
import re
from email.utils import parseaddr
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
from .models import AuthenticatedProfile, GetAuthenticatedProfileInput

_USER_AGENT = "superdurable-dex-linkedin-connector/0.1"
_REQUEST_ID = re.compile(r"[A-Za-z0-9_.:/-]{1,128}\Z")


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


class Client:
    """Own the provider HTTP transport and resolve credentials per Dex call."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("LinkedIn configuration and credentials are required")
        config = Config(
            config.user_info_url or Config().user_info_url,
            config.timeout_seconds or Config().timeout_seconds,
            config.max_response_bytes or Config().max_response_bytes,
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
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)

    def get_authenticated_profile(self) -> "GetAuthenticatedProfileOperation":
        """Bind the authenticated profile Query."""
        return GetAuthenticatedProfileOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider transport after Worker shutdown."""
        await self.http_client.aclose()


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
        for request_attempt in range(2):
            try:
                request = self.client.http_client.build_request(
                    "GET",
                    self.client.config.user_info_url,
                    headers={
                        "Accept": "application/json",
                        "Authorization": "Bearer " + credentials.access_token.reveal(),
                        "User-Agent": _USER_AGENT,
                    },
                    timeout=self.client.config.timeout_seconds,
                )
                response, _ = await send_traced(self.client.http_client, request)
                request_id = _request_id(response.headers)
                body = await read_bounded_body(response, self.client.config.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "LinkedIn UserInfo response exceeds the configured size limit",
                    request_id,
                )
            except (ProviderTransportError, httpx.HTTPError, ValueError):
                return QueryAttempt.retry(_failure(FailureKind.AVAILABILITY, "LinkedIn is unavailable"))
            if (
                response.status_code == 401
                and request_attempt == 0
                and hasattr(self.client.credentials, "resolve_after_rejection")
            ):
                try:
                    credentials = await self.client.credentials.resolve_after_rejection(call)
                    if credentials.access_token.reveal():
                        continue
                except Exception:
                    pass
            break
        status = response.status_code
        if status == 429:
            retry_after = response.headers.get("Retry-After", "")
            delay = (
                int(retry_after)
                if retry_after.isascii() and retry_after.isdigit() and int(retry_after) > 0
                else 60
            )
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
            claims = json.loads(body)
            if not isinstance(claims, dict) or any(
                type(claims.get(key, default)) is not type(default)
                for key, default in (
                    ("sub", ""),
                    ("name", ""),
                    ("given_name", ""),
                    ("family_name", ""),
                    ("picture", ""),
                    ("email", ""),
                    ("email_verified", False),
                )
            ):
                raise ValueError("invalid claim types")
            subject = claims.get("sub", "").strip()
            if not subject or len(subject.encode()) > 255:
                raise ValueError("invalid subject")
        except (ValueError, UnicodeError, TypeError):
            return _branch(
                INVALID_RESPONSE,
                FailureKind.PROTOCOL,
                "LinkedIn returned an invalid UserInfo response",
                request_id,
            )
        email = _verified_email(claims.get("email", ""), claims.get("email_verified", False))
        if not email:
            return _branch(
                VERIFIED_EMAIL_REQUIRED,
                FailureKind.AUTHENTICATION,
                "LinkedIn verified email is required",
                request_id,
            )
        profile = AuthenticatedProfile(
            subject=subject,
            name=_bounded(claims.get("name", ""), 256),
            given_name=_bounded(claims.get("given_name", ""), 128),
            family_name=_bounded(claims.get("family_name", ""), 128),
            picture_url=_safe_url(claims.get("picture", "")),
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


def _bounded(value: str, maximum: int) -> str:
    value = value.strip()
    return value.encode()[:maximum].decode("utf-8", "ignore")


def _verified_email(value: str, verified: bool) -> str:
    if not verified:
        return ""
    email = value.strip().lower()
    _, parsed = parseaddr(email)
    if parsed != email or len(email.encode()) > 320 or "@" not in email:
        return ""
    return email


def _locale(value: object) -> str:
    if isinstance(value, str):
        return _bounded(value, 64)
    if isinstance(value, dict):
        language, country = value.get("language", ""), value.get("country", "")
        if not isinstance(language, str) or not isinstance(country, str):
            return ""
        return "-".join(part for part in (_bounded(language, 16), _bounded(country, 16)) if part)
    return ""


def _safe_url(value: str) -> str:
    value = _bounded(value, 2048)
    try:
        parsed = urlsplit(value)
        if parsed.scheme != "https" or not parsed.hostname or parsed.username is not None:
            return ""
    except ValueError:
        return ""
    return value
