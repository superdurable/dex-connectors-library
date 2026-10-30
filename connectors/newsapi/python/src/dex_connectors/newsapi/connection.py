# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""NewsAPI connection and project credential binding."""

from typing import Any

import httpx
from dex_connectors.sdk import Call, ConnectionRef, Query, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from .client import CONNECTOR_ID, SEARCH_ARTICLES_DEFINITION, Client, Config, Credentials
from .models import ArticlePage, SearchArticlesInput


class Connection:
    """Nonserializable NewsAPI operation binding for a named project connection."""

    __slots__ = ("_operation", "_client", "reference")

    def __init__(
        self,
        reference: ConnectionRef,
        operation: Query[SearchArticlesInput, ArticlePage],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or operation is None:
            raise ValueError("NewsAPI connection provider and operation are required")
        if operation.definition().operation != SEARCH_ARTICLES_DEFINITION.operation:
            raise ValueError("NewsAPI connection operation identity differs")
        self.reference = reference
        self._operation = operation
        self._client = client

    def search_articles(self) -> Query[SearchArticlesInput, ArticlePage]:
        """Return the Query operation used by an application Step."""
        return self._operation

    async def aclose(self) -> None:
        """Close the owned provider transport after Worker shutdown."""
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        """Keep provider transport and credentials out of logs."""
        return "newsapi.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        """Prevent a live connection from entering durable Flow state."""
        raise TypeError("NewsAPI connections cannot be serialized")


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind a client to an explicit logical connection reference."""
    if client is None:
        raise ValueError("NewsAPI client is required")
    return Connection(reference, client.search_articles(), client)


def new_connection_with_operations(
    operation: Query[SearchArticlesInput, ArticlePage], reference: ConnectionRef
) -> Connection:
    """Bind a mock Query to the same connection identity as the real provider."""
    return Connection(reference, operation)


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        value = material.get("api_key")
        if not isinstance(value, str) or not value:
            raise ValueError("NewsAPI credential api_key is required")
        return Credentials(SecretString(value))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Open declared ordinary settings and resolve current private key per call."""
    if project is None:
        raise ValueError("NewsAPI connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("NewsAPI project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("NewsAPI project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return Connection(reference, client.search_articles(), client)


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("NewsAPI configuration has unknown fields")
    base_url = value.get("baseUrl", "")
    timeout = value.get("timeout", 0)
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not isinstance(base_url, str):
        raise ValueError("NewsAPI configuration baseUrl must be a string")
    if type(timeout) is not int or type(max_response_bytes) is not int:
        raise ValueError("NewsAPI configuration timeout and maxResponseBytes must be integers")
    return Config(base_url, timeout / 1_000_000_000, max_response_bytes)
