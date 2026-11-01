# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Cohere connection and project credential binding."""

from typing import Any

import httpx
from dex_connectors.sdk import Call, ConnectionRef, Query, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from .client import CONNECTOR_ID, EMBED_DEFINITION, RERANK_DEFINITION, Client, Config, Credentials
from .models import EmbedInput, EmbedOutput, RerankInput, RerankOutput


class Connection:
    """Nonserializable Cohere operation bindings for a named project connection."""

    __slots__ = ("_embed", "_rerank", "_client", "reference")

    def __init__(
        self,
        reference: ConnectionRef,
        embed: Query[EmbedInput, EmbedOutput],
        rerank: Query[RerankInput, RerankOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or embed is None or rerank is None:
            raise ValueError("Cohere connection provider and operations are required")
        if (
            embed.definition().operation != EMBED_DEFINITION.operation
            or rerank.definition().operation != RERANK_DEFINITION.operation
        ):
            raise ValueError("Cohere connection operation identity differs")
        self.reference = reference
        self._embed = embed
        self._rerank = rerank
        self._client = client

    def embed(self) -> Query[EmbedInput, EmbedOutput]:
        """Return the embedding Query used by an application Step."""
        return self._embed

    def rerank(self) -> Query[RerankInput, RerankOutput]:
        """Return the rerank Query used by an application Step."""
        return self._rerank

    async def aclose(self) -> None:
        """Close the owned provider transport after Worker shutdown."""
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        """Keep provider transport and credentials out of logs."""
        return "cohere.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        """Prevent a live connection from entering durable Flow state."""
        raise TypeError("Cohere connections cannot be serialized")


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind a client to an explicit logical connection reference."""
    if client is None:
        raise ValueError("Cohere client is required")
    return Connection(reference, client.embed(), client.rerank(), client)


def new_connection_with_operations(
    embed: Query[EmbedInput, EmbedOutput],
    rerank: Query[RerankInput, RerankOutput],
    reference: ConnectionRef,
) -> Connection:
    """Bind mock operations to the same connection identity as the real provider."""
    return Connection(reference, embed, rerank)


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        value = material.get("api_key")
        if not isinstance(value, str) or not value:
            raise ValueError("Cohere credential api_key is required")
        return Credentials(SecretString(value))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Open declared ordinary settings and resolve the current private key per call."""
    if project is None:
        raise ValueError("Cohere connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Cohere project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Cohere project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Cohere configuration has unknown fields")
    base_url = value.get("baseUrl", "")
    timeout = value.get("timeout", 0)
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not isinstance(base_url, str):
        raise ValueError("Cohere configuration baseUrl must be a string")
    if type(timeout) is not int or type(max_response_bytes) is not int:
        raise ValueError("Cohere configuration timeout and maxResponseBytes must be integers")
    return Config(base_url, timeout / 1_000_000_000, max_response_bytes)
