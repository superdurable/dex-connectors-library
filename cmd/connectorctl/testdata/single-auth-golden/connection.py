# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, LIST_PLAYLIST_TRACKS_DEFINITION
from ..models import ListPlaylistTracksInput, PlaylistTrackPage

class Connection:

    __slots__ = ('_list_playlist_tracks', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_playlist_tracks: Query[ListPlaylistTracksInput, PlaylistTrackPage],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_playlist_tracks is None:
            raise ValueError("Spotify connection provider and operations are required")
        if list_playlist_tracks.definition().operation != LIST_PLAYLIST_TRACKS_DEFINITION.operation:
            raise ValueError("Spotify connection operation identity differs")
        self.reference = reference
        self._list_playlist_tracks = list_playlist_tracks
        self._client = client

    def list_playlist_tracks(self) -> Query[ListPlaylistTracksInput, PlaylistTrackPage]:
        return self._list_playlist_tracks

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "spotify_fixture.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Spotify connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.spotify_fixture.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Spotify client is required")
    return Connection(reference, client.list_playlist_tracks(), client)

setattr(new_connection, '__module__', 'dex_connectors.spotify_fixture.connection')


def new_connection_with_operations(
    list_playlist_tracks: Query[ListPlaylistTracksInput, PlaylistTrackPage],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_playlist_tracks)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.spotify_fixture.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Spotify stored credential does not match the connector credential fields")
        if set(material) - set(('oauth_client_id', 'oauth_client_secret', 'access_token', 'refresh_token')):
            raise ValueError("Spotify stored credential does not match the connector credential fields")
        is_shared = bool(self.project.shared_connections and self.key in self.project.shared_connections)
        oauth_client_id = material.get("oauth_client_id")
        if oauth_client_id is not None and not (isinstance(oauth_client_id, str)):
            raise ValueError("Spotify credential oauth_client_id has an invalid type")
        oauth_client_secret = material.get("oauth_client_secret")
        if oauth_client_secret is not None and not (isinstance(oauth_client_secret, str)):
            raise ValueError("Spotify credential oauth_client_secret has an invalid type")
        access_token = material.get("access_token")
        if access_token is not None and not (isinstance(access_token, str)):
            raise ValueError("Spotify credential access_token has an invalid type")
        refresh_token = material.get("refresh_token")
        if refresh_token is not None and not (isinstance(refresh_token, str)):
            raise ValueError("Spotify credential refresh_token has an invalid type")
        if not oauth_client_secret and not is_shared:
            raise ValueError("Spotify credential oauth_client_secret is required")
        if not access_token:
            raise ValueError("Spotify credential access_token is required")
        return Credentials((oauth_client_id or ''), SecretString(oauth_client_secret or ''), SecretString(access_token), SecretString(refresh_token or ''))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Spotify connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Spotify project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Spotify project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.spotify_fixture.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"endpoint", "timeout", "maxResponseBytes"}:
        raise ValueError("Spotify configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "endpoint" in value:
        endpoint = value["endpoint"]
        if not (isinstance(endpoint, str)):
            raise ValueError("Spotify configuration endpoint has an invalid type")
        config_arguments['endpoint'] = endpoint
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Spotify configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Spotify configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
