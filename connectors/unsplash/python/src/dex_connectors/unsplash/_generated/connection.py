# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated operation bindings for one named Dex connection."""

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_PHOTO_DEFINITION, SEARCH_PHOTOS_DEFINITION
from ..models import GetPhotoInput, Photo, PhotoPage, SearchPhotosInput

class Connection:
    """Nonserializable Unsplash operation binding."""

    __slots__ = ('_search_photos', '_get_photo', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        search_photos: Query[SearchPhotosInput, PhotoPage],
        get_photo: Query[GetPhotoInput, Photo],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or search_photos is None or get_photo is None:
            raise ValueError("Unsplash connection provider and operations are required")
        if search_photos.definition().operation != SEARCH_PHOTOS_DEFINITION.operation:
            raise ValueError("Unsplash connection operation identity differs")
        if get_photo.definition().operation != GET_PHOTO_DEFINITION.operation:
            raise ValueError("Unsplash connection operation identity differs")
        self.reference = reference
        self._search_photos = search_photos
        self._get_photo = get_photo
        self._client = client

    def search_photos(self) -> Query[SearchPhotosInput, PhotoPage]:
        """Return the search_photos operation for a Dex Step."""
        return self._search_photos

    def get_photo(self) -> Query[GetPhotoInput, Photo]:
        """Return the get_photo operation for a Dex Step."""
        return self._get_photo

    async def aclose(self) -> None:
        """Close the owned provider transport after Worker shutdown."""
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        """Keep provider transport and credentials out of logs."""
        return "unsplash.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        """Prevent a live connection from entering durable Flow state."""
        raise TypeError("Unsplash connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.unsplash.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind a provider client to an explicit logical connection reference."""
    if client is None:
        raise ValueError("Unsplash client is required")
    return Connection(reference, client.search_photos(), client.get_photo(), client)

setattr(new_connection, '__module__', 'dex_connectors.unsplash.connection')


def new_connection_with_operations(
    search_photos: Query[SearchPhotosInput, PhotoPage],
    get_photo: Query[GetPhotoInput, Photo],
    reference: ConnectionRef,
) -> Connection:
    """Bind mock operations to the same logical connection identity."""
    return Connection(reference, search_photos, get_photo)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.unsplash.connection')


class _ProjectCredentialSource:
    """Resolve current secret material at each provider attempt."""

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        access_key = material.get("access_key")
        if not isinstance(access_key, str) or not access_key:
            raise ValueError("Unsplash credential access_key is required")
        return Credentials(SecretString(access_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Open declared project settings and resolve provider credentials per call."""
    if project is None:
        raise ValueError("Unsplash connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Unsplash project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Unsplash project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.unsplash.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Unsplash configuration has unknown fields")
    base_url = value.get("baseUrl", '')
    if not isinstance(base_url, str):
        raise ValueError("Unsplash configuration baseUrl has an invalid type")
    timeout = value.get("timeout", 0)
    if not type(timeout) is int:
        raise ValueError("Unsplash configuration timeout has an invalid type")
    timeout_seconds = timeout / 1_000_000_000
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not type(max_response_bytes) is int:
        raise ValueError("Unsplash configuration maxResponseBytes has an invalid type")
    return Config(base_url=base_url, timeout_seconds=timeout_seconds, max_response_bytes=max_response_bytes)
