# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Hacker News connection for a named project connection."""

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query
from dex_connectors.sdk.projectconfig import LoadedProject

from .client import CONNECTOR_ID, GET_ITEM_DEFINITION, LIST_STORY_IDS_DEFINITION, Client, Config
from .models import GetItemInput, Item, ListStoryIDsInput, StoryIDs


class Connection:
    """Nonserializable Hacker News operation bindings for a named project connection."""

    __slots__ = ("_list_story_ids", "_get_item", "_client", "reference")

    def __init__(
        self,
        reference: ConnectionRef,
        list_story_ids: Query[ListStoryIDsInput, StoryIDs],
        get_item: Query[GetItemInput, Item],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_story_ids is None or get_item is None:
            raise ValueError("Hacker News connection provider and operations are required")
        if (
            list_story_ids.definition().operation != LIST_STORY_IDS_DEFINITION.operation
            or get_item.definition().operation != GET_ITEM_DEFINITION.operation
        ):
            raise ValueError("Hacker News connection operation identity differs")
        self.reference = reference
        self._list_story_ids = list_story_ids
        self._get_item = get_item
        self._client = client

    def list_story_ids(self) -> Query[ListStoryIDsInput, StoryIDs]:
        """Return the feed snapshot Query used by an application Step."""
        return self._list_story_ids

    def get_item(self) -> Query[GetItemInput, Item]:
        """Return the item Query used by an application Step."""
        return self._get_item

    async def aclose(self) -> None:
        """Close the owned provider transport after Worker shutdown."""
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        """Keep provider transport out of logs."""
        return "hackernews.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        """Prevent a live connection from entering durable Flow state."""
        raise TypeError("Hacker News connections cannot be serialized")


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind a client to an explicit logical connection reference."""
    if client is None:
        raise ValueError("Hacker News client is required")
    return Connection(reference, client.list_story_ids(), client.get_item(), client)


def new_connection_with_operations(
    list_story_ids: Query[ListStoryIDsInput, StoryIDs],
    get_item: Query[GetItemInput, Item],
    reference: ConnectionRef,
) -> Connection:
    """Bind mock operations to the same connection identity as the real provider."""
    return Connection(reference, list_story_ids, get_item)


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Open the declared connection settings; the public API needs no credentials."""
    if project is None:
        raise ValueError("Hacker News connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Hacker News project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Hacker News project connection provider differs")
    return new_connection(Client(_decode_config(matching[0].configuration), http_client), reference)


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Hacker News configuration has unknown fields")
    base_url = value.get("baseUrl", "")
    timeout = value.get("timeout", 0)
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not isinstance(base_url, str):
        raise ValueError("Hacker News configuration baseUrl must be a string")
    if type(timeout) is not int or type(max_response_bytes) is not int:
        raise ValueError("Hacker News configuration timeout and maxResponseBytes must be integers")
    return Config(base_url, timeout / 1_000_000_000, max_response_bytes)
