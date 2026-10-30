# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Google OAuth and Workspace delegated credential refresh without storage ownership."""

import json
from collections.abc import Callable
from dataclasses import replace
from datetime import UTC, datetime, timedelta
from urllib.parse import urlsplit

import httpx
from dex_connectors.sdk import (
    CredentialRefreshResult,
    CredentialRefreshState,
    ReauthorizationRequiredError,
    SecretString,
)
from dex_connectors.sdk.oauthtoken import (
    ClientAuthenticationMethod,
    ClientCredentials,
    JWTBearerAssertion,
    MissingExpiryRule,
    TokenEndpoint,
    has_all_scopes,
    is_refresh_required,
    parse_rsa_private_key_pem,
    sign_jwt_bearer_assertion,
)

from .models import Credentials

GOOGLE_OAUTH_AUTH_METHOD_ID = "google-oauth"
WORKSPACE_DOMAIN_DELEGATION_AUTH_METHOD_ID = "workspace-domain-delegation"
GOOGLE_TOKEN_ENDPOINT = "https://oauth2.googleapis.com/token"
GMAIL_SCOPES = (
    "https://www.googleapis.com/auth/gmail.readonly",
    "https://www.googleapis.com/auth/gmail.send",
)
_TERMINAL_ERROR_CODES = ("invalid_grant", "invalid_client", "unauthorized_client")


class CredentialRefreshDriver:
    """Exchange Google credentials; a credential provider owns locking and persistence."""

    def __init__(self, http_client: httpx.AsyncClient, now: Callable[[], datetime] | None = None) -> None:
        if http_client is None:
            raise ValueError("Google token HTTP client is required")
        self._http_client = http_client
        self._now = now or (lambda: datetime.now(UTC))

    def refresh_required(self, state: CredentialRefreshState[Credentials]) -> bool:
        """Refresh absent or expiring access material within the SDK's five-minute skew."""
        return is_refresh_required(
            bool(state.credentials.access_token.reveal()),
            state.expires_at,
            state.now,
            MissingExpiryRule.REFRESH_WHEN_EXPIRY_MISSING,
        )

    async def refresh(
        self, state: CredentialRefreshState[Credentials]
    ) -> CredentialRefreshResult[Credentials]:
        """Replace OAuth or delegated access material, retaining rotated refresh tokens."""
        credential = state.credentials
        if credential.auth_method_id in ("", GOOGLE_OAUTH_AUTH_METHOD_ID):
            token = await self._token_endpoint(GOOGLE_TOKEN_ENDPOINT).exchange_refresh_token(
                ClientCredentials(
                    credential.oauth_client_id,
                    credential.oauth_client_secret,
                    ClientAuthenticationMethod.CLIENT_SECRET_POST,
                ),
                credential.refresh_token,
            )
            self._validate_scopes(token.scope)
            replacement = replace(
                credential,
                access_token=token.access_token,
                refresh_token=token.next_refresh_token(credential.refresh_token),
            )
        elif credential.auth_method_id == WORKSPACE_DOMAIN_DELEGATION_AUTH_METHOD_ID:
            key = self._parse_service_account_key(credential.service_account_key)
            delegated_user = credential.delegated_user.strip()
            if not delegated_user or "@" not in delegated_user:
                raise ReauthorizationRequiredError()
            try:
                private_key = parse_rsa_private_key_pem(key["private_key"])
            except ValueError as error:
                raise ReauthorizationRequiredError(error) from error
            now = self._now().astimezone(UTC)
            assertion = sign_jwt_bearer_assertion(
                JWTBearerAssertion(
                    issuer=key["client_email"],
                    subject=delegated_user,
                    audience=key["token_uri"],
                    scope=" ".join(GMAIL_SCOPES),
                    issued_at=now,
                    lifetime=timedelta(hours=1),
                ),
                private_key,
            )
            token = await self._token_endpoint(key["token_uri"]).exchange_jwt_bearer_assertion(assertion)
            self._validate_scopes(token.scope)
            replacement = replace(credential, access_token=token.access_token, primary_email=delegated_user)
        else:
            raise ValueError("Gmail authorization method is not supported")
        return CredentialRefreshResult(replacement, self._now().astimezone(UTC) + token.expires_in)

    def _token_endpoint(self, url: str) -> TokenEndpoint:
        return TokenEndpoint("Google", url, self._http_client, terminal_error_codes=_TERMINAL_ERROR_CODES)

    @staticmethod
    def _parse_service_account_key(secret: SecretString) -> dict[str, str]:
        try:
            document = json.loads(secret.reveal())
        except (ValueError, UnicodeDecodeError) as error:
            raise ReauthorizationRequiredError(error) from error
        if not isinstance(document, dict) or any(
            not isinstance(document.get(name), str) or not document[name]
            for name in ("client_email", "private_key", "token_uri")
        ):
            raise ReauthorizationRequiredError()
        parsed = urlsplit(document["token_uri"])
        if parsed.scheme != "https" or not parsed.hostname:
            raise ReauthorizationRequiredError()
        return {name: document[name] for name in ("client_email", "private_key", "token_uri")}

    @staticmethod
    def _validate_scopes(returned_scopes: str) -> None:
        if returned_scopes and not has_all_scopes(returned_scopes, GMAIL_SCOPES):
            raise ReauthorizationRequiredError()


__all__ = [
    "CredentialRefreshDriver",
    "GOOGLE_OAUTH_AUTH_METHOD_ID",
    "WORKSPACE_DOMAIN_DELEGATION_AUTH_METHOD_ID",
    "GMAIL_SCOPES",
]
