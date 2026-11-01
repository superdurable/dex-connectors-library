# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Netlify connection and project credential binding."""

from typing import Any

import httpx
from dex_connectors.sdk import Call, ConnectionRef, Query, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from .client import (
    CONNECTOR_ID,
    LIST_DEPLOYS_DEFINITION,
    LIST_SITES_DEFINITION,
    Client,
    Config,
    Credentials,
)
from .models import DeploysPage, ListDeploysInput, ListSitesInput, SitesPage


class Connection:
    """Nonserializable Netlify operation bindings for a named project connection."""

    __slots__ = ("_list_sites", "_list_deploys", "_client", "reference")

    def __init__(
        self,
        reference: ConnectionRef,
        list_sites: Query[ListSitesInput, SitesPage],
        list_deploys: Query[ListDeploysInput, DeploysPage],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_sites is None or list_deploys is None:
            raise ValueError("Netlify connection provider and operations are required")
        if (
            list_sites.definition().operation != LIST_SITES_DEFINITION.operation
            or list_deploys.definition().operation != LIST_DEPLOYS_DEFINITION.operation
        ):
            raise ValueError("Netlify connection operation identity differs")
        self.reference = reference
        self._list_sites = list_sites
        self._list_deploys = list_deploys
        self._client = client

    def list_sites(self) -> Query[ListSitesInput, SitesPage]:
        """Return the site page Query used by an application Step."""
        return self._list_sites

    def list_deploys(self) -> Query[ListDeploysInput, DeploysPage]:
        """Return the deploy page Query used by an application Step."""
        return self._list_deploys

    async def aclose(self) -> None:
        """Close the owned provider transport after Worker shutdown."""
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        """Keep provider transport and credentials out of logs."""
        return "netlify.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        """Prevent a live connection from entering durable Flow state."""
        raise TypeError("Netlify connections cannot be serialized")


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind a client to an explicit logical connection reference."""
    if client is None:
        raise ValueError("Netlify client is required")
    return Connection(reference, client.list_sites(), client.list_deploys(), client)


def new_connection_with_operations(
    list_sites: Query[ListSitesInput, SitesPage],
    list_deploys: Query[ListDeploysInput, DeploysPage],
    reference: ConnectionRef,
) -> Connection:
    """Bind mock operations to the same connection identity as the real provider."""
    return Connection(reference, list_sites, list_deploys)


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        value = material.get("access_token")
        if not isinstance(value, str) or not value:
            raise ValueError("Netlify credential access_token is required")
        return Credentials(SecretString(value))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Open declared ordinary settings and resolve the current access token per call."""
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


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Netlify configuration has unknown fields")
    base_url = value.get("baseUrl", "")
    timeout = value.get("timeout", 0)
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not isinstance(base_url, str):
        raise ValueError("Netlify configuration baseUrl must be a string")
    if type(timeout) is not int or type(max_response_bytes) is not int:
        raise ValueError("Netlify configuration timeout and maxResponseBytes must be integers")
    return Config(base_url, timeout / 1_000_000_000, max_response_bytes)
