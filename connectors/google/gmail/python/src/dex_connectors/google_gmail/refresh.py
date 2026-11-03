# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Gmail-specific validation around the generated manifest token grants."""

import json
from collections.abc import Callable
from dataclasses import replace
from datetime import datetime
from urllib.parse import urlsplit

import httpx
from dex_connectors.sdk import (
    CredentialRefreshResult,
    CredentialRefreshState,
    ReauthorizationRequiredError,
)
from dex_connectors.sdk.oauthtoken import (
    ManifestCredentialRefreshDriver,
    MissingExpiryRule,
    is_refresh_required,
)

from ._generated.refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from .models import Credentials

GOOGLE_OAUTH_AUTH_METHOD_ID = "google-oauth"
WORKSPACE_DOMAIN_DELEGATION_AUTH_METHOD_ID = "workspace-domain-delegation"
GOOGLE_TOKEN_ENDPOINT = "https://oauth2.googleapis.com/token"
GMAIL_SCOPES = (
    "https://www.googleapis.com/auth/gmail.readonly",
    "https://www.googleapis.com/auth/gmail.send",
)


class CredentialRefreshDriver:
    """Exchange Gmail grants through the manifest driver with Google-specific checks."""

    def __init__(self, http_client: httpx.AsyncClient, now: Callable[[], datetime] | None = None) -> None:
        if http_client is None:
            raise ValueError("Google token HTTP client is required")
        self._manifest_driver = ManifestCredentialRefreshDriver[Credentials](
            "Google", AUTHORIZATION, CREDENTIAL_FIELDS, http_client, now=now
        )

    def refresh_required(self, state: CredentialRefreshState[Credentials]) -> bool:
        """Refresh absent or expiring material, including credentials without expiry."""
        return is_refresh_required(
            bool(state.credentials.access_token.reveal()),
            state.expires_at,
            state.now,
            MissingExpiryRule.REFRESH_WHEN_EXPIRY_MISSING,
        )

    async def refresh(
        self, state: CredentialRefreshState[Credentials]
    ) -> CredentialRefreshResult[Credentials]:
        """Apply Gmail's method and delegated mailbox checks before the declared grant."""
        credentials = state.credentials
        if credentials.auth_method_id not in (
            "",
            GOOGLE_OAUTH_AUTH_METHOD_ID,
            WORKSPACE_DOMAIN_DELEGATION_AUTH_METHOD_ID,
        ):
            raise ValueError("Gmail authorization method is not supported")
        if credentials.auth_method_id != WORKSPACE_DOMAIN_DELEGATION_AUTH_METHOD_ID:
            return await self._manifest_driver.refresh(state)

        self._validate_service_account_key(credentials.service_account_key.reveal())
        delegated_user = credentials.delegated_user.strip()
        if not delegated_user or "@" not in delegated_user:
            raise ReauthorizationRequiredError()
        sanitized = replace(credentials, delegated_user=delegated_user)
        result = await self._manifest_driver.refresh(replace(state, credentials=sanitized))
        return replace(
            result,
            credentials=replace(result.credentials, delegated_user=credentials.delegated_user),
        )

    @staticmethod
    def _validate_service_account_key(raw: str) -> None:
        try:
            document = json.loads(raw)
        except (ValueError, UnicodeDecodeError) as error:
            raise ReauthorizationRequiredError(error) from error
        if not isinstance(document, dict) or any(
            not isinstance(document.get(name), str) or not document[name]
            for name in ("client_email", "private_key", "token_uri")
        ):
            raise ReauthorizationRequiredError()
        try:
            endpoint = urlsplit(document["token_uri"])
        except ValueError as error:
            raise ReauthorizationRequiredError(error) from error
        if endpoint.scheme != "https" or not endpoint.hostname:
            raise ReauthorizationRequiredError()


__all__ = [
    "CredentialRefreshDriver",
    "GOOGLE_OAUTH_AUTH_METHOD_ID",
    "WORKSPACE_DOMAIN_DELEGATION_AUTH_METHOD_ID",
    "GOOGLE_TOKEN_ENDPOINT",
    "GMAIL_SCOPES",
]
