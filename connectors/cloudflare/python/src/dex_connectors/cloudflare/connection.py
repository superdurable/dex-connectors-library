# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Cloudflare connection and project credential binding."""

from typing import Any

import httpx
from dex_connectors.sdk import Call, ConnectionRef, Mutation, Query, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from .client import (
    CONNECTOR_ID,
    LIST_ZONES_DEFINITION,
    PURGE_CACHE_BY_URLS_DEFINITION,
    Client,
    Config,
    Credentials,
)
from .models import CachePurge, ListZonesInput, PurgeCacheByURLsInput, ZonesPage


class Connection:
    """Nonserializable Cloudflare operation bindings for a named project connection."""

    __slots__ = ("_purge_cache_by_urls", "_list_zones", "_client", "reference")

    def __init__(
        self,
        reference: ConnectionRef,
        purge_cache_by_urls: Mutation[PurgeCacheByURLsInput, CachePurge],
        list_zones: Query[ListZonesInput, ZonesPage],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or purge_cache_by_urls is None or list_zones is None:
            raise ValueError("Cloudflare connection provider and operations are required")
        if (
            purge_cache_by_urls.definition().operation != PURGE_CACHE_BY_URLS_DEFINITION.operation
            or list_zones.definition().operation != LIST_ZONES_DEFINITION.operation
        ):
            raise ValueError("Cloudflare connection operation identity differs")
        self.reference = reference
        self._purge_cache_by_urls = purge_cache_by_urls
        self._list_zones = list_zones
        self._client = client

    def purge_cache_by_urls(self) -> Mutation[PurgeCacheByURLsInput, CachePurge]:
        """Return the URL cache purge Mutation used by an application Step."""
        return self._purge_cache_by_urls

    def list_zones(self) -> Query[ListZonesInput, ZonesPage]:
        """Return the zone listing Query used by an application Step."""
        return self._list_zones

    async def aclose(self) -> None:
        """Close the owned provider transport after Worker shutdown."""
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        """Keep provider transport and credentials out of logs."""
        return "cloudflare.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        """Prevent a live connection from entering durable Flow state."""
        raise TypeError("Cloudflare connections cannot be serialized")


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind a client to an explicit logical connection reference."""
    if client is None:
        raise ValueError("Cloudflare client is required")
    return Connection(reference, client.purge_cache_by_urls(), client.list_zones(), client)


def new_connection_with_operations(
    purge_cache_by_urls: Mutation[PurgeCacheByURLsInput, CachePurge],
    list_zones: Query[ListZonesInput, ZonesPage],
    reference: ConnectionRef,
) -> Connection:
    """Bind mock operations to the same connection identity as the real provider."""
    return Connection(reference, purge_cache_by_urls, list_zones)


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        value = material.get("api_token")
        if not isinstance(value, str) or not value:
            raise ValueError("Cloudflare credential api_token is required")
        return Credentials(SecretString(value))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Open declared ordinary settings and resolve the current API token per call."""
    if project is None:
        raise ValueError("Cloudflare connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Cloudflare project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Cloudflare project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Cloudflare configuration has unknown fields")
    base_url = value.get("baseUrl", "")
    timeout = value.get("timeout", 0)
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not isinstance(base_url, str):
        raise ValueError("Cloudflare configuration baseUrl must be a string")
    if any(type(number) is not int for number in (timeout, max_response_bytes)):
        raise ValueError("Cloudflare configuration timeout and maxResponseBytes must be integers")
    return Config(base_url, timeout / 1_000_000_000, max_response_bytes)
