# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded authenticated Discord HTTP client with call-time credentials."""

from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import TYPE_CHECKING

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    ReauthorizationRequiredError,
    Receipt,
)
from dex_connectors.sdk.providerhttp import (
    MAX_ERROR_BODY_BYTES,
    BodyTooLargeError,
    is_header_safe_credential,
    new_provider_http_client,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials

if TYPE_CHECKING:
    from .operations import (
        AddReactionOperation,
        CreateMessageOperation,
        GetChannelOperation,
        ListChannelMessagesOperation,
    )

USER_AGENT = "DiscordBot (https://github.com/superdurable/dex-connectors-library, 0.1.0)"


@dataclass(frozen=True, slots=True)
class ProviderResponse:
    """One Discord response, including its bounded body and size status."""

    status_code: int
    headers: httpx.Headers
    body: bytes
    is_body_too_large: bool = False


class Client:
    """Execute Discord operations with fresh credentials on every call.

    The caller owns an injected HTTP client. Close a connector-owned client with ``aclose``.
    """

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Discord configuration and credential source are required")
        endpoint = validate_base_url(config.endpoint or Config().endpoint)
        parsed = httpx.URL(endpoint)
        if parsed.scheme != "https" and not (
            parsed.scheme == "http" and parsed.host in ("localhost", "127.0.0.1")
        ):
            raise ValueError("Discord endpoint must use HTTPS or loopback HTTP")
        if config.max_response_bytes < 0:
            raise ValueError("Discord maxResponseBytes must be positive")
        self.base_url = endpoint.rstrip("/")
        self.max_response_bytes = config.max_response_bytes or Config().max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(25)
        self._owns_client = http_client is None
        self.now = now or (lambda: datetime.now(UTC))

    async def aclose(self) -> None:
        """Close this client's owned HTTP transport."""
        if self._owns_client:
            await self.http_client.aclose()

    def get_channel(self) -> "GetChannelOperation":
        """Return the channel Query bound to this client."""
        from .operations import GetChannelOperation

        return GetChannelOperation(self)

    def list_channel_messages(self) -> "ListChannelMessagesOperation":
        """Return the bounded message-page Query bound to this client."""
        from .operations import ListChannelMessagesOperation

        return ListChannelMessagesOperation(self)

    def create_message(self) -> "CreateMessageOperation":
        """Return the nonce-deduplicated message Mutation."""
        from .operations import CreateMessageOperation

        return CreateMessageOperation(self)

    def add_reaction(self) -> "AddReactionOperation":
        """Return the repeat-safe reaction Mutation."""
        from .operations import AddReactionOperation

        return AddReactionOperation(self)

    async def resolve_token(self, call: Call, operation: str) -> tuple[str | None, Failure | None, str]:
        """Resolve the current bot token and classify unavailable credentials."""
        try:
            credentials = await self.credentials.resolve(call)
        except ReauthorizationRequiredError:
            return (
                None,
                Failure(
                    FailureKind.AUTHENTICATION,
                    "discord",
                    operation,
                    "the Discord connection must be authorized again",
                ),
                "providerRejected",
            )
        except Exception:
            return (
                None,
                Failure(
                    FailureKind.AUTHENTICATION,
                    "discord",
                    operation,
                    "Discord connection credentials are unavailable or invalid",
                ),
                "defect",
            )
        if credentials is None or not is_header_safe_credential(credentials.bot_token.reveal()):
            return (
                None,
                Failure(
                    FailureKind.AUTHENTICATION,
                    "discord",
                    operation,
                    "Discord connection credentials are unavailable or invalid",
                ),
                "defect",
            )
        return credentials.bot_token.reveal(), None, ""

    async def request(
        self, call: Call, token: str, method: str, path: str, body: bytes | None = None
    ) -> ProviderResponse:
        """Send one request and read a bounded response without following redirects."""
        headers = {"Accept": "application/json", "Authorization": "Bot " + token, "User-Agent": USER_AGENT}
        if body is not None:
            headers["Content-Type"] = "application/json"
        request = self.http_client.build_request(method, self.base_url + path, headers=headers, content=body)
        response, _ = await send_traced(self.http_client, request)
        limit = self.max_response_bytes if 200 <= response.status_code < 300 else MAX_ERROR_BODY_BYTES
        try:
            response_body = await read_bounded_body(response, limit)
            return ProviderResponse(response.status_code, response.headers, response_body)
        except BodyTooLargeError:
            return ProviderResponse(
                response.status_code, response.headers, b"", 200 <= response.status_code < 300
            )

    def receipt(self, call: Call, object_id: str) -> Receipt:
        """Record stable call identity without retaining the secret token."""
        return Receipt(
            call_id=call.id,
            provider="discord",
            observed_at=self.now(),
            idempotency_key=call.idempotency_key,
            provider_object_id=object_id,
        )
