# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, LIST_USER_VIDEOS_DEFINITION
from ..models import ListUserVideosInput, UserVideosPage

class Connection:

    __slots__ = ('_list_user_videos', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_user_videos: Query[ListUserVideosInput, UserVideosPage],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_user_videos is None:
            raise ValueError("Kuaishou connection provider and operations are required")
        if list_user_videos.definition().operation != LIST_USER_VIDEOS_DEFINITION.operation:
            raise ValueError("Kuaishou connection operation identity differs")
        self.reference = reference
        self._list_user_videos = list_user_videos
        self._client = client

    def list_user_videos(self) -> Query[ListUserVideosInput, UserVideosPage]:
        return self._list_user_videos

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "kuaishou.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Kuaishou connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.kuaishou.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Kuaishou client is required")
    return Connection(reference, client.list_user_videos(), client)

setattr(new_connection, '__module__', 'dex_connectors.kuaishou.connection')


def new_connection_with_operations(
    list_user_videos: Query[ListUserVideosInput, UserVideosPage],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_user_videos)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.kuaishou.connection')


class CredentialMaterialError(ValueError):
    "Stored Kuaishou credential material fails the connector credential checks, so no retry can succeed."


def is_credential_material_error(error: BaseException) -> bool:
    """Report malformed stored credential material, also behind the SDK provider's decoding ValueError."""
    if isinstance(error, CredentialMaterialError):
        return True
    # Only the SDK decoding wrapper qualifies; other errors may be transient.
    return (
        type(error) is ValueError
        and str(error) in {'project credential decoding failed', 'shared project credential decoding failed'}
        and isinstance(error.__context__, CredentialMaterialError)
    )


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise CredentialMaterialError("Kuaishou stored credential does not match the connector credential fields")
        if set(material) - set(('appID', 'accessToken')):
            raise CredentialMaterialError("Kuaishou stored credential does not match the connector credential fields")
        app_id = material.get("appID")
        if app_id is not None and not (isinstance(app_id, str)):
            raise CredentialMaterialError("Kuaishou credential appID has an invalid type")
        access_token = material.get("accessToken")
        if access_token is not None and not (isinstance(access_token, str)):
            raise CredentialMaterialError("Kuaishou credential accessToken has an invalid type")
        if not app_id:
            raise CredentialMaterialError("Kuaishou credential appID is required")
        if not access_token:
            raise CredentialMaterialError("Kuaishou credential accessToken is required")
        return Credentials(app_id, SecretString(access_token))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Kuaishou connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Kuaishou project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Kuaishou project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.kuaishou.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes", "pageSize"}:
        raise ValueError("Kuaishou configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Kuaishou configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Kuaishou configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Kuaishou configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    if "pageSize" in value:
        page_size = value["pageSize"]
        if not (type(page_size) is int):
            raise ValueError("Kuaishou configuration pageSize has an invalid type")
        config_arguments['page_size'] = page_size
    return Config(**config_arguments)
