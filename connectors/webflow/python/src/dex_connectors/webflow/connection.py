# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Webflow connection and project credential binding."""

from typing import Any

import httpx
from dex_connectors.sdk import Call, ConnectionRef, Query, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from .client import (
    CONNECTOR_ID,
    GET_COLLECTION_ITEM_DEFINITION,
    LIST_COLLECTION_ITEMS_DEFINITION,
    Client,
    Config,
    Credentials,
)
from .models import (
    GetCollectionItemInput,
    GetCollectionItemOutput,
    ListCollectionItemsInput,
    ListCollectionItemsOutput,
)


class Connection:
    """Nonserializable Webflow operation bindings for a named project connection."""

    __slots__ = ("_list_collection_items", "_get_collection_item", "_client", "reference")

    def __init__(
        self,
        reference: ConnectionRef,
        list_collection_items: Query[ListCollectionItemsInput, ListCollectionItemsOutput],
        get_collection_item: Query[GetCollectionItemInput, GetCollectionItemOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_collection_items is None or get_collection_item is None:
            raise ValueError("Webflow connection provider and operations are required")
        if (
            list_collection_items.definition().operation != LIST_COLLECTION_ITEMS_DEFINITION.operation
            or get_collection_item.definition().operation != GET_COLLECTION_ITEM_DEFINITION.operation
        ):
            raise ValueError("Webflow connection operation identity differs")
        self.reference = reference
        self._list_collection_items = list_collection_items
        self._get_collection_item = get_collection_item
        self._client = client

    def list_collection_items(self) -> Query[ListCollectionItemsInput, ListCollectionItemsOutput]:
        """Return the listCollectionItems Query used by an application Step."""
        return self._list_collection_items

    def get_collection_item(self) -> Query[GetCollectionItemInput, GetCollectionItemOutput]:
        """Return the getCollectionItem Query used by an application Step."""
        return self._get_collection_item

    async def aclose(self) -> None:
        """Close the owned provider transport after Worker shutdown."""
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        """Keep provider transport and credentials out of logs."""
        return "webflow.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        """Prevent a live connection from entering durable Flow state."""
        raise TypeError("Webflow connections cannot be serialized")


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind a client to an explicit logical connection reference."""
    if client is None:
        raise ValueError("Webflow client is required")
    return Connection(reference, client.list_collection_items(), client.get_collection_item(), client)


def new_connection_with_operations(
    list_collection_items: Query[ListCollectionItemsInput, ListCollectionItemsOutput],
    get_collection_item: Query[GetCollectionItemInput, GetCollectionItemOutput],
    reference: ConnectionRef,
) -> Connection:
    """Bind mock operations to the same connection identity as the real provider."""
    return Connection(reference, list_collection_items, get_collection_item)


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        value = material.get("site_token")
        if not isinstance(value, str) or not value:
            raise ValueError("Webflow credential site_token is required")
        return Credentials(SecretString(value))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Open declared ordinary settings and resolve the current token per call."""
    if project is None:
        raise ValueError("Webflow connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Webflow project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Webflow project connection provider differs")
    client = Client(
        _decode_config(matching[0].configuration), _ProjectCredentialSource(project, key), http_client
    )
    return new_connection(client, reference)


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Webflow configuration has unknown fields")
    base_url = value.get("baseUrl", "")
    timeout = value.get("timeout", 0)
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not isinstance(base_url, str):
        raise ValueError("Webflow configuration baseUrl must be a string")
    if type(timeout) is not int or type(max_response_bytes) is not int:
        raise ValueError("Webflow configuration timeout and maxResponseBytes must be integers")
    return Config(base_url, timeout / 1_000_000_000, max_response_bytes)
