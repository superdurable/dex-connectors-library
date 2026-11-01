# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Contentful connection and project credential binding."""

from typing import Any

import httpx
from dex_connectors.sdk import Call, ConnectionRef, Query, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from .client import CONNECTOR_ID, GET_ENTRY_DEFINITION, LIST_ENTRIES_DEFINITION, Client, Config, Credentials
from .models import GetEntryInput, GetEntryOutput, ListEntriesInput, ListEntriesOutput


class Connection:
    """Nonserializable Contentful operation bindings for a named project connection."""

    __slots__ = ("_get_entry", "_list_entries", "_client", "reference")

    def __init__(
        self,
        reference: ConnectionRef,
        get_entry: Query[GetEntryInput, GetEntryOutput],
        list_entries: Query[ListEntriesInput, ListEntriesOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_entry is None or list_entries is None:
            raise ValueError("Contentful connection provider and operations are required")
        if (
            get_entry.definition().operation != GET_ENTRY_DEFINITION.operation
            or list_entries.definition().operation != LIST_ENTRIES_DEFINITION.operation
        ):
            raise ValueError("Contentful connection operation identity differs")
        self.reference = reference
        self._get_entry = get_entry
        self._list_entries = list_entries
        self._client = client

    def get_entry(self) -> Query[GetEntryInput, GetEntryOutput]:
        """Return the getEntry Query used by an application Step."""
        return self._get_entry

    def list_entries(self) -> Query[ListEntriesInput, ListEntriesOutput]:
        """Return the listEntries Query used by an application Step."""
        return self._list_entries

    async def aclose(self) -> None:
        """Close the owned provider transport after Worker shutdown."""
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        """Keep provider transport and credentials out of logs."""
        return "contentful.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        """Prevent a live connection from entering durable Flow state."""
        raise TypeError("Contentful connections cannot be serialized")


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind a client to an explicit logical connection reference."""
    if client is None:
        raise ValueError("Contentful client is required")
    return Connection(reference, client.get_entry(), client.list_entries(), client)


def new_connection_with_operations(
    get_entry: Query[GetEntryInput, GetEntryOutput],
    list_entries: Query[ListEntriesInput, ListEntriesOutput],
    reference: ConnectionRef,
) -> Connection:
    """Bind mock operations to the same connection identity as the real provider."""
    return Connection(reference, get_entry, list_entries)


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        value = material.get("delivery_token")
        if not isinstance(value, str) or not value:
            raise ValueError("Contentful credential delivery_token is required")
        return Credentials(SecretString(value))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Open declared ordinary settings and resolve the current token per call."""
    if project is None:
        raise ValueError("Contentful connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Contentful project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Contentful project connection provider differs")
    client = Client(
        _decode_config(matching[0].configuration), _ProjectCredentialSource(project, key), http_client
    )
    return new_connection(client, reference)


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Contentful configuration has unknown fields")
    base_url = value.get("baseUrl", "")
    timeout = value.get("timeout", 0)
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not isinstance(base_url, str):
        raise ValueError("Contentful configuration baseUrl must be a string")
    if type(timeout) is not int or type(max_response_bytes) is not int:
        raise ValueError("Contentful configuration timeout and maxResponseBytes must be integers")
    return Config(base_url, timeout / 1_000_000_000, max_response_bytes)
