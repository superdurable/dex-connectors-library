# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, LIST_DEPLOYS_DEFINITION, LIST_SITES_DEFINITION
from ..models import DeploysPage, ListDeploysInput, ListSitesInput, SitesPage

class Connection:

    __slots__ = ('_list_sites', '_list_deploys', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_sites: Query[ListSitesInput, SitesPage],
        list_deploys: Query[ListDeploysInput, DeploysPage],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_sites is None or list_deploys is None:
            raise ValueError("Netlify connection provider and operations are required")
        if list_sites.definition().operation != LIST_SITES_DEFINITION.operation:
            raise ValueError("Netlify connection operation identity differs")
        if list_deploys.definition().operation != LIST_DEPLOYS_DEFINITION.operation:
            raise ValueError("Netlify connection operation identity differs")
        self.reference = reference
        self._list_sites = list_sites
        self._list_deploys = list_deploys
        self._client = client

    def list_sites(self) -> Query[ListSitesInput, SitesPage]:
        return self._list_sites

    def list_deploys(self) -> Query[ListDeploysInput, DeploysPage]:
        return self._list_deploys

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "netlify.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Netlify connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.netlify.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Netlify client is required")
    return Connection(reference, client.list_sites(), client.list_deploys(), client)

setattr(new_connection, '__module__', 'dex_connectors.netlify.connection')


def new_connection_with_operations(
    list_sites: Query[ListSitesInput, SitesPage],
    list_deploys: Query[ListDeploysInput, DeploysPage],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_sites, list_deploys)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.netlify.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        access_token = material.get("access_token")
        if not isinstance(access_token, str) or not access_token:
            raise ValueError("Netlify credential access_token is required")
        return Credentials(SecretString(access_token))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Netlify connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Netlify project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Netlify project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.netlify.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Netlify configuration has unknown fields")
    base_url = value.get("baseUrl", '')
    if not isinstance(base_url, str):
        raise ValueError("Netlify configuration baseUrl has an invalid type")
    timeout = value.get("timeout", 0)
    if not type(timeout) is int:
        raise ValueError("Netlify configuration timeout has an invalid type")
    timeout_seconds = timeout / 1_000_000_000
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not type(max_response_bytes) is int:
        raise ValueError("Netlify configuration maxResponseBytes has an invalid type")
    return Config(base_url=base_url, timeout_seconds=timeout_seconds, max_response_bytes=max_response_bytes)
