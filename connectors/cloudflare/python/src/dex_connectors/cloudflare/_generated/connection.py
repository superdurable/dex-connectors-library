# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, LIST_ZONES_DEFINITION, PURGE_CACHE_BY_URLS_DEFINITION
from ..models import CachePurge, ListZonesInput, PurgeCacheByURLsInput, ZonesPage

class Connection:

    __slots__ = ('_purge_cache_by_urls', '_list_zones', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        purge_cache_by_urls: Mutation[PurgeCacheByURLsInput, CachePurge],
        list_zones: Query[ListZonesInput, ZonesPage],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or purge_cache_by_urls is None or list_zones is None:
            raise ValueError("Cloudflare connection provider and operations are required")
        if purge_cache_by_urls.definition().operation != PURGE_CACHE_BY_URLS_DEFINITION.operation:
            raise ValueError("Cloudflare connection operation identity differs")
        if list_zones.definition().operation != LIST_ZONES_DEFINITION.operation:
            raise ValueError("Cloudflare connection operation identity differs")
        self.reference = reference
        self._purge_cache_by_urls = purge_cache_by_urls
        self._list_zones = list_zones
        self._client = client

    def purge_cache_by_urls(self) -> Mutation[PurgeCacheByURLsInput, CachePurge]:
        return self._purge_cache_by_urls

    def list_zones(self) -> Query[ListZonesInput, ZonesPage]:
        return self._list_zones

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "cloudflare.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Cloudflare connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.cloudflare.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Cloudflare client is required")
    return Connection(reference, client.purge_cache_by_urls(), client.list_zones(), client)

setattr(new_connection, '__module__', 'dex_connectors.cloudflare.connection')


def new_connection_with_operations(
    purge_cache_by_urls: Mutation[PurgeCacheByURLsInput, CachePurge],
    list_zones: Query[ListZonesInput, ZonesPage],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, purge_cache_by_urls, list_zones)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.cloudflare.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        api_token = material.get("api_token")
        if not isinstance(api_token, str) or not api_token:
            raise ValueError("Cloudflare credential api_token is required")
        return Credentials(SecretString(api_token))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
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

setattr(new_project_connection, '__module__', 'dex_connectors.cloudflare.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Cloudflare configuration has unknown fields")
    base_url = value.get("baseUrl", '')
    if not isinstance(base_url, str):
        raise ValueError("Cloudflare configuration baseUrl has an invalid type")
    timeout = value.get("timeout", 0)
    if not type(timeout) is int:
        raise ValueError("Cloudflare configuration timeout has an invalid type")
    timeout_seconds = timeout / 1_000_000_000
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not type(max_response_bytes) is int:
        raise ValueError("Cloudflare configuration maxResponseBytes has an invalid type")
    return Config(base_url=base_url, timeout_seconds=timeout_seconds, max_response_bytes=max_response_bytes)
