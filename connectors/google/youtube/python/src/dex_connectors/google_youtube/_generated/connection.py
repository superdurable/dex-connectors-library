# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString, CredentialRefreshDriver
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject
from dex_connectors.sdk.projectconfig.provider import RefreshingCredentialProvider
from dex_connectors.sdk.oauthtoken import ManifestCredentialRefreshDriver

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, PROVIDER, ADD_PLAYLIST_ITEM_DEFINITION, GET_CHANNEL_DEFINITION, GET_VIDEO_DEFINITION, LIST_PLAYLIST_ITEMS_DEFINITION, SEARCH_VIDEOS_DEFINITION
from .refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ..models import AddPlaylistItemInput, AddedPlaylistItem, Channel, GetChannelInput, GetVideoInput, ListPlaylistItemsInput, PlaylistItemPage, SearchVideosInput, Video, VideoSearchPage

class Connection:

    __slots__ = ('_search_videos', '_get_video', '_get_channel', '_list_playlist_items', '_add_playlist_item', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        search_videos: Query[SearchVideosInput, VideoSearchPage],
        get_video: Query[GetVideoInput, Video],
        get_channel: Query[GetChannelInput, Channel],
        list_playlist_items: Query[ListPlaylistItemsInput, PlaylistItemPage],
        add_playlist_item: Mutation[AddPlaylistItemInput, AddedPlaylistItem],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != PROVIDER or search_videos is None or get_video is None or get_channel is None or list_playlist_items is None or add_playlist_item is None:
            raise ValueError("YouTube connection provider and operations are required")
        if search_videos.definition().operation != SEARCH_VIDEOS_DEFINITION.operation:
            raise ValueError("YouTube connection operation identity differs")
        if get_video.definition().operation != GET_VIDEO_DEFINITION.operation:
            raise ValueError("YouTube connection operation identity differs")
        if get_channel.definition().operation != GET_CHANNEL_DEFINITION.operation:
            raise ValueError("YouTube connection operation identity differs")
        if list_playlist_items.definition().operation != LIST_PLAYLIST_ITEMS_DEFINITION.operation:
            raise ValueError("YouTube connection operation identity differs")
        if add_playlist_item.definition().operation != ADD_PLAYLIST_ITEM_DEFINITION.operation:
            raise ValueError("YouTube connection operation identity differs")
        self.reference = reference
        self._search_videos = search_videos
        self._get_video = get_video
        self._get_channel = get_channel
        self._list_playlist_items = list_playlist_items
        self._add_playlist_item = add_playlist_item
        self._client = client

    def search_videos(self) -> Query[SearchVideosInput, VideoSearchPage]:
        return self._search_videos

    def get_video(self) -> Query[GetVideoInput, Video]:
        return self._get_video

    def get_channel(self) -> Query[GetChannelInput, Channel]:
        return self._get_channel

    def list_playlist_items(self) -> Query[ListPlaylistItemsInput, PlaylistItemPage]:
        return self._list_playlist_items

    def add_playlist_item(self) -> Mutation[AddPlaylistItemInput, AddedPlaylistItem]:
        return self._add_playlist_item

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "google_youtube.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("YouTube connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.google_youtube.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("YouTube client is required")
    return Connection(reference, client.search_videos(), client.get_video(), client.get_channel(), client.list_playlist_items(), client.add_playlist_item(), client)

setattr(new_connection, '__module__', 'dex_connectors.google_youtube.connection')


def new_connection_with_operations(
    search_videos: Query[SearchVideosInput, VideoSearchPage],
    get_video: Query[GetVideoInput, Video],
    get_channel: Query[GetChannelInput, Channel],
    list_playlist_items: Query[ListPlaylistItemsInput, PlaylistItemPage],
    add_playlist_item: Mutation[AddPlaylistItemInput, AddedPlaylistItem],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, search_videos, get_video, get_channel, list_playlist_items, add_playlist_item)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.google_youtube.connection')


def decode_credentials(material: dict[str, Any], *, shared: bool = False) -> Credentials:
    if not isinstance(material, dict):
        raise ValueError("YouTube stored credential does not match the connector credential fields")
    if set(material) - set(('oauth_client_id', 'oauth_client_secret', 'access_token', 'refresh_token', 'auth_method')):
        raise ValueError("YouTube stored credential does not match the connector credential fields")
    is_shared = shared
    oauth_client_id = material.get("oauth_client_id")
    if oauth_client_id is not None and not (isinstance(oauth_client_id, str)):
        raise ValueError("YouTube credential oauth_client_id has an invalid type")
    oauth_client_secret = material.get("oauth_client_secret")
    if oauth_client_secret is not None and not (isinstance(oauth_client_secret, str)):
        raise ValueError("YouTube credential oauth_client_secret has an invalid type")
    access_token = material.get("access_token")
    if access_token is not None and not (isinstance(access_token, str)):
        raise ValueError("YouTube credential access_token has an invalid type")
    refresh_token = material.get("refresh_token")
    if refresh_token is not None and not (isinstance(refresh_token, str)):
        raise ValueError("YouTube credential refresh_token has an invalid type")
    selected_method = material.get('auth_method')
    if selected_method is None:
        selected_method = ''
    if not isinstance(selected_method, str):
        raise ValueError("YouTube credential auth_method is invalid")
    method = selected_method or "google-oauth-readonly"
    values: dict[str, Any] = {'auth_method_id': method}
    if method == "google-oauth-readonly":
        if not oauth_client_id:
            raise ValueError("YouTube credential oauth_client_id is required")
        if oauth_client_id:
            values["oauth_client_id"] = oauth_client_id
        if not oauth_client_secret and not is_shared:
            raise ValueError("YouTube credential oauth_client_secret is required")
        if oauth_client_secret:
            values["oauth_client_secret"] = SecretString(oauth_client_secret)
        if not access_token:
            raise ValueError("YouTube credential access_token is required")
        if access_token:
            values["access_token"] = SecretString(access_token)
        if not refresh_token and not is_shared:
            raise ValueError("YouTube credential refresh_token is required")
        if refresh_token:
            values["refresh_token"] = SecretString(refresh_token)
    elif method == "google-oauth-manage":
        if not oauth_client_id:
            raise ValueError("YouTube credential oauth_client_id is required")
        if oauth_client_id:
            values["oauth_client_id"] = oauth_client_id
        if not oauth_client_secret and not is_shared:
            raise ValueError("YouTube credential oauth_client_secret is required")
        if oauth_client_secret:
            values["oauth_client_secret"] = SecretString(oauth_client_secret)
        if not access_token:
            raise ValueError("YouTube credential access_token is required")
        if access_token:
            values["access_token"] = SecretString(access_token)
        if not refresh_token and not is_shared:
            raise ValueError("YouTube credential refresh_token is required")
        if refresh_token:
            values["refresh_token"] = SecretString(refresh_token)
    else:
        raise ValueError("YouTube credential auth_method is invalid")
    return Credentials(**values)


def decode_shared_credentials(material: dict[str, Any]) -> Credentials:
    return decode_credentials(material, shared=True)


def encode_credentials(credentials: Credentials) -> dict[str, Any]:
    material: dict[str, Any] = {}
    method = credentials.auth_method_id or "google-oauth-readonly"
    material['auth_method'] = method
    if method == "google-oauth-readonly":
        if credentials.oauth_client_id:
            material["oauth_client_id"] = credentials.oauth_client_id
        if (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else ''):
            material["oauth_client_secret"] = (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else '')
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
        if (credentials.refresh_token.reveal() if credentials.refresh_token is not None else ''):
            material["refresh_token"] = (credentials.refresh_token.reveal() if credentials.refresh_token is not None else '')
    elif method == "google-oauth-manage":
        if credentials.oauth_client_id:
            material["oauth_client_id"] = credentials.oauth_client_id
        if (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else ''):
            material["oauth_client_secret"] = (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else '')
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
        if (credentials.refresh_token.reveal() if credentials.refresh_token is not None else ''):
            material["refresh_token"] = (credentials.refresh_token.reveal() if credentials.refresh_token is not None else '')
    else:
        raise ValueError("YouTube credential auth_method is invalid")
    decode_credentials(material)
    return material


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey, http_client: httpx.AsyncClient, settings: dict[str, Any]) -> None:
        self.provider = RefreshingCredentialProvider(
            project.connections, key, decode_credentials, encode_credentials, decode_shared_credentials
        )
        self.driver = ManifestCredentialRefreshDriver[Credentials]("YouTube", AUTHORIZATION, CREDENTIAL_FIELDS, http_client, settings=settings)

    async def resolve(self, call: Call) -> Credentials:
        return await self.provider.resolve_with_refresh(call, self.driver)

    async def resolve_with_refresh(self, call: Call, driver: CredentialRefreshDriver[Credentials] | None = None) -> Credentials:
        return await self.provider.resolve_with_refresh(call, driver or self.driver)

    async def resolve_after_rejection(self, call: Call, driver: CredentialRefreshDriver[Credentials] | None = None) -> Credentials:
        return await self.provider.resolve_after_rejection(call, driver or self.driver)


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("YouTube connection requires loaded project configuration")
    reference = ConnectionRef(PROVIDER, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("YouTube project connection is missing or duplicated")
    if matching[0].provider != PROVIDER:
        raise ValueError("YouTube project connection provider differs")
    config = _decode_config(matching[0].configuration)
    http_client = http_client or httpx.AsyncClient()
    client = Client(config, _ProjectCredentialSource(project, key, http_client, matching[0].configuration), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.google_youtube.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"endpoint", "maxResponseBytes"}:
        raise ValueError("YouTube configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "endpoint" in value:
        endpoint = value["endpoint"]
        if not (isinstance(endpoint, str)):
            raise ValueError("YouTube configuration endpoint has an invalid type")
        config_arguments['endpoint'] = endpoint
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("YouTube configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
