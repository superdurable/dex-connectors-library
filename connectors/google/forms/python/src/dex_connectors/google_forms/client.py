# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Google Forms provider client and generated configuration contracts."""

import re
from typing import TYPE_CHECKING

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    CredentialRefreshDriver,
    RejectedCredentialRefreshingProvider,
    resolve_credential,
    resolve_credential_after_rejection,
)
from dex_connectors.sdk.providerhttp import (
    is_header_safe_credential,
    new_provider_http_client,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.refresh import AUTHORIZATION, CREDENTIAL_FIELDS

if TYPE_CHECKING:
    from .operations import GetFormOperation, GetResponseOperation, ListResponsesOperation

_GOOGLE_ID = re.compile(r"[A-Za-z0-9_-]{1,256}\Z")


def is_google_id(value: str) -> bool:
    """Check the exact Google ID alphabet accepted by the Go connector."""
    return isinstance(value, str) and _GOOGLE_ID.fullmatch(value) is not None


class Client:
    """Make bounded authenticated Forms reads with a single 401 refresh attempt."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        refresh_driver: CredentialRefreshDriver[Credentials] | None = None,
    ) -> None:
        if credentials is None:
            raise ValueError("credential provider is required")
        if config.max_response_bytes < 1 or not 1 <= config.response_page_size <= 1000:
            raise ValueError("Google Forms limits are invalid")
        self.api_base_url = validate_base_url(config.endpoint or Config().endpoint) + "/v1"
        self.max_response_bytes = config.max_response_bytes
        self.response_page_size = config.response_page_size
        self.credentials = credentials
        self._owns_http_client = http_client is None
        self.http_client = http_client or new_provider_http_client(25.0)
        if refresh_driver is None:
            from dex_connectors.sdk.oauthtoken import ManifestCredentialRefreshDriver

            refresh_driver = ManifestCredentialRefreshDriver[Credentials](
                "Google Forms", AUTHORIZATION, CREDENTIAL_FIELDS, self.http_client
            )
        self.refresh_driver = refresh_driver

    async def aclose(self) -> None:
        """Close the owned transport when no longer used by a Worker."""
        if self._owns_http_client:
            await self.http_client.aclose()

    def get_form(self) -> "GetFormOperation":
        """Return the form read Query."""
        from .operations import GetFormOperation

        return GetFormOperation(self)

    def list_responses(self) -> "ListResponsesOperation":
        """Return the bounded response list Query."""
        from .operations import ListResponsesOperation

        return ListResponsesOperation(self)

    def get_response(self) -> "GetResponseOperation":
        """Return the single response read Query."""
        from .operations import GetResponseOperation

        return GetResponseOperation(self)

    async def resolve_credentials(self, call: Call) -> Credentials:
        """Resolve call-time credentials through the generated grant driver."""
        result = await resolve_credential(self.credentials, call, self.refresh_driver)
        self._validate_credentials(result)
        return result

    @staticmethod
    def _validate_credentials(credentials: Credentials) -> None:
        token = credentials.access_token.reveal() if credentials.access_token is not None else ""
        if not is_header_safe_credential(token) or credentials.auth_method_id not in (
            "",
            "google-oauth",
            "workspace-domain-delegation",
        ):
            raise ValueError("Google Forms access credentials are unavailable")

    async def get(self, call: Call, credential: Credentials, target: str) -> httpx.Response:
        """Send a Forms GET; refresh once after 401 when the source supports it."""
        for attempt in range(2):
            token = credential.access_token.reveal() if credential.access_token else ""
            request = self.http_client.build_request(
                "GET", target, headers={"Authorization": "Bearer " + token, "Accept": "application/json"}
            )
            response = await self.http_client.send(request)
            if (
                response.status_code != 401
                or attempt
                or not isinstance(self.credentials, RejectedCredentialRefreshingProvider)
            ):
                return response
            try:
                refreshed = await resolve_credential_after_rejection(
                    self.credentials, call, self.refresh_driver
                )
                self._validate_credentials(refreshed)
            except Exception:
                return response
            await response.aclose()
            credential = refreshed
        raise RuntimeError("authenticated request retry was exhausted")
