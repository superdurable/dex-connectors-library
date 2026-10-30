# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider-neutral OAuth token exchanges and deterministic refresh decisions."""

import base64
import json
import re
from collections.abc import Mapping
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta
from enum import StrEnum
from typing import Any
from urllib.parse import urlsplit

import httpx
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import padding, rsa

from ..credentials import ReauthorizationRequiredError, SecretString
from ..providerhttp import read_bounded_body

MAX_TOKEN_RESPONSE_BYTES = 1 << 20
REFRESH_SKEW = timedelta(minutes=5)
_STANDARD_RESPONSE_FIELDS = frozenset({
    "access_token", "refresh_token", "expires_in", "refresh_token_expires_in", "scope",
    "token_type", "error", "error_description", "error_uri", "id_token",
})
_GRANT_REQUEST_FIELDS = frozenset({
    "grant_type", "refresh_token", "client_id", "client_secret", "assertion",
    "client_assertion", "client_assertion_type",
})


class MissingExpiryRule(StrEnum):
    """Choose whether a credential without expiry is refreshable or permanent."""

    REFRESH_WHEN_EXPIRY_MISSING = "refresh"
    KEEP_WHEN_EXPIRY_MISSING = "keep"


def proportional_refresh_skew(issued_at: datetime | None, expires_at: datetime | None) -> timedelta:
    """Return the smaller of five minutes and one quarter of the token lifetime."""
    if issued_at is None or expires_at is None or expires_at <= issued_at:
        return REFRESH_SKEW
    return min(REFRESH_SKEW, (expires_at - issued_at) / 4)


def forced_refresh_floor(issued_at: datetime | None, expires_at: datetime | None) -> timedelta:
    """Bound repeated forced refreshes to one minute or a quarter lifetime."""
    if issued_at is None or expires_at is None or expires_at <= issued_at:
        return timedelta(minutes=1)
    return min(timedelta(minutes=1), (expires_at - issued_at) / 4)


def is_refresh_required(
    has_access_token: bool,
    expires_at: datetime | None,
    now: datetime,
    rule: MissingExpiryRule,
    issued_at: datetime | None = None,
) -> bool:
    """Decide refresh from token presence, optional expiry, and expiry skew."""
    if not has_access_token:
        return True
    if expires_at is None:
        return rule != MissingExpiryRule.KEEP_WHEN_EXPIRY_MISSING
    return now >= expires_at - proportional_refresh_skew(issued_at, expires_at)


def has_all_scopes(returned_scope: str, required_scopes: tuple[str, ...]) -> bool:
    """Accept all required scopes in a space- or comma-separated OAuth value."""
    return set(required_scopes) <= set(filter(None, re.split(r"[ ,]+", returned_scope)))


def has_exact_scopes(returned_scope: str, required_scopes: tuple[str, ...]) -> bool:
    """Require equal scope sets, accepting spaces or commas as separators."""
    return set(required_scopes) == set(filter(None, re.split(r"[ ,]+", returned_scope)))


class ClientAuthenticationMethod(StrEnum):
    """OAuth token endpoint client authentication method."""

    CLIENT_SECRET_POST = "client_secret_post"
    CLIENT_SECRET_BASIC = "client_secret_basic"
    PUBLIC_CLIENT = "none"


@dataclass(frozen=True, slots=True)
class ClientCredentials:
    """OAuth client identity and redacted secret used for one token grant."""

    id: str
    secret: SecretString
    authentication_method: ClientAuthenticationMethod


@dataclass(frozen=True, slots=True)
class TokenResponse:
    """Complete bounded OAuth token response with redacted token material."""

    access_token: SecretString = field(repr=False)
    refresh_token: SecretString = field(repr=False)
    expires_in: timedelta
    refresh_token_expires_in: timedelta
    scope: str
    token_type: str
    retained_fields: dict[str, object] = field(default_factory=dict)

    def next_refresh_token(self, prior: SecretString) -> SecretString:
        """Use rotated refresh material when present, otherwise retain the prior token."""
        return self.refresh_token if self.refresh_token.reveal() else prior


@dataclass(frozen=True, slots=True)
class TokenEndpoint:
    """OAuth token endpoint and connector-owned response rules.

    The HTTP client belongs to the caller. Redirects are disabled per request,
    and terminal codes below HTTP 500 require interactive reauthorization.
    """

    provider_name: str
    url: str
    http_client: httpx.AsyncClient = field(repr=False)
    terminal_error_codes: tuple[str, ...] = ()
    accepted_token_types: tuple[str, ...] = ()
    accepts_missing_token_type: bool = False
    accepts_missing_expires_in: bool = False
    retained_response_fields: tuple[str, ...] = ()
    uses_json_request_body: bool = False
    allows_plain_http: bool = False
    additional_request_parameters: dict[str, str] = field(default_factory=dict)

    async def exchange_refresh_token(
        self, client: ClientCredentials, refresh_token: SecretString
    ) -> TokenResponse:
        """Exchange a refresh token; missing grant material needs reauthorization."""
        if not client.id or not refresh_token.reveal():
            raise ReauthorizationRequiredError()
        parameters = {"grant_type": "refresh_token", "refresh_token": refresh_token.reveal()}
        basic_client = self._apply_client_authentication(parameters, client, allow_public=True)
        return await self._exchange(parameters, basic_client)

    async def exchange_client_credentials(
        self, client: ClientCredentials, scopes: tuple[str, ...] = ()
    ) -> TokenResponse:
        """Request a client-credentials grant with optional space-separated scopes."""
        if not client.id or not client.secret.reveal():
            raise ReauthorizationRequiredError()
        parameters = {"grant_type": "client_credentials"}
        if scopes:
            parameters["scope"] = " ".join(scopes)
        basic_client = self._apply_client_authentication(parameters, client, allow_public=False)
        return await self._exchange(parameters, basic_client)

    async def exchange_jwt_bearer_assertion(self, assertion: SecretString) -> TokenResponse:
        """Exchange a signed RFC 7523 JWT-bearer assertion without client material."""
        if not assertion.reveal():
            raise ReauthorizationRequiredError()
        return await self._exchange({
            "grant_type": "urn:ietf:params:oauth:grant-type:jwt-bearer",
            "assertion": assertion.reveal(),
        }, None)

    def _apply_client_authentication(
        self, parameters: dict[str, str], client: ClientCredentials, allow_public: bool
    ) -> ClientCredentials | None:
        if client.authentication_method == ClientAuthenticationMethod.PUBLIC_CLIENT and allow_public:
            parameters["client_id"] = client.id
            return None
        if client.authentication_method not in (
            ClientAuthenticationMethod.CLIENT_SECRET_POST, ClientAuthenticationMethod.CLIENT_SECRET_BASIC
        ):
            raise ValueError("OAuth client authentication method is not supported")
        if not client.secret.reveal():
            raise ReauthorizationRequiredError()
        if client.authentication_method == ClientAuthenticationMethod.CLIENT_SECRET_POST:
            parameters["client_id"] = client.id
            parameters["client_secret"] = client.secret.reveal()
            return None
        return client

    async def _exchange(
        self, parameters: dict[str, str], basic_client: ClientCredentials | None
    ) -> TokenResponse:
        parsed = urlsplit(self.url)
        if (
            parsed.scheme not in ({"http", "https"} if self.allows_plain_http else {"https"})
            or not parsed.hostname or parsed.username is not None or parsed.password is not None
        ):
            raise ValueError("OAuth token endpoint URL is invalid")
        if any(name in _STANDARD_RESPONSE_FIELDS for name in self.retained_response_fields):
            raise ValueError("OAuth token endpoint cannot retain standard token fields")
        if any(name in _GRANT_REQUEST_FIELDS for name in self.additional_request_parameters):
            raise ValueError("OAuth token endpoint cannot replace grant parameters")
        parameters.update(self.additional_request_parameters)
        request = self.http_client.build_request(
            "POST", self.url,
            json=parameters if self.uses_json_request_body else None,
            data=None if self.uses_json_request_body else parameters,
            headers={"Accept": "application/json"},
        )
        if basic_client is not None:
            request.headers["Authorization"] = httpx.BasicAuth(
                basic_client.id, basic_client.secret.reveal()
            ).auth_flow(request).__next__().headers["Authorization"]
        try:
            response = await self.http_client.send(request, follow_redirects=False, stream=True)
            contents = await read_bounded_body(response, MAX_TOKEN_RESPONSE_BYTES)
        except (httpx.HTTPError, OSError, ValueError):
            raise OSError(f"{self.provider_name} token endpoint is unavailable") from None
        if not 200 <= response.status_code < 300 and not contents:
            raise OSError(f"{self.provider_name} token endpoint returned HTTP {response.status_code}")
        try:
            decoded: Any = json.loads(contents)
        except (UnicodeDecodeError, json.JSONDecodeError) as error:
            raise ValueError(f"{self.provider_name} token response is invalid") from error
        if not isinstance(decoded, dict):
            raise ValueError(f"{self.provider_name} token response is invalid")
        error_code = decoded.get("error")
        if not isinstance(error_code, str):
            error_code = ""
        if not 200 <= response.status_code < 300 or error_code:
            if response.status_code < 500 and error_code in self.terminal_error_codes:
                raise ReauthorizationRequiredError()
            raise OSError(f"{self.provider_name} token endpoint returned HTTP {response.status_code}")
        access_token = decoded.get("access_token")
        expires_in = decoded.get("expires_in", 0)
        token_type = decoded.get("token_type", "")
        accepted_types = self.accepted_token_types or ("Bearer",)
        if (
            not isinstance(access_token, str) or not access_token
            or not isinstance(expires_in, int) or isinstance(expires_in, bool)
            or (expires_in <= 0 and not (expires_in == 0 and self.accepts_missing_expires_in))
            or not isinstance(token_type, str)
            or (not token_type and not self.accepts_missing_token_type)
            or (token_type and token_type.lower() not in {value.lower() for value in accepted_types})
        ):
            raise ValueError(f"{self.provider_name} token response omitted required fields")
        refresh_token = decoded.get("refresh_token", "")
        refresh_expiry = decoded.get("refresh_token_expires_in", 0)
        scope = decoded.get("scope", "")
        if (
            not isinstance(refresh_token, str)
            or not isinstance(refresh_expiry, int)
            or not isinstance(scope, str)
        ):
            raise ValueError(f"{self.provider_name} token response is invalid")
        return TokenResponse(
            SecretString(access_token), SecretString(refresh_token), timedelta(seconds=expires_in),
            timedelta(seconds=max(0, refresh_expiry)), scope, token_type,
            {name: decoded[name] for name in self.retained_response_fields if name in decoded},
        )


@dataclass(frozen=True, slots=True)
class JWTBearerAssertion:
    """RFC 7523 RS256 assertion claims for a service-account grant."""

    issuer: str
    audience: str
    issued_at: datetime
    lifetime: timedelta
    subject: str = ""
    scope: str = ""


def parse_rsa_private_key_pem(pem_text: str) -> rsa.RSAPrivateKey:
    """Parse an RSA PKCS #1 or PKCS #8 PEM key without echoing key material."""
    try:
        private_key = serialization.load_pem_private_key(pem_text.encode(), password=None)
    except (ValueError, TypeError) as error:
        raise ValueError("private key is invalid") from error
    if not isinstance(private_key, rsa.RSAPrivateKey):
        raise ValueError("private key must use RSA")
    return private_key


def sign_jwt_bearer_assertion(assertion: JWTBearerAssertion, private_key: rsa.RSAPrivateKey) -> SecretString:
    """Sign a compact RS256 assertion for exchange_jwt_bearer_assertion."""
    if not assertion.issuer or not assertion.audience or assertion.lifetime <= timedelta(0):
        raise ValueError("JWT-bearer assertion claims are incomplete")
    issued_at = assertion.issued_at.astimezone(UTC)
    claims: dict[str, str | int] = {
        "aud": assertion.audience, "exp": int((issued_at + assertion.lifetime).timestamp()),
        "iat": int(issued_at.timestamp()), "iss": assertion.issuer,
    }
    if assertion.subject:
        claims["sub"] = assertion.subject
    if assertion.scope:
        claims["scope"] = assertion.scope
    header = {"alg": "RS256", "typ": "JWT"}
    def encode(value: Mapping[str, str | int]) -> str:
        serialized = json.dumps(value, separators=(",", ":"), sort_keys=True).encode()
        return base64.urlsafe_b64encode(serialized).rstrip(b"=").decode()
    unsigned = f"{encode(header)}.{encode(claims)}"
    signature = private_key.sign(unsigned.encode(), padding.PKCS1v15(), hashes.SHA256())
    return SecretString(f"{unsigned}.{base64.urlsafe_b64encode(signature).rstrip(b'=').decode()}")
