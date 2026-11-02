# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Signed Standard Webhooks delivery client."""

from __future__ import annotations

from collections.abc import Callable
from datetime import UTC, datetime
from typing import TYPE_CHECKING
from urllib.parse import urlsplit

import httpx
from dex_connectors.sdk import CredentialProvider
from dex_connectors.sdk.providerhttp import new_provider_http_client

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials

if TYPE_CHECKING:
    from .operations import SendEventOperation


class Client:
    """Hold validated delivery settings and resolve the signing secret per call."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        clock: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("webhook settings and credentials are required")
        if config.max_body_bytes <= 0:
            raise ValueError("webhook maxBodyBytes must be positive")
        self.delivery_url = _validate_delivery_url(config.delivery_url)
        self.max_body_bytes = config.max_body_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(25)
        self.clock = clock or (lambda: datetime.now(UTC))

    def send_event(self) -> SendEventOperation:
        """Bind the signed event delivery Mutation."""
        from .operations import SendEventOperation

        return SendEventOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after Worker shutdown."""
        await self.http_client.aclose()


def _validate_delivery_url(value: str) -> str:
    if not value:
        return ""
    if any(ord(character) <= 32 or ord(character) > 126 for character in value):
        raise ValueError("webhook deliveryUrl must be printable ASCII without spaces")
    parsed = urlsplit(value)
    if (
        parsed.scheme != "https"
        or not parsed.hostname
        or parsed.username
        or parsed.password
        or parsed.fragment
    ):
        raise ValueError(
            "webhook deliveryUrl must be an absolute HTTPS URL without credentials or a fragment"
        )
    return value
