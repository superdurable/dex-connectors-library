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
from .definitions import CONNECTOR_ID, DOWNLOAD_OBJECT_DEFINITION, LIST_OBJECTS_DEFINITION, UPLOAD_OBJECT_DEFINITION
from .refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ..models import DownloadObjectInput, DownloadedObject, ListObjectsInput, Object, ObjectsPage, UploadObjectInput

class Connection:

    __slots__ = ('_list_objects', '_download_object', '_upload_object', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_objects: Query[ListObjectsInput, ObjectsPage],
        download_object: Query[DownloadObjectInput, DownloadedObject],
        upload_object: Mutation[UploadObjectInput, Object],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_objects is None or download_object is None or upload_object is None:
            raise ValueError("Google Cloud Storage connection provider and operations are required")
        if list_objects.definition().operation != LIST_OBJECTS_DEFINITION.operation:
            raise ValueError("Google Cloud Storage connection operation identity differs")
        if download_object.definition().operation != DOWNLOAD_OBJECT_DEFINITION.operation:
            raise ValueError("Google Cloud Storage connection operation identity differs")
        if upload_object.definition().operation != UPLOAD_OBJECT_DEFINITION.operation:
            raise ValueError("Google Cloud Storage connection operation identity differs")
        self.reference = reference
        self._list_objects = list_objects
        self._download_object = download_object
        self._upload_object = upload_object
        self._client = client

    def list_objects(self) -> Query[ListObjectsInput, ObjectsPage]:
        return self._list_objects

    def download_object(self) -> Query[DownloadObjectInput, DownloadedObject]:
        return self._download_object

    def upload_object(self) -> Mutation[UploadObjectInput, Object]:
        return self._upload_object

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "google_cloud_storage.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Google Cloud Storage connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.google_cloud_storage.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Google Cloud Storage client is required")
    return Connection(reference, client.list_objects(), client.download_object(), client.upload_object(), client)

setattr(new_connection, '__module__', 'dex_connectors.google_cloud_storage.connection')


def new_connection_with_operations(
    list_objects: Query[ListObjectsInput, ObjectsPage],
    download_object: Query[DownloadObjectInput, DownloadedObject],
    upload_object: Mutation[UploadObjectInput, Object],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_objects, download_object, upload_object)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.google_cloud_storage.connection')


def decode_credentials(material: dict[str, Any], *, shared: bool = False) -> Credentials:
    if not isinstance(material, dict):
        raise ValueError("Google Cloud Storage stored credential does not match the connector credential fields")
    if set(material) - set(('oauth_client_id', 'oauth_client_secret', 'access_token', 'refresh_token')):
        raise ValueError("Google Cloud Storage stored credential does not match the connector credential fields")
    is_shared = shared
    oauth_client_id = material.get("oauth_client_id")
    if oauth_client_id is not None and not (isinstance(oauth_client_id, str)):
        raise ValueError("Google Cloud Storage credential oauth_client_id has an invalid type")
    oauth_client_secret = material.get("oauth_client_secret")
    if oauth_client_secret is not None and not (isinstance(oauth_client_secret, str)):
        raise ValueError("Google Cloud Storage credential oauth_client_secret has an invalid type")
    access_token = material.get("access_token")
    if access_token is not None and not (isinstance(access_token, str)):
        raise ValueError("Google Cloud Storage credential access_token has an invalid type")
    refresh_token = material.get("refresh_token")
    if refresh_token is not None and not (isinstance(refresh_token, str)):
        raise ValueError("Google Cloud Storage credential refresh_token has an invalid type")
    if not oauth_client_id:
        raise ValueError("Google Cloud Storage credential oauth_client_id is required")
    if not oauth_client_secret and not is_shared:
        raise ValueError("Google Cloud Storage credential oauth_client_secret is required")
    if not access_token:
        raise ValueError("Google Cloud Storage credential access_token is required")
    if not refresh_token and not is_shared:
        raise ValueError("Google Cloud Storage credential refresh_token is required")
    return Credentials(oauth_client_id, SecretString(oauth_client_secret or ''), SecretString(access_token), SecretString(refresh_token or ''))


def decode_shared_credentials(material: dict[str, Any]) -> Credentials:
    return decode_credentials(material, shared=True)


def encode_credentials(credentials: Credentials) -> dict[str, Any]:
    material: dict[str, Any] = {}
    if credentials.oauth_client_id:
        material["oauth_client_id"] = credentials.oauth_client_id
    if credentials.oauth_client_secret.reveal():
        material["oauth_client_secret"] = credentials.oauth_client_secret.reveal()
    if credentials.access_token.reveal():
        material["access_token"] = credentials.access_token.reveal()
    if credentials.refresh_token.reveal():
        material["refresh_token"] = credentials.refresh_token.reveal()
    decode_credentials(material)
    return material


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey, http_client: httpx.AsyncClient, settings: dict[str, Any]) -> None:
        self.provider = RefreshingCredentialProvider(
            project.connections, key, decode_credentials, encode_credentials, decode_shared_credentials
        )
        self.driver = ManifestCredentialRefreshDriver[Credentials]("Google Cloud Storage", AUTHORIZATION, CREDENTIAL_FIELDS, http_client, settings=settings)

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
        raise ValueError("Google Cloud Storage connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Google Cloud Storage project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Google Cloud Storage project connection provider differs")
    config = _decode_config(matching[0].configuration)
    http_client = http_client or httpx.AsyncClient()
    client = Client(config, _ProjectCredentialSource(project, key, http_client, matching[0].configuration), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.google_cloud_storage.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes", "maxUploadBytes"}:
        raise ValueError("Google Cloud Storage configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Google Cloud Storage configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Google Cloud Storage configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Google Cloud Storage configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    if "maxUploadBytes" in value:
        max_upload_bytes = value["maxUploadBytes"]
        if not (type(max_upload_bytes) is int):
            raise ValueError("Google Cloud Storage configuration maxUploadBytes has an invalid type")
        config_arguments['max_upload_bytes'] = max_upload_bytes
    return Config(**config_arguments)
