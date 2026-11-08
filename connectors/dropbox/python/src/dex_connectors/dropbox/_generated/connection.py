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
from .definitions import CONNECTOR_ID, CONTINUE_FOLDER_DEFINITION, DOWNLOAD_FILE_DEFINITION, GET_METADATA_DEFINITION, LIST_FOLDER_DEFINITION, UPLOAD_FILE_DEFINITION
from .refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ..models import ContinueFolderInput, DownloadFileInput, DownloadedFile, EntryMetadata, FolderPage, GetMetadataInput, ListFolderInput, UploadFileInput, UploadedFile

class Connection:

    __slots__ = ('_list_folder', '_get_metadata', '_continue_folder', '_download_file', '_upload_file', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_folder: Query[ListFolderInput, FolderPage],
        get_metadata: Query[GetMetadataInput, EntryMetadata],
        continue_folder: Query[ContinueFolderInput, FolderPage],
        download_file: Query[DownloadFileInput, DownloadedFile],
        upload_file: Mutation[UploadFileInput, UploadedFile],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_folder is None or get_metadata is None or continue_folder is None or download_file is None or upload_file is None:
            raise ValueError("Dropbox connection provider and operations are required")
        if list_folder.definition().operation != LIST_FOLDER_DEFINITION.operation:
            raise ValueError("Dropbox connection operation identity differs")
        if get_metadata.definition().operation != GET_METADATA_DEFINITION.operation:
            raise ValueError("Dropbox connection operation identity differs")
        if continue_folder.definition().operation != CONTINUE_FOLDER_DEFINITION.operation:
            raise ValueError("Dropbox connection operation identity differs")
        if download_file.definition().operation != DOWNLOAD_FILE_DEFINITION.operation:
            raise ValueError("Dropbox connection operation identity differs")
        if upload_file.definition().operation != UPLOAD_FILE_DEFINITION.operation:
            raise ValueError("Dropbox connection operation identity differs")
        self.reference = reference
        self._list_folder = list_folder
        self._get_metadata = get_metadata
        self._continue_folder = continue_folder
        self._download_file = download_file
        self._upload_file = upload_file
        self._client = client

    def list_folder(self) -> Query[ListFolderInput, FolderPage]:
        return self._list_folder

    def get_metadata(self) -> Query[GetMetadataInput, EntryMetadata]:
        return self._get_metadata

    def continue_folder(self) -> Query[ContinueFolderInput, FolderPage]:
        return self._continue_folder

    def download_file(self) -> Query[DownloadFileInput, DownloadedFile]:
        return self._download_file

    def upload_file(self) -> Mutation[UploadFileInput, UploadedFile]:
        return self._upload_file

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "dropbox.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Dropbox connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.dropbox.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Dropbox client is required")
    return Connection(reference, client.list_folder(), client.get_metadata(), client.continue_folder(), client.download_file(), client.upload_file(), client)

setattr(new_connection, '__module__', 'dex_connectors.dropbox.connection')


def new_connection_with_operations(
    list_folder: Query[ListFolderInput, FolderPage],
    get_metadata: Query[GetMetadataInput, EntryMetadata],
    continue_folder: Query[ContinueFolderInput, FolderPage],
    download_file: Query[DownloadFileInput, DownloadedFile],
    upload_file: Mutation[UploadFileInput, UploadedFile],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_folder, get_metadata, continue_folder, download_file, upload_file)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.dropbox.connection')


def decode_credentials(material: dict[str, Any], *, shared: bool = False) -> Credentials:
    if not isinstance(material, dict):
        raise ValueError("Dropbox stored credential does not match the connector credential fields")
    if set(material) - set(('oauth_client_id', 'oauth_client_secret', 'access_token', 'refresh_token')):
        raise ValueError("Dropbox stored credential does not match the connector credential fields")
    is_shared = shared
    oauth_client_id = material.get("oauth_client_id")
    if oauth_client_id is not None and not (isinstance(oauth_client_id, str)):
        raise ValueError("Dropbox credential oauth_client_id has an invalid type")
    oauth_client_secret = material.get("oauth_client_secret")
    if oauth_client_secret is not None and not (isinstance(oauth_client_secret, str)):
        raise ValueError("Dropbox credential oauth_client_secret has an invalid type")
    access_token = material.get("access_token")
    if access_token is not None and not (isinstance(access_token, str)):
        raise ValueError("Dropbox credential access_token has an invalid type")
    refresh_token = material.get("refresh_token")
    if refresh_token is not None and not (isinstance(refresh_token, str)):
        raise ValueError("Dropbox credential refresh_token has an invalid type")
    if not oauth_client_id:
        raise ValueError("Dropbox credential oauth_client_id is required")
    if not oauth_client_secret and not is_shared:
        raise ValueError("Dropbox credential oauth_client_secret is required")
    if not access_token:
        raise ValueError("Dropbox credential access_token is required")
    if not refresh_token and not is_shared:
        raise ValueError("Dropbox credential refresh_token is required")
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
        self.driver = ManifestCredentialRefreshDriver[Credentials]("Dropbox", AUTHORIZATION, CREDENTIAL_FIELDS, http_client, settings=settings)

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
        raise ValueError("Dropbox connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Dropbox project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Dropbox project connection provider differs")
    config = _decode_config(matching[0].configuration)
    http_client = http_client or httpx.AsyncClient()
    client = Client(config, _ProjectCredentialSource(project, key, http_client, matching[0].configuration), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.dropbox.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "contentBaseUrl", "timeout", "maxResponseBytes", "maxDownloadBytes", "maxUploadBytes"}:
        raise ValueError("Dropbox configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Dropbox configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "contentBaseUrl" in value:
        content_base_url = value["contentBaseUrl"]
        if not (isinstance(content_base_url, str)):
            raise ValueError("Dropbox configuration contentBaseUrl has an invalid type")
        config_arguments['content_base_url'] = content_base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Dropbox configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Dropbox configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    if "maxDownloadBytes" in value:
        max_download_bytes = value["maxDownloadBytes"]
        if not (type(max_download_bytes) is int):
            raise ValueError("Dropbox configuration maxDownloadBytes has an invalid type")
        config_arguments['max_download_bytes'] = max_download_bytes
    if "maxUploadBytes" in value:
        max_upload_bytes = value["maxUploadBytes"]
        if not (type(max_upload_bytes) is int):
            raise ValueError("Dropbox configuration maxUploadBytes has an invalid type")
        config_arguments['max_upload_bytes'] = max_upload_bytes
    return Config(**config_arguments)
