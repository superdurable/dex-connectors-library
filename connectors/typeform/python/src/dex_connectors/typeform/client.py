# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typeform HTTP transport and operation bindings."""

import json
from datetime import UTC, datetime
from typing import TYPE_CHECKING, Any
from urllib.parse import quote

import httpx
from dex_connectors.sdk import Call, CredentialProvider, Receipt
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    new_provider_http_client,
    read_bounded_body,
    send_traced,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials

if TYPE_CHECKING:
    from .operations import (
        GetFormOperation,
        ListFormsOperation,
        ListResponsesOperation,
        UpsertWebhookOperation,
    )

_HOSTS = {
    "us": "https://api.typeform.com",
    "eu": "https://api.eu.typeform.com",
    "newEu": "https://api.typeform.eu",
}


class Client:
    """Run Typeform requests with call-time credentials and bounded 2xx bodies."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Typeform configuration and credentials are required")
        self.config = config
        self.credentials = credentials
        base_url = _HOSTS.get(config.data_center or "us")
        if base_url is None or config.max_response_bytes < 0 or config.webhook_max_body_bytes < 0:
            raise ValueError("Typeform configuration is invalid")
        self.base_url = base_url
        self.max_response_bytes = config.max_response_bytes or 1048576
        self.http_client = http_client or new_provider_http_client(25)
        self.owns_http_client = http_client is None

    async def aclose(self) -> None:
        """Close the connector-owned HTTP client."""
        if self.owns_http_client:
            await self.http_client.aclose()

    def list_forms(self) -> "ListFormsOperation":
        """Return the listForms Query."""
        from .operations import ListFormsOperation

        return ListFormsOperation(self)

    def get_form(self) -> "GetFormOperation":
        """Return the getForm Query."""
        from .operations import GetFormOperation

        return GetFormOperation(self)

    def list_responses(self) -> "ListResponsesOperation":
        """Return the listResponses Query."""
        from .operations import ListResponsesOperation

        return ListResponsesOperation(self)

    def upsert_webhook(self) -> "UpsertWebhookOperation":
        """Return the upsertWebhook Mutation."""
        from .operations import UpsertWebhookOperation

        return UpsertWebhookOperation(self)

    async def request(
        self,
        token: str,
        method: str,
        path: str,
        query: dict[str, str] | None = None,
        body: dict[str, Any] | None = None,
    ) -> tuple[httpx.Response, bytes]:
        """Send one request and return the response with a bounded success body."""
        target = self.base_url + path
        if query:
            target += "?" + "&".join(
                f"{_query_escape(key)}={_query_escape(value)}" for key, value in sorted(query.items())
            )
        headers = {"Authorization": "Bearer " + token, "Accept": "application/json"}
        if body is not None:
            headers["Content-Type"] = "application/json"
        request = self.http_client.build_request(
            method,
            target,
            headers=headers,
            content=json.dumps(body, separators=(",", ":"), ensure_ascii=False).encode()
            if body is not None
            else None,
        )
        response, _ = await send_traced(self.http_client, request)
        if response.status_code < 200 or response.status_code >= 300:
            try:
                contents = await read_bounded_body(response, 65536)
            except (BodyTooLargeError, ProviderTransportError):
                contents = b""
            return response, contents
        contents = await read_bounded_body(response, self.max_response_bytes)
        return response, contents

    def receipt(self, call: Call, object_id: str = "") -> Receipt:
        """Record the provider identity and observed time without secrets."""
        return Receipt(call.id, "typeform", datetime.now(UTC), call.idempotency_key, object_id)


def _query_escape(value: str) -> str:
    return quote(value, safe="").replace("~", "%7E").replace("%20", "+")
