# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Algolia connection and project credential binding."""

from typing import Any

import httpx
from dex_connectors.sdk import Call, ConnectionRef, Mutation, Query, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from .client import (
    CONNECTOR_ID,
    SAVE_OBJECT_DEFINITION,
    SEARCH_DEFINITION,
    Client,
    Config,
    Credentials,
)
from .models import SavedObject, SaveObjectInput, SearchInput, SearchPage


class Connection:
    """Nonserializable Algolia operation bindings for a named project connection."""

    __slots__ = ("_save_object", "_search", "_client", "reference")

    def __init__(
        self,
        reference: ConnectionRef,
        save_object: Mutation[SaveObjectInput, SavedObject],
        search: Query[SearchInput, SearchPage],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or save_object is None or search is None:
            raise ValueError("Algolia connection provider and operations are required")
        if (
            save_object.definition().operation != SAVE_OBJECT_DEFINITION.operation
            or search.definition().operation != SEARCH_DEFINITION.operation
        ):
            raise ValueError("Algolia connection operation identity differs")
        self.reference = reference
        self._save_object = save_object
        self._search = search
        self._client = client

    def save_object(self) -> Mutation[SaveObjectInput, SavedObject]:
        """Return the record replacement Mutation used by an application Step."""
        return self._save_object

    def search(self) -> Query[SearchInput, SearchPage]:
        """Return the index search Query used by an application Step."""
        return self._search

    async def aclose(self) -> None:
        """Close the owned provider transport after Worker shutdown."""
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        """Keep provider transport and credentials out of logs."""
        return "algolia.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        """Prevent a live connection from entering durable Flow state."""
        raise TypeError("Algolia connections cannot be serialized")


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind a client to an explicit logical connection reference."""
    if client is None:
        raise ValueError("Algolia client is required")
    return Connection(reference, client.save_object(), client.search(), client)


def new_connection_with_operations(
    save_object: Mutation[SaveObjectInput, SavedObject],
    search: Query[SearchInput, SearchPage],
    reference: ConnectionRef,
) -> Connection:
    """Bind mock operations to the same connection identity as the real provider."""
    return Connection(reference, save_object, search)


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        application_id = material.get("application_id")
        api_key = material.get("api_key")
        if (
            not isinstance(application_id, str)
            or not application_id
            or not isinstance(api_key, str)
            or not api_key
        ):
            raise ValueError("Algolia application_id and api_key are required")
        return Credentials(application_id, SecretString(api_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Open declared ordinary settings and resolve the current API key per call."""
    if project is None:
        raise ValueError("Algolia connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Algolia project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Algolia project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Algolia configuration has unknown fields")
    base_url = value.get("baseUrl", "")
    timeout = value.get("timeout", 0)
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not isinstance(base_url, str):
        raise ValueError("Algolia configuration baseUrl must be a string")
    if any(type(number) is not int for number in (timeout, max_response_bytes)):
        raise ValueError("Algolia configuration timeout and maxResponseBytes must be integers")
    return Config(base_url, timeout / 1_000_000_000, max_response_bytes)
